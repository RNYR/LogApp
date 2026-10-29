package api

// Notification API surface: the manual overdue run and the delivery log. The
// scheduler itself is covered in package jobs; here the focus is the HTTP
// mapping and the admin-only access rules.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"sklad/server/internal/jobs"
	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
)

// stubRunner returns canned results so each error mapping can be exercised.
type stubRunner struct {
	mu     sync.Mutex
	result jobs.RunResult
	err    error
	calls  []notify.Kind
}

func (s *stubRunner) Run(_ context.Context, trigger notify.Kind) (jobs.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, trigger)
	result := s.result
	if result.Trigger == "" {
		result.Trigger = trigger
	}
	return result, s.err
}

func (s *stubRunner) seen() []notify.Kind {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]notify.Kind(nil), s.calls...)
}

// recordingNotifier captures the reports a real scheduler would deliver.
type recordingNotifier struct {
	mu      sync.Mutex
	reports []notify.Report
}

func (n *recordingNotifier) Name() string { return "recording" }

func (n *recordingNotifier) Send(_ context.Context, report notify.Report) (notify.Result, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reports = append(n.reports, report)
	return notify.Result{Messages: 1, Embeds: len(report.Items) + 1, MessageIDs: []string{"7"}}, nil
}

func (n *recordingNotifier) count() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.reports)
}

// seedNotificationSettings stores the runtime configuration the scheduler reads.
func seedNotificationSettings(t *testing.T, env *testEnv, webhook string, enabled bool) {
	t.Helper()
	var adminID int64
	if err := env.db.QueryRow(`SELECT id FROM users WHERE username = ?`, testAdminUser).Scan(&adminID); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	if err := settings.Save(context.Background(), env.db, settings.Settings{
		DiscordWebhookURL: webhook,
		DiscordEnabled:    enabled,
		CheckHour:         11,
		DefaultLocale:     "en",
		OrganizationName:  "PEKAT VISION",
	}, &adminID); err != nil {
		t.Fatalf("settings.Save: %v", err)
	}
}

// testWebhookURL carries a distinctive token so the tests can prove it never
// appears unmasked in a response, an audit row or an error.
const testWebhookURL = "https://discord.com/api/webhooks/1234567890123456789/super-secret-token"

// TestRunNowUsesTheRealScheduler proves the endpoint drives a real scheduler and
// that the per-item/per-day dedupe is visible through the API.
func TestRunNowUsesTheRealScheduler(t *testing.T) {
	t.Parallel()
	recorder := &recordingNotifier{}

	env := newTestEnv(t, withScheduler(func(db *sql.DB) OverdueRunner {
		scheduler, err := jobs.New(jobs.Options{
			DB:       db,
			Location: time.UTC,
			AppURL:   "https://sklad.local",
			Now:      func() time.Time { return testNow },
			Notifier: func(string) (notify.Notifier, error) { return recorder, nil },
		})
		if err != nil {
			t.Fatalf("jobs.New: %v", err)
		}
		return scheduler
	}))
	seedNotificationSettings(t, env, testWebhookURL, true)

	// An item whose return date has passed (the harness clock is 2026-03-15).
	overdue := env.createItem(map[string]any{"name": "Overdue camera", "return_date": "2026-03-10"})

	payload := env.admin.mustDo(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{}, http.StatusOK)
	run := decode[struct {
		Data struct {
			DueCount   int    `json:"due_count"`
			Sent       bool   `json:"sent"`
			DeliveryID *int64 `json:"delivery_id"`
			Trigger    string `json:"trigger"`
			NewItems   int    `json:"new_items"`
			Reason     string `json:"reason"`
		} `json:"data"`
	}](t, payload)

	if run.Data.DueCount != 1 || !run.Data.Sent || run.Data.NewItems != 1 || run.Data.DeliveryID == nil {
		t.Fatalf("unexpected run result: %+v", run.Data)
	}
	if run.Data.Trigger != string(notify.KindManual) {
		t.Fatalf("run-now must record the manual trigger, got %q", run.Data.Trigger)
	}
	if recorder.count() != 1 {
		t.Fatalf("expected one delivered report, got %d", recorder.count())
	}

	// The delivery log exposes the row.
	payload = env.admin.mustDo(http.MethodGet, "/api/v1/notifications", nil, http.StatusOK)
	list := decode[struct {
		Data []notificationDelivery `json:"data"`
		Meta deliveryListMeta       `json:"meta"`
	}](t, payload)
	if len(list.Data) != 1 || list.Meta.Total != 1 {
		t.Fatalf("expected one delivery row, got %+v", list)
	}
	row := list.Data[0]
	if row.Trigger != "manual" || row.Status != "sent" || row.ItemCount != 1 {
		t.Fatalf("unexpected delivery row: %+v", row)
	}
	if row.SentAt == nil || row.Error != nil || row.ChannelID == 0 {
		t.Fatalf("a sent delivery needs sent_at, no error and a channel: %+v", row)
	}
	if row.ID != *run.Data.DeliveryID {
		t.Fatalf("the log row (%d) does not match the reported delivery id (%d)", row.ID, *run.Data.DeliveryID)
	}

	// A second run the same day reports the dedupe instead of sending twice.
	payload = env.admin.mustDo(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{}, http.StatusOK)
	second := decode[struct {
		Data struct {
			Sent       bool   `json:"sent"`
			DeliveryID *int64 `json:"delivery_id"`
			NewItems   int    `json:"new_items"`
			Skipped    int    `json:"skipped_items"`
			Reason     string `json:"reason"`
		} `json:"data"`
	}](t, payload)
	if second.Data.Sent || second.Data.DeliveryID != nil || second.Data.NewItems != 0 ||
		second.Data.Skipped != 1 || second.Data.Reason != jobs.ReasonAlreadyNotified {
		t.Fatalf("a repeated run must be a no-op: %+v", second.Data)
	}
	if recorder.count() != 1 {
		t.Fatalf("the dedupe must prevent a second delivery, got %d", recorder.count())
	}

	// The overdue item itself is untouched by the notification run.
	if after := env.getItem(overdue.ID); after.Version != overdue.Version {
		t.Fatalf("the notification run modified the item: %+v", after)
	}
}

func TestRunNowErrorMapping(t *testing.T) {
	t.Parallel()

	deliveryID := int64(9)
	permanent := &stubRunner{
		result: jobs.RunResult{DueCount: 1, NewCount: 1, DeliveryID: &deliveryID},
		err: fmt.Errorf("%w: %w", jobs.ErrDeliveryFailed,
			&notify.DeliveryError{Status: 400, Permanent: true, Message: "Invalid Webhook Token"}),
	}
	transient := &stubRunner{
		result: jobs.RunResult{DueCount: 1, NewCount: 1},
		err: fmt.Errorf("%w: %w", jobs.ErrDeliveryFailed,
			&notify.DeliveryError{Message: "transport error", Err: errors.New("connection refused")}),
	}

	cases := map[string]struct {
		runner OverdueRunner
		status int
		code   string
	}{
		"no scheduler": {nil, http.StatusServiceUnavailable, CodeInternal},
		"in progress":  {&stubRunner{err: jobs.ErrRunInProgress}, http.StatusConflict, codeConflict},
		"no webhook":   {&stubRunner{err: jobs.ErrWebhookInvalid}, http.StatusUnprocessableEntity, codeDiscordNotConfigured},
		"rejected":     {permanent, http.StatusUnprocessableEntity, codeDiscordRejected},
		"unreachable":  {transient, http.StatusBadGateway, codeDiscordUnreachable},
		"unexpected":   {&stubRunner{err: errors.New("database exploded")}, http.StatusInternalServerError, CodeInternal},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			env := newTestEnv(t, withScheduler(func(*sql.DB) OverdueRunner { return tc.runner }))

			status, payload := env.admin.do(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{})
			if status != tc.status {
				t.Fatalf("expected %d, got %d (%s)", tc.status, status, payload)
			}
			if code := errorCode(t, payload); code != tc.code {
				t.Fatalf("expected code %q, got %q (%s)", tc.code, code, payload)
			}
			if name == "rejected" {
				details := errorDetails(t, payload)
				if len(details) == 0 || !containsSubstring(details, "delivery 9") {
					t.Fatalf("the failure should reference the recorded delivery: %v", details)
				}
				if containsSubstring(details, "super-secret-token") {
					t.Fatalf("the error leaked the webhook token: %v", details)
				}
			}
			if name == "in progress" && len(tc.runner.(*stubRunner).seen()) == 0 {
				t.Fatal("the runner should have been called once")
			}
		})
	}
}

// TestRunNowReportsCountsWithoutSending keeps the "nothing to do" shape stable:
// due_count reflects the window, sent is false and delivery_id is null.
func TestRunNowReportsCountsWithoutSending(t *testing.T) {
	t.Parallel()
	runner := &stubRunner{result: jobs.RunResult{
		DueCount: 4, NewCount: 0, SkippedCount: 4, Sent: false, Reason: jobs.ReasonAlreadyNotified,
	}}
	env := newTestEnv(t, withScheduler(func(*sql.DB) OverdueRunner { return runner }))

	payload := env.admin.mustDo(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{}, http.StatusOK)
	run := decode[struct {
		Data struct {
			DueCount   int    `json:"due_count"`
			Sent       bool   `json:"sent"`
			DeliveryID *int64 `json:"delivery_id"`
			Skipped    int    `json:"skipped_items"`
			Trigger    string `json:"trigger"`
			Reason     string `json:"reason"`
		} `json:"data"`
	}](t, payload)
	if run.Data.DueCount != 4 || run.Data.Sent || run.Data.DeliveryID != nil || run.Data.Skipped != 4 {
		t.Fatalf("unexpected payload: %+v", run.Data)
	}
	if run.Data.Trigger != string(notify.KindManual) {
		t.Fatalf("unexpected trigger %q", run.Data.Trigger)
	}
	if seen := runner.seen(); len(seen) != 1 || seen[0] != notify.KindManual {
		t.Fatalf("the runner should be called once with the manual trigger, got %v", seen)
	}
}

func TestDeliveryLogPaginationAndAccess(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// A channel row is required by the delivery foreign key.
	env.exec(`INSERT INTO notification_channels (id, type, name, config, enabled, created_at, updated_at)
		VALUES (1, 'discord', 'Discord', '{}', 1, '2026-03-10T10:00:00Z', '2026-03-10T10:00:00Z')`)
	for i := 1; i <= 3; i++ {
		status := "sent"
		var errText any
		var sentAt any = fmt.Sprintf("2026-03-1%dT10:00:05Z", i-1)
		if i == 2 {
			status, errText, sentAt = "failed", "Invalid Webhook Token", nil
		}
		env.exec(`INSERT INTO notification_deliveries
			(channel_id, trigger, status, idempotency_key, item_count, payload, error, created_at, sent_at)
			VALUES (1, 'overdue_daily', ?, ?, ?, '{"asset_tags":[]}', ?, ?, ?)`,
			status, fmt.Sprintf("key-%d", i), i, errText, fmt.Sprintf("2026-03-1%dT10:00:00Z", i-1), sentAt)
	}

	// Newest first, one row per page.
	payload := env.admin.mustDo(http.MethodGet, "/api/v1/notifications?per_page=1&page=2", nil, http.StatusOK)
	list := decode[struct {
		Data []notificationDelivery `json:"data"`
		Meta deliveryListMeta       `json:"meta"`
	}](t, payload)
	if len(list.Data) != 1 || list.Meta.Total != 3 || list.Meta.TotalPages != 3 || list.Meta.Page != 2 {
		t.Fatalf("unexpected pagination: %+v / %+v", list.Data, list.Meta)
	}
	if list.Data[0].CreatedAt != "2026-03-11T10:00:00Z" || list.Data[0].Status != "failed" {
		t.Fatalf("page 2 should hold the middle, failed row: %+v", list.Data[0])
	}
	if list.Data[0].Error == nil || *list.Data[0].Error != "Invalid Webhook Token" || list.Data[0].SentAt != nil {
		t.Fatalf("a failed row must expose the error and no sent_at: %+v", list.Data[0])
	}

	for _, bad := range []string{"?per_page=0", "?per_page=1001", "?page=abc", "?page=0"} {
		if status, body := env.admin.do(http.MethodGet, "/api/v1/notifications"+bad, nil); status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", bad, status, body)
		}
	}

	// Admin-only, like the rest of the notification surface.
	viewer := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	if status, _ := viewer.do(http.MethodGet, "/api/v1/notifications", nil); status != http.StatusUnauthorized {
		t.Fatalf("viewer reading the delivery log: expected 403, got %d", status)
	}
	if status, _ := viewer.do(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{}); status != http.StatusUnauthorized {
		t.Fatalf("viewer triggering a run: expected 403, got %d", status)
	}
	anon := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	if status, _ := anon.do(http.MethodGet, "/api/v1/notifications", nil); status != http.StatusUnauthorized {
		t.Fatalf("anonymous access to the log: expected 401, got %d", status)
	}

	// Mutations still require the CSRF header.
	noCSRF := &sessionClient{t: t, server: env.server, client: env.admin.client}
	if status, payload := noCSRF.do(http.MethodPost, "/api/v1/notifications/run-now", map[string]any{}); status != http.StatusForbidden {
		t.Fatalf("run-now without CSRF: expected 403, got %d (%s)", status, payload)
	}
}

// containsSubstring reports whether any entry contains the needle.
func containsSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
