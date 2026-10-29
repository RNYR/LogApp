// Package settings owns the runtime configuration that lives in the database
// `settings` table (docs/contracts/schema.md), as opposed to the environment
// configuration in package config.
//
// It is the single source of truth for the settings keys, their defaults, their
// validation rules and the masking of secrets, so the admin API (task 4.a) and
// the overdue scheduler (task 4.b) cannot drift apart.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Settings keys (docs/contracts/schema.md, "Settings keys (seeded defaults)").
const (
	KeyDiscordWebhookURL  = "discord.webhook_url"
	KeyDiscordEnabled     = "discord.enabled"
	KeySchedulerCheckHour = "scheduler.check_hour"
	KeySchedulerDaysAhead = "scheduler.days_ahead"
	KeyLocaleDefault      = "locale.default"
	KeyOrganizationName   = "general.organization_name"
)

// Defaults and limits. The values mirror the settings table in schema.md.
const (
	DefaultCheckHour = 11
	DefaultDaysAhead = 0
	DefaultLocale    = "en"

	// MaxDaysAhead keeps the overdue window inside a decade; beyond that the
	// daily report stops being a reminder and becomes a data dump.
	MaxDaysAhead = 3650
	// MaxOrganizationNameLen keeps the name printable in embeds and labels.
	MaxOrganizationNameLen = 120
)

// SupportedLocales are the languages the app ships (request: Czech, English,
// Italian).
var SupportedLocales = []string{"en", "cs", "it"}

// Querier is satisfied by *sql.DB and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Execer is satisfied by *sql.DB and *sql.Tx.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Settings is the typed runtime configuration.
//
// The Discord webhook URL is a secret: it is never logged, never returned raw by
// the API (use MaskWebhook) and never stored in an audit snapshot.
type Settings struct {
	DiscordWebhookURL string
	DiscordEnabled    bool
	CheckHour         int
	DaysAhead         int
	DefaultLocale     string
	OrganizationName  string

	// Problems lists keys whose stored value could not be decoded; the loader
	// falls back to the default so a corrupt row can still be repaired through
	// the admin API.
	Problems []string
}

// Default returns the compiled-in defaults, which apply to every key that has
// no row in the settings table yet.
func Default() Settings {
	return Settings{
		CheckHour:     DefaultCheckHour,
		DaysAhead:     DefaultDaysAhead,
		DefaultLocale: DefaultLocale,
	}
}

// Load reads every known key and substitutes defaults for missing or
// unreadable values.
func Load(ctx context.Context, q Querier) (Settings, error) {
	s := Default()

	rows, err := q.QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return Settings{}, fmt.Errorf("settings: load: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return Settings{}, fmt.Errorf("settings: scan: %w", err)
		}
		if err := s.apply(key, value); err != nil {
			s.Problems = append(s.Problems, fmt.Sprintf("%s: %v", key, err))
		}
	}
	if err := rows.Err(); err != nil {
		return Settings{}, fmt.Errorf("settings: load rows: %w", err)
	}
	return s, nil
}

// apply decodes one stored value onto the matching field. Unknown keys are
// ignored: the settings table is extensible and older/newer builds may coexist.
func (s *Settings) apply(key, value string) error {
	switch key {
	case KeyDiscordWebhookURL:
		return json.Unmarshal([]byte(value), &s.DiscordWebhookURL)
	case KeyDiscordEnabled:
		return json.Unmarshal([]byte(value), &s.DiscordEnabled)
	case KeySchedulerCheckHour:
		return json.Unmarshal([]byte(value), &s.CheckHour)
	case KeySchedulerDaysAhead:
		return json.Unmarshal([]byte(value), &s.DaysAhead)
	case KeyLocaleDefault:
		return json.Unmarshal([]byte(value), &s.DefaultLocale)
	case KeyOrganizationName:
		return json.Unmarshal([]byte(value), &s.OrganizationName)
	default:
		return nil
	}
}

// Save upserts every settings key inside the caller's transaction, so the whole
// runtime configuration is written atomically (partial updates are expressed by
// loading, mutating and saving the full set).
func Save(ctx context.Context, tx Execer, s Settings, updatedBy *int64) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var actor any
	if updatedBy != nil {
		actor = *updatedBy
	}

	values := map[string]any{
		KeyDiscordWebhookURL:  s.DiscordWebhookURL,
		KeyDiscordEnabled:     s.DiscordEnabled,
		KeySchedulerCheckHour: s.CheckHour,
		KeySchedulerDaysAhead: s.DaysAhead,
		KeyLocaleDefault:      s.DefaultLocale,
		KeyOrganizationName:   s.OrganizationName,
	}

	for _, key := range keys() {
		encoded, err := json.Marshal(values[key])
		if err != nil {
			return fmt.Errorf("settings: encode %s: %w", key, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at, updated_by)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
			key, string(encoded), now, actor,
		); err != nil {
			return fmt.Errorf("settings: save %s: %w", key, err)
		}
	}
	return nil
}

// keys lists every managed key in a stable order.
func keys() []string {
	return []string{
		KeyDiscordWebhookURL,
		KeyDiscordEnabled,
		KeySchedulerCheckHour,
		KeySchedulerDaysAhead,
		KeyLocaleDefault,
		KeyOrganizationName,
	}
}

// Validate returns human-readable problems with the current values. An empty
// slice means the settings are usable.
func (s Settings) Validate() []string {
	var problems []string

	if s.CheckHour < 0 || s.CheckHour > 23 {
		problems = append(problems, "scheduler.check_hour must be between 0 and 23")
	}
	if s.DaysAhead < 0 {
		problems = append(problems, "scheduler.days_ahead must not be negative")
	}
	if s.DaysAhead > MaxDaysAhead {
		problems = append(problems, fmt.Sprintf("scheduler.days_ahead must not exceed %d", MaxDaysAhead))
	}
	if !IsSupportedLocale(s.DefaultLocale) {
		problems = append(problems, fmt.Sprintf("locale.default must be one of %s", strings.Join(SupportedLocales, ", ")))
	}
	if len(s.OrganizationName) > MaxOrganizationNameLen {
		problems = append(problems, fmt.Sprintf("general.organization_name must not exceed %d characters", MaxOrganizationNameLen))
	}
	if strings.ContainsAny(s.OrganizationName, "\r\n\t") {
		problems = append(problems, "general.organization_name must not contain control characters")
	}
	// The webhook URL itself is validated by the delivery layer
	// (notify.ValidateWebhookURL) so this package stays independent of it; the
	// API validates before saving and the scheduler surfaces a delivery error.
	return problems
}

// WebhookConfigured reports whether a Discord webhook URL is stored.
func (s Settings) WebhookConfigured() bool {
	return strings.TrimSpace(s.DiscordWebhookURL) != ""
}

// IsSupportedLocale reports whether code is one of the shipped languages.
func IsSupportedLocale(code string) bool {
	for _, locale := range SupportedLocales {
		if strings.EqualFold(strings.TrimSpace(code), locale) {
			return true
		}
	}
	return false
}
