package api

// Runtime settings and Discord notifications.
//
// Settings live in the database (not the environment) so an administrator can
// change the webhook, the daily check hour and the default language without
// touching the container. The Discord webhook URL is a secret: the API returns a
// masked preview, never logs it, and never stores it in an audit snapshot.

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
)

// Error codes for notification configuration problems. The SPA maps
// discord_not_configured onto errors.discordNotConfigured.
const (
	codeDiscordNotConfigured = "discord_not_configured"
	codeDiscordRejected      = "discord_rejected"
	codeDiscordUnreachable   = "discord_unreachable"
)

// ---------------------------------------------------------------------------
// Wire types (openapi Settings / SettingsUpdate)
// ---------------------------------------------------------------------------

type discordSettings struct {
	WebhookSet     bool   `json:"webhook_set"`
	WebhookPreview string `json:"webhook_preview"`
	Enabled        bool   `json:"enabled"`
}

type schedulerSettings struct {
	CheckHour int `json:"check_hour"`
	DaysAhead int `json:"days_ahead"`
}

type localeSettings struct {
	Default string `json:"default"`
}

type generalSettings struct {
	OrganizationName string `json:"organization_name"`
}

// settingsResponse is the GET/PUT /settings body.
type settingsResponse struct {
	Discord   discordSettings   `json:"discord"`
	Scheduler schedulerSettings `json:"scheduler"`
	Locale    localeSettings    `json:"locale"`
	General   generalSettings   `json:"general"`
}

// settingsUpdateRequest is the PUT /settings body. Every field is optional:
// absent means "leave unchanged", which is what the SPA sends for a partial
// form submit.
type settingsUpdateRequest struct {
	Discord   *discordSettingsUpdate   `json:"discord"`
	Scheduler *schedulerSettingsUpdate `json:"scheduler"`
	Locale    *localeSettingsUpdate    `json:"locale"`
	General   *generalSettingsUpdate   `json:"general"`
}

type discordSettingsUpdate struct {
	// WebhookURL replaces the stored webhook. An empty string keeps the stored
	// value (see openapi/openapi.yaml).
	WebhookURL *string `json:"webhook_url"`
	// ClearWebhook removes the stored webhook. It is an additive extension to
	// the frozen schema (which otherwise offers no way to delete the secret);
	// sending it together with a non-empty webhook_url is a validation error.
	ClearWebhook bool  `json:"clear_webhook"`
	Enabled      *bool `json:"enabled"`
}

type schedulerSettingsUpdate struct {
	CheckHour *int `json:"check_hour"`
	DaysAhead *int `json:"days_ahead"`
}

type localeSettingsUpdate struct {
	Default *string `json:"default"`
}

type generalSettingsUpdate struct {
	OrganizationName *string `json:"organization_name"`
}

// settingsSnapshot is the audit representation of the settings: the same masked
// shape the API returns, plus whether the webhook secret was touched.
type settingsSnapshot struct {
	settingsResponse
	WebhookUpdated bool `json:"webhook_updated"`
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

// mountSettingsRoutes registers the admin-only settings and notification
// endpoints on the protected router.
func (a *API) mountSettingsRoutes(r chi.Router) {
	admin := a.auth.RequireRole(auth.RoleAdmin)
	r.With(admin).Get("/settings", a.handleGetSettings)
	r.With(admin).Put("/settings", a.handlePutSettings)
	r.With(admin).Post("/notifications/test", a.handleTestNotification)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleGetSettings implements GET /api/v1/settings.
func (a *API) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	current, err := settings.Load(r.Context(), a.db)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	a.logSettingsProblems(current)
	writeData(w, http.StatusOK, settingsDTO(current))
}

// handlePutSettings implements PUT /api/v1/settings with partial-update
// semantics: only the fields present in the request change.
func (a *API) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body settingsUpdateRequest
	if err := DecodeJSON(w, r, &body); err != nil {
		DecodeError(w, r, err)
		return
	}

	var (
		updated settings.Settings
		before  settingsSnapshot
		after   settingsSnapshot
	)

	err := a.withTx(r.Context(), func(tx *sql.Tx) error {
		current, err := settings.Load(r.Context(), tx)
		if err != nil {
			return err
		}
		a.logSettingsProblems(current)
		before = settingsSnapshot{settingsResponse: settingsDTO(current)}

		next := current
		if err := applySettingsUpdate(&next, body); err != nil {
			return err
		}
		if details := next.Validate(); len(details) > 0 {
			return &validationError{details: details}
		}

		actor := auth.UserFrom(r.Context())
		if err := settings.Save(r.Context(), tx, next, actorID(actor)); err != nil {
			return err
		}

		after = settingsSnapshot{
			settingsResponse: settingsDTO(next),
			WebhookUpdated:   next.DiscordWebhookURL != current.DiscordWebhookURL,
		}
		updated = next

		return a.recordAudit(r.Context(), tx, a.auditRecord(r,
			audit.ActionSettingsUpdate, audit.EntitySettings, "settings", before, after))
	})
	if err != nil {
		var validation *validationError
		if errors.As(err, &validation) {
			Error(w, r, http.StatusBadRequest, CodeValidationFailed, validation.Error(), validation.details...)
			return
		}
		InternalError(w, r, a.log, err)
		return
	}

	writeData(w, http.StatusOK, settingsDTO(updated))
}

// handleTestNotification implements POST /api/v1/notifications/test: it posts a
// sample report so an administrator can verify the webhook before relying on it.
//
// A test is an explicit admin action, so it is allowed while discord.enabled is
// off (that switch gates the automatic overdue alerts).
func (a *API) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	current, err := settings.Load(r.Context(), a.db)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}
	if !current.WebhookConfigured() {
		Error(w, r, http.StatusUnprocessableEntity, codeDiscordNotConfigured,
			"no Discord webhook is configured")
		return
	}

	notifier, err := notify.NewDiscord(notify.DiscordOptions{
		WebhookURL: current.DiscordWebhookURL,
		Now:        a.now,
	})
	if err != nil {
		// The stored URL no longer parses: tell the admin to fix it.
		Error(w, r, http.StatusUnprocessableEntity, codeDiscordNotConfigured,
			"the stored Discord webhook is not usable", err.Error())
		return
	}

	report := notify.SampleReport(current.DefaultLocale, current.OrganizationName, a.cfg.BaseURL, a.now())
	result, sendErr := notifier.Send(r.Context(), report)

	outcome := map[string]any{
		"ok":       sendErr == nil,
		"messages": result.Messages,
		"embeds":   result.Embeds,
	}
	a.auditNotification(r, "test", outcome, sendErr)

	if sendErr != nil {
		a.writeNotificationError(w, r, sendErr)
		return
	}
	writeData(w, http.StatusOK, outcome)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// applySettingsUpdate merges the request onto the stored settings.
func applySettingsUpdate(target *settings.Settings, body settingsUpdateRequest) error {
	if body.Discord != nil {
		if body.Discord.ClearWebhook {
			if body.Discord.WebhookURL != nil && strings.TrimSpace(*body.Discord.WebhookURL) != "" {
				return &validationError{details: []string{
					"discord.webhook_url and discord.clear_webhook contradict each other"}}
			}
			target.DiscordWebhookURL = ""
		} else if body.Discord.WebhookURL != nil {
			// An empty string keeps the stored secret (contract).
			if raw := strings.TrimSpace(*body.Discord.WebhookURL); raw != "" {
				if err := notify.ValidateWebhookURL(raw); err != nil {
					return &validationError{details: []string{"discord.webhook_url " + err.Error()}}
				}
				target.DiscordWebhookURL = raw
			}
		}
		if body.Discord.Enabled != nil {
			target.DiscordEnabled = *body.Discord.Enabled
		}
	}
	if body.Scheduler != nil {
		if body.Scheduler.CheckHour != nil {
			target.CheckHour = *body.Scheduler.CheckHour
		}
		if body.Scheduler.DaysAhead != nil {
			target.DaysAhead = *body.Scheduler.DaysAhead
		}
	}
	if body.Locale != nil && body.Locale.Default != nil {
		target.DefaultLocale = strings.ToLower(strings.TrimSpace(*body.Locale.Default))
	}
	if body.General != nil && body.General.OrganizationName != nil {
		target.OrganizationName = strings.TrimSpace(*body.General.OrganizationName)
	}
	return nil
}

// settingsDTO renders the masked wire representation.
func settingsDTO(current settings.Settings) settingsResponse {
	return settingsResponse{
		Discord: discordSettings{
			WebhookSet:     current.WebhookConfigured(),
			WebhookPreview: notify.MaskWebhook(current.DiscordWebhookURL),
			Enabled:        current.DiscordEnabled,
		},
		Scheduler: schedulerSettings{
			CheckHour: current.CheckHour,
			DaysAhead: current.DaysAhead,
		},
		Locale:  localeSettings{Default: current.DefaultLocale},
		General: generalSettings{OrganizationName: current.OrganizationName},
	}
}

// logSettingsProblems surfaces unreadable settings rows without failing the
// request: an admin must be able to repair them through this very endpoint.
func (a *API) logSettingsProblems(current settings.Settings) {
	for _, problem := range current.Problems {
		a.log.Warn("unreadable setting", "problem", problem)
	}
}

// auditNotification records a delivery attempt. The payload never contains the
// webhook URL or token, only the outcome.
func (a *API) auditNotification(r *http.Request, trigger string, outcome map[string]any, sendErr error) {
	if a.audit == nil {
		return
	}
	after := map[string]any{
		"trigger":  trigger,
		"ok":       outcome["ok"],
		"messages": outcome["messages"],
		"embeds":   outcome["embeds"],
	}
	if sendErr != nil {
		after["error"] = sendErr.Error()
		after["status"] = notify.Status(sendErr)
	}
	if err := a.audit.Record(r.Context(), a.auditRecord(r,
		audit.ActionNotify, audit.EntityNotification, trigger, nil, after)); err != nil {
		a.log.Warn("failed to record notification audit", "error", err)
	}
}

// writeNotificationError maps a delivery failure onto the error envelope: a
// rejected webhook is the administrator's to fix (422), while an unreachable or
// rate-limited endpoint is a transient upstream failure (502).
func (a *API) writeNotificationError(w http.ResponseWriter, r *http.Request, err error) {
	details := []string{err.Error()}
	if status := notify.Status(err); status != 0 {
		details = append(details, fmt.Sprintf("Discord responded with HTTP %d", status))
	}
	if notify.IsPermanent(err) {
		Error(w, r, http.StatusUnprocessableEntity, codeDiscordRejected,
			"Discord rejected the message", details...)
		return
	}
	Error(w, r, http.StatusBadGateway, codeDiscordUnreachable,
		"could not reach Discord", details...)
}
