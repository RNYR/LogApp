package api

// Notification delivery log and on-demand overdue runs.
//
// The endpoints are admin-only: the log exposes which items were reported and
// why a delivery failed, and a manual run posts to the organisation's Discord
// channel.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/auth"
	"sklad/server/internal/jobs"
	"sklad/server/internal/notify"
)

// OverdueRunner performs one overdue check on demand. It is implemented by
// *jobs.Scheduler; tests can substitute a stub.
type OverdueRunner interface {
	Run(ctx context.Context, trigger notify.Kind) (jobs.RunResult, error)
}

// notificationDelivery is one row of the delivery log
// (openapi/components/schemas/NotificationDelivery).
type notificationDelivery struct {
	ID        int64   `json:"id"`
	ChannelID int64   `json:"channel_id"`
	Trigger   string  `json:"trigger"`
	Status    string  `json:"status"`
	ItemCount int     `json:"item_count"`
	Error     *string `json:"error"`
	CreatedAt string  `json:"created_at"`
	SentAt    *string `json:"sent_at"`
}

// deliveryListMeta is the pagination block of the delivery log.
type deliveryListMeta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// mountNotifyRoutes registers the admin-only notification endpoints on the
// protected router. POST /notifications/test is registered with the settings
// routes (it is part of configuring the webhook).
func (a *API) mountNotifyRoutes(r chi.Router) {
	admin := a.auth.RequireRole(auth.RoleAdmin)
	r.With(admin).Get("/notifications", a.handleListNotifications)
	r.With(admin).Post("/notifications/run-now", a.handleRunNotifications)
}

// handleListNotifications implements GET /api/v1/notifications: the delivery
// log, newest first.
func (a *API) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	page, err := intParam(query.Get("page"), 1, 1, maxPage)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, fmt.Sprintf("page: %s", err))
		return
	}
	perPage, err := intParam(query.Get("per_page"), defaultPerPage, 1, maxPerPage)
	if err != nil {
		Error(w, r, http.StatusBadRequest, CodeValidationFailed, fmt.Sprintf("per_page: %s", err))
		return
	}

	deliveries, total, err := a.queryDeliveries(r.Context(), perPage, (page-1)*perPage)
	if err != nil {
		InternalError(w, r, a.log, err)
		return
	}

	totalPages := 0
	if total > 0 {
		totalPages = (total + perPage - 1) / perPage
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": deliveries,
		"meta": deliveryListMeta{Page: page, PerPage: perPage, Total: total, TotalPages: totalPages},
	})
}

// queryDeliveries reads one page of the delivery log plus the total row count.
func (a *API) queryDeliveries(ctx context.Context, limit, offset int) ([]notificationDelivery, int, error) {
	var total int
	if err := a.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_deliveries`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("api: count deliveries: %w", err)
	}

	rows, err := a.db.QueryContext(ctx, `SELECT id, channel_id, trigger, status, item_count, error, created_at, sent_at
		FROM notification_deliveries
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("api: list deliveries: %w", err)
	}
	defer rows.Close()

	deliveries := make([]notificationDelivery, 0, limit)
	for rows.Next() {
		var (
			delivery  notificationDelivery
			errorText sql.NullString
			sentAt    sql.NullString
		)
		if err := rows.Scan(&delivery.ID, &delivery.ChannelID, &delivery.Trigger, &delivery.Status,
			&delivery.ItemCount, &errorText, &delivery.CreatedAt, &sentAt); err != nil {
			return nil, 0, fmt.Errorf("api: scan delivery: %w", err)
		}
		delivery.Error = nullStringValue(errorText)
		delivery.SentAt = nullStringValue(sentAt)
		deliveries = append(deliveries, delivery)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("api: iterate deliveries: %w", err)
	}
	return deliveries, total, nil
}

// handleRunNotifications implements POST /api/v1/notifications/run-now: it runs
// the overdue check immediately, with the same per-item/per-day dedupe as the
// scheduled run, and reports what happened.
func (a *API) handleRunNotifications(w http.ResponseWriter, r *http.Request) {
	if a.scheduler == nil {
		Error(w, r, http.StatusServiceUnavailable, CodeInternal, "the notification scheduler is not available")
		return
	}

	result, err := a.scheduler.Run(r.Context(), notify.KindManual)
	if err != nil {
		a.writeRunError(w, r, result, err)
		return
	}

	// NotifyRunResponse plus the counters the SPA shows for skipped/truncated
	// reports (additional properties are allowed by the schema).
	WriteJSON(w, http.StatusOK, map[string]any{
		"data": map[string]any{
			"due_count":     result.DueCount,
			"sent":          result.Sent,
			"delivery_id":   result.DeliveryID,
			"trigger":       string(result.Trigger),
			"new_items":     result.NewCount,
			"skipped_items": result.SkippedCount,
			"truncated":     result.Truncated,
			"reason":        result.Reason,
		},
	})
}

// writeRunError maps a manual-run failure onto the error envelope, reusing the
// notification codes from the settings API.
func (a *API) writeRunError(w http.ResponseWriter, r *http.Request, result jobs.RunResult, err error) {
	switch {
	case errors.Is(err, jobs.ErrRunInProgress):
		Error(w, r, http.StatusConflict, codeConflict, "a notification run is already in progress")
	case errors.Is(err, jobs.ErrWebhookInvalid):
		Error(w, r, http.StatusUnprocessableEntity, codeDiscordNotConfigured,
			"no usable Discord webhook is configured", err.Error())
	case errors.Is(err, jobs.ErrDeliveryFailed):
		details := []string{err.Error()}
		if status := notify.Status(err); status != 0 {
			details = append(details, fmt.Sprintf("Discord responded with HTTP %d", status))
		}
		if result.DeliveryID != nil {
			details = append(details, fmt.Sprintf("delivery %d was recorded as failed", *result.DeliveryID))
		}
		if notify.IsPermanent(err) {
			Error(w, r, http.StatusUnprocessableEntity, codeDiscordRejected, "Discord rejected the message", details...)
			return
		}
		Error(w, r, http.StatusBadGateway, codeDiscordUnreachable, "could not reach Discord", details...)
	default:
		InternalError(w, r, a.log, err)
	}
}
