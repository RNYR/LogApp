package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sklad/server/internal/audit"
	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
	"sklad/server/internal/store"
)

const (
	testWebhook = "https://discord.com/api/webhooks/1234567890123456789/super-secret-token"
	testDay     = "2026-03-15"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// fakeNotifier stands in for Discord: it records reports, can fail on demand and
// can block so that overlapping runs can be tested.
type fakeNotifier struct {
	mu      sync.Mutex
	reports []notify.Report
	result  notify.Result
	err     error

	block   chan struct{} // when set, Send waits for a receive or cancellation
	entered chan struct{} // closed when Send is entered
}

func (f *fakeNotifier) Name() string { return "fake" }

func (f *fakeNotifier) Send(ctx context.Context, report notify.Report) (notify.Result, error) {
	if f.entered != nil {
		select {
		case <-f.entered:
		default:
			close(f.entered)
		}
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return notify.Result{}, ctx.Err()
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.reports = append(f.reports, report)
	if f.err != nil {
		return f.result, f.err
	}
	result := f.result
	if result.Messages == 0 {
		result.Messages = 1
		result.Embeds = len(report.Items) + 1
		result.MessageIDs = []string{"42"}
	}
	return result, nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reports)
}

func (f *fakeNotifier) last() notify.Report {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.reports) == 0 {
		return notify.Report{}
	}
	return f.reports[len(f.reports)-1]
}

type jobsEnv struct {
	t        *testing.T
	db       *sql.DB
	fake     *fakeNotifier
	fixedNow time.Time
	ownerID  int64
}

func newJobsEnv(t *testing.T) *jobsEnv {
	t.Helper()
	dir := t.TempDir()
	database, err := store.Open(context.Background(), store.Options{
		DBPath:  filepath.Join(dir, "sklad.db"),
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	env := &jobsEnv{
		t:        t,
		db:       database.DB(),
		fake:     &fakeNotifier{},
		fixedNow: time.Date(2026, 3, 15, 11, 30, 0, 0, time.UTC),
	}

	// settings.updated_by has a foreign key, so the test needs a user.
	res, err := env.db.Exec(`INSERT INTO users (username, display_name, email, password_hash, role, must_change_password, active, created_at, updated_at)
		VALUES ('admin','Admin','','x','admin',0,1,'2026-03-01T00:00:00Z','2026-03-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if env.ownerID, err = res.LastInsertId(); err != nil {
		t.Fatalf("user id: %v", err)
	}

	now := "2026-03-01T00:00:00Z"
	env.exec(`INSERT INTO item_types (value, label_en, label_cs, label_it, sort_order, created_at, updated_at)
		VALUES ('Camera','Camera','Kamera','Telecamera',0,?,?)`, now, now)
	env.exec(`INSERT INTO statuses (value, label_en, label_cs, label_it, color, sort_order, created_at, updated_at)
		VALUES ('Loaned','Loaned','Půjčeno','Prestato','#e67e22',0,?,?)`, now, now)
	return env
}

func (e *jobsEnv) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(query, args...); err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

// seedSettings stores the runtime configuration the scheduler reads.
func (e *jobsEnv) seedSettings(webhook string, enabled bool, locale string, checkHour, daysAhead int) {
	e.t.Helper()
	owner := e.ownerID
	if err := settings.Save(context.Background(), e.db, settings.Settings{
		DiscordWebhookURL: webhook,
		DiscordEnabled:    enabled,
		CheckHour:         checkHour,
		DaysAhead:         daysAhead,
		DefaultLocale:     locale,
		OrganizationName:  "PEKAT VISION",
	}, &owner); err != nil {
		e.t.Fatalf("settings.Save: %v", err)
	}
}

// insertItem adds an inventory row with the given return date.
func (e *jobsEnv) insertItem(tag, name, returnDate string, archived bool) int64 {
	e.t.Helper()
	var typeID, statusID int64
	if err := e.db.QueryRow(`SELECT id FROM item_types WHERE value = 'Camera'`).Scan(&typeID); err != nil {
		e.t.Fatalf("type id: %v", err)
	}
	if err := e.db.QueryRow(`SELECT id FROM statuses WHERE value = 'Loaned'`).Scan(&statusID); err != nil {
		e.t.Fatalf("status id: %v", err)
	}
	var ret any
	if returnDate != "" {
		ret = returnDate
	}
	res, err := e.db.Exec(`INSERT INTO items
		(asset_tag, name, sn, type_id, status_id, sent_date, return_date, note, version, archived, created_at, updated_at)
		VALUES (?, ?, 'SN-1', ?, ?, '2026-02-01', ?, 'on site', 1, ?, '2026-02-01T00:00:00Z', '2026-02-01T00:00:00Z')`,
		tag, name, typeID, statusID, ret, boolToInt(archived))
	if err != nil {
		e.t.Fatalf("insert item %s: %v", tag, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		e.t.Fatalf("item id: %v", err)
	}
	return id
}

// newScheduler builds a scheduler around a recording notifier.
func (e *jobsEnv) newScheduler(maxItems int) *Scheduler {
	e.t.Helper()
	return e.newSchedulerWith(e.fake, maxItems, DefaultTick)
}

func (e *jobsEnv) newSchedulerWith(notifier notify.Notifier, maxItems int, tick time.Duration) *Scheduler {
	e.t.Helper()
	scheduler, err := New(Options{
		DB:         e.db,
		Audit:      audit.New(e.db),
		Location:   time.UTC,
		AppURL:     "https://sklad.local",
		Now:        func() time.Time { return e.fixedNow },
		MaxItems:   maxItems,
		Tick:       tick,
		RunTimeout: 5 * time.Second,
		Notifier:   func(string) (notify.Notifier, error) { return notifier, nil },
	})
	if err != nil {
		e.t.Fatalf("jobs.New: %v", err)
	}
	return scheduler
}

type deliveryRow struct {
	ID             int64
	ChannelID      int64
	Trigger        string
	Status         string
	IdempotencyKey string
	ItemCount      int
	Payload        string
	Error          sql.NullString
	SentAt         sql.NullString
	CreatedAt      string
}

func (e *jobsEnv) deliveries() []deliveryRow {
	e.t.Helper()
	rows, err := e.db.Query(`SELECT id, channel_id, trigger, status, idempotency_key, item_count, payload, error, sent_at, created_at
		FROM notification_deliveries ORDER BY id`)
	if err != nil {
		e.t.Fatalf("deliveries: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []deliveryRow
	for rows.Next() {
		var row deliveryRow
		if err := rows.Scan(&row.ID, &row.ChannelID, &row.Trigger, &row.Status, &row.IdempotencyKey,
			&row.ItemCount, &row.Payload, &row.Error, &row.SentAt, &row.CreatedAt); err != nil {
			e.t.Fatalf("scan delivery: %v", err)
		}
		out = append(out, row)
	}
	return out
}

func (e *jobsEnv) payloadTags(payload string) []string {
	e.t.Helper()
	var decoded struct {
		AssetTags []string `json:"asset_tags"`
	}
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		e.t.Fatalf("decode payload: %v (%s)", err, payload)
	}
	return decoded.AssetTags
}

func (e *jobsEnv) auditNotifyCount() int {
	e.t.Helper()
	var count int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action = 'notify'`).Scan(&count); err != nil {
		e.t.Fatalf("audit count: %v", err)
	}
	return count
}

// ---------------------------------------------------------------------------
// One run
// ---------------------------------------------------------------------------

func TestRunSendsDueItemsAndRecordsDelivery(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-10", false) // 5 days overdue
	env.insertItem("PV-002", "Camera B", testDay, false)      // due today
	env.insertItem("PV-003", "Camera C", "2026-04-01", false)
	env.insertItem("PV-004", "Archived", "2026-03-01", true)
	env.insertItem("PV-005", "No due date", "", false)

	result, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DueCount != 2 || result.NewCount != 2 || result.SkippedCount != 0 || !result.Sent {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.DeliveryID == nil {
		t.Fatal("the run should have reserved a delivery row")
	}
	if env.fake.count() != 1 {
		t.Fatalf("expected one delivery attempt, got %d", env.fake.count())
	}

	report := env.fake.last()
	if report.Kind != notify.KindOverdueDaily || len(report.Items) != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Items[0].AssetTag != "PV-001" || report.Items[1].AssetTag != "PV-002" {
		t.Fatalf("items must be most-overdue first: %+v", report.Items)
	}
	if report.Items[0].DaysOverdue != 5 || report.Items[1].DaysOverdue != 0 {
		t.Fatalf("day counts wrong: %d / %d", report.Items[0].DaysOverdue, report.Items[1].DaysOverdue)
	}
	if report.Items[0].Type != "Camera" || report.Items[0].Status != "Loaned" || report.Items[0].StatusColor != "#e67e22" {
		t.Fatalf("labels/colour missing: %+v", report.Items[0])
	}
	if report.Items[0].Note != "on site" || report.Items[0].ReturnDate != "2026-03-10" {
		t.Fatalf("note/date missing: %+v", report.Items[0])
	}
	if report.Organization != "PEKAT VISION" || report.AppURL != "https://sklad.local" || report.Locale != "en" {
		t.Fatalf("report metadata wrong: %+v", report)
	}
	if !report.GeneratedAt.Equal(env.fixedNow) {
		t.Fatalf("the report must use the injected clock, got %s", report.GeneratedAt)
	}

	rows := env.deliveries()
	if len(rows) != 1 {
		t.Fatalf("expected one delivery row, got %d", len(rows))
	}
	row := rows[0]
	if row.Trigger != "overdue_daily" || row.Status != statusSent || row.ItemCount != 2 {
		t.Fatalf("unexpected delivery row: %+v", row)
	}
	if !row.SentAt.Valid || row.Error.Valid {
		t.Fatalf("a sent delivery has sent_at and no error: %+v", row)
	}
	if !strings.HasPrefix(row.IdempotencyKey, keyPrefix+":"+testDay+":") {
		t.Fatalf("unexpected idempotency key %q", row.IdempotencyKey)
	}
	if tags := env.payloadTags(row.Payload); len(tags) != 2 || tags[0] != "PV-001" {
		t.Fatalf("the payload must carry the asset tags (the dedupe depends on it): %v", tags)
	}
	if row.ChannelID == 0 {
		t.Fatal("the delivery must reference a channel")
	}
	if channels := env.countRows(`SELECT COUNT(*) FROM notification_channels WHERE type = 'discord'`); channels != 1 {
		t.Fatalf("expected exactly one discord channel row, got %d", channels)
	}
	if env.auditNotifyCount() != 1 {
		t.Fatalf("expected one notify audit row, got %d", env.auditNotifyCount())
	}
}

func (e *jobsEnv) countRows(query string, args ...any) int {
	e.t.Helper()
	var count int
	if err := e.db.QueryRow(query, args...).Scan(&count); err != nil {
		e.t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

// ---------------------------------------------------------------------------
// Idempotency and dedupe
// ---------------------------------------------------------------------------

func TestRunIsIdempotentAcrossRestarts(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	scheduler := env.newScheduler(0)
	if _, err := scheduler.Run(context.Background(), notify.KindOverdueDaily); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Same process, same day: nothing new to send.
	result, err := scheduler.Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if result.Sent || result.NewCount != 0 || result.SkippedCount != 1 || result.Reason != ReasonAlreadyNotified {
		t.Fatalf("second run should be a no-op: %+v", result)
	}
	if result.DueCount != 1 {
		t.Fatalf("the due count must still report the window: %+v", result)
	}

	// Restart: a fresh scheduler on the same database must not resend.
	restarted := env.newScheduler(0)
	result, err = restarted.Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("run after restart: %v", err)
	}
	if result.Sent || result.NewCount != 0 {
		t.Fatalf("a restart must not resend: %+v", result)
	}
	if env.fake.count() != 1 {
		t.Fatalf("expected exactly one delivery across restarts, got %d", env.fake.count())
	}
	if rows := env.deliveries(); len(rows) != 1 {
		t.Fatalf("expected exactly one delivery row, got %d", len(rows))
	}
	if env.auditNotifyCount() != 1 {
		t.Fatalf("expected one notify audit row, got %d", env.auditNotifyCount())
	}
}

func TestNewItemLaterTheSameDayIsReported(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	scheduler := env.newScheduler(0)
	if _, err := scheduler.Run(context.Background(), notify.KindOverdueDaily); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// A second item becomes overdue later the same day.
	env.insertItem("PV-002", "Camera B", testDay, false)
	result, err := scheduler.Run(context.Background(), notify.KindManual)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !result.Sent || result.NewCount != 1 || result.SkippedCount != 1 || result.DueCount != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	report := env.fake.last()
	if len(report.Items) != 1 || report.Items[0].AssetTag != "PV-002" {
		t.Fatalf("only the new item may be reported: %+v", report.Items)
	}
	rows := env.deliveries()
	if len(rows) != 2 || rows[1].Trigger != "manual" || rows[1].ItemCount != 1 {
		t.Fatalf("unexpected delivery rows: %+v", rows)
	}
	if rows[0].IdempotencyKey == rows[1].IdempotencyKey {
		t.Fatal("different item sets must produce different idempotency keys")
	}
}

func TestNothingDueDoesNotSend(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera", "2026-05-01", false)

	result, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Sent || result.DueCount != 0 || result.Reason != ReasonNothingDue {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(env.deliveries()) != 0 || env.fake.count() != 0 {
		t.Fatal("nothing due must not create a delivery row or a message")
	}
}

// ---------------------------------------------------------------------------
// Failures
// ---------------------------------------------------------------------------

func TestFailedDeliveryIsRecordedAndRetriedInPlace(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	env.fake.err = &notify.DeliveryError{Status: 400, Permanent: true, Message: "Invalid Webhook Token"}

	scheduler := env.newScheduler(0)
	result, err := scheduler.Run(context.Background(), notify.KindOverdueDaily)
	if err == nil {
		t.Fatal("expected a delivery failure")
	}
	if !errors.Is(err, ErrDeliveryFailed) || !notify.IsPermanent(err) || notify.Status(err) != 400 {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.DeliveryID == nil {
		t.Fatal("a failed attempt must still be recorded")
	}
	rows := env.deliveries()
	if len(rows) != 1 || rows[0].Status != statusFailed || !rows[0].Error.Valid || rows[0].SentAt.Valid {
		t.Fatalf("unexpected failed row: %+v", rows)
	}
	if !strings.Contains(rows[0].Error.String, "Invalid Webhook Token") {
		t.Fatalf("the failure reason should be stored, got %q", rows[0].Error.String)
	}
	if strings.Contains(rows[0].Error.String, "super-secret-token") {
		t.Fatalf("the stored error leaked the webhook token: %q", rows[0].Error.String)
	}
	if env.auditNotifyCount() != 1 {
		t.Fatalf("a failed attempt must be audited, got %d rows", env.auditNotifyCount())
	}

	// The failure is not "already notified": a retry reuses the row and sends.
	env.fake.err = nil
	result, err = scheduler.Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !result.Sent || result.NewCount != 1 {
		t.Fatalf("retry result: %+v", result)
	}
	rows = env.deliveries()
	if len(rows) != 1 {
		t.Fatalf("a retry must reuse the delivery row, got %d rows", len(rows))
	}
	if rows[0].Status != statusSent || rows[0].Error.Valid || !rows[0].SentAt.Valid {
		t.Fatalf("unexpected retried row: %+v", rows[0])
	}
	if !strings.Contains(rows[0].Payload, "PV-001") {
		t.Fatalf("the retried payload must keep the asset tags: %s", rows[0].Payload)
	}
}

func TestTransientFailureIsRecorded(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	env.fake.err = &notify.DeliveryError{Message: "transport error", Err: errors.New("connection refused")}
	result, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily)
	if err == nil || notify.IsPermanent(err) {
		t.Fatalf("expected a transient failure, got %v", err)
	}
	if result.Sent {
		t.Fatalf("nothing should have been delivered: %+v", result)
	}
	rows := env.deliveries()
	if len(rows) != 1 || rows[0].Status != statusFailed {
		t.Fatalf("unexpected row: %+v", rows)
	}
}

func TestWebhookMustBeConfigured(t *testing.T) {
	t.Parallel()

	env := newJobsEnv(t)
	env.seedSettings("", true, "en", 11, 0)
	env.insertItem("PV-001", "Camera", "2026-03-01", false)
	if _, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily); !errors.Is(err, ErrWebhookInvalid) {
		t.Fatalf("expected ErrWebhookInvalid without a webhook, got %v", err)
	}
	if len(env.deliveries()) != 0 {
		t.Fatal("a missing webhook must not create a delivery row")
	}

	env.seedSettings("https://example.com/not-a-webhook", true, "en", 11, 0)
	if _, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily); !errors.Is(err, ErrWebhookInvalid) {
		t.Fatalf("expected ErrWebhookInvalid for an unusable URL, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Windowing, truncation and localization
// ---------------------------------------------------------------------------

func TestTruncationAndDaysAheadWindow(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 7)
	for i := 1; i <= 5; i++ {
		env.insertItem(fmt.Sprintf("PV-%03d", i), "Camera", fmt.Sprintf("2026-03-%02d", i), false)
	}
	env.insertItem("PV-100", "Soon", "2026-03-20", false) // inside the days_ahead window
	env.insertItem("PV-200", "Later", "2026-04-30", false)

	result, err := env.newScheduler(3).Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.DueCount != 6 || result.NewCount != 3 || !result.Truncated {
		t.Fatalf("unexpected result: %+v", result)
	}
	report := env.fake.last()
	if len(report.Items) != 3 || report.TotalItems != 6 {
		t.Fatalf("report should be capped but honest: %d items, total %d", len(report.Items), report.TotalItems)
	}
	for _, item := range report.Items {
		if item.AssetTag == "PV-200" {
			t.Fatal("an item beyond the window must not be reported")
		}
	}
	if tags := env.payloadTags(env.deliveries()[0].Payload); len(tags) != 3 {
		t.Fatalf("the dedupe payload must match the report: %v", tags)
	}
	if env.deliveries()[0].ItemCount != 3 {
		t.Fatalf("item_count should reflect the sent items")
	}
}

func TestLocalizedLabelsFollowTheSettings(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "cs", 11, 0)
	env.insertItem("PV-001", "Kamera", "2026-03-01", false)

	if _, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily); err != nil {
		t.Fatalf("Run: %v", err)
	}
	item := env.fake.last().Items[0]
	if item.Type != "Kamera" || item.Status != "Půjčeno" {
		t.Fatalf("Czech option labels missing: %+v", item)
	}
}

// ---------------------------------------------------------------------------
// Run serialization and the daily loop
// ---------------------------------------------------------------------------

func TestRunInProgressIsRejected(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	blocked := &fakeNotifier{block: make(chan struct{}), entered: make(chan struct{})}
	scheduler := env.newSchedulerWith(blocked, 0, DefaultTick)

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := scheduler.Run(context.Background(), notify.KindManual); err != nil {
			t.Errorf("blocked run: %v", err)
		}
	}()
	select {
	case <-blocked.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the delivery never started")
	}

	if _, err := scheduler.Run(context.Background(), notify.KindManual); !errors.Is(err, ErrRunInProgress) {
		t.Fatalf("expected ErrRunInProgress, got %v", err)
	}

	close(blocked.block)
	<-done
}

func TestStartRunsOnceAtTheCheckHour(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	scheduler := env.newSchedulerWith(env.fake, 0, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Start(ctx)
	}()

	// The clock is fixed at 11:30 with check_hour = 11: the immediate catch-up
	// check must run, and the per-day guard must prevent repeats.
	deadline := time.After(3 * time.Second)
	for env.fake.count() == 0 {
		select {
		case <-deadline:
			cancel()
			t.Fatalf("the scheduler never ran")
		case <-time.After(2 * time.Millisecond):
		}
	}
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	if env.fake.count() != 1 {
		t.Fatalf("expected exactly one scheduled run, got %d", env.fake.count())
	}
	if env.auditNotifyCount() != 1 {
		t.Fatalf("expected one notify audit row, got %d", env.auditNotifyCount())
	}
}

func TestStartSkipsDisabledAndCatchesUpWhenEnabled(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, false, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	// Disabled: the loop must not send.
	disabled := env.newSchedulerWith(env.fake, 0, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		disabled.Start(ctx)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	if env.fake.count() != 0 {
		t.Fatalf("a disabled scheduler must not send, got %d", env.fake.count())
	}

	// Enabled later the same day: the next process catches up.
	env.seedSettings(testWebhook, true, "en", 11, 0)
	enabled := env.newSchedulerWith(env.fake, 0, 5*time.Millisecond)
	ctx2, cancel2 := context.WithCancel(context.Background())
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		enabled.Start(ctx2)
	}()
	deadline := time.After(3 * time.Second)
	for env.fake.count() == 0 {
		select {
		case <-deadline:
			cancel2()
			t.Fatalf("the enabled scheduler never caught up")
		case <-time.After(2 * time.Millisecond):
		}
	}
	cancel2()
	<-done2
	if env.fake.count() != 1 {
		t.Fatalf("expected one catch-up run, got %d", env.fake.count())
	}
}

func TestBeforeTheCheckHourNothingRuns(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 23, 0) // 23:00; the clock says 11:30
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	scheduler := env.newSchedulerWith(env.fake, 0, 5*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Start(ctx)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done

	if env.fake.count() != 0 || len(env.deliveries()) != 0 {
		t.Fatal("nothing may run before the configured hour")
	}
}

// TestSchedulerClosesItsAuditTrail keeps the audit shape pinned: the delivery
// attempt is attributed to the system (no actor) and records the counts.
func TestSchedulerAuditsWithoutSecrets(t *testing.T) {
	t.Parallel()
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera A", "2026-03-01", false)

	if _, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var (
		actor sql.NullInt64
		after string
	)
	if err := env.db.QueryRow(`SELECT actor_user_id, after FROM audit_log WHERE action = 'notify'`).
		Scan(&actor, &after); err != nil {
		t.Fatalf("read notify audit row: %v", err)
	}
	if actor.Valid {
		t.Fatalf("a scheduled delivery has no human actor, got %d", actor.Int64)
	}
	for _, want := range []string{`"trigger":"overdue_daily"`, `"sent":true`, `"new":1`} {
		if !strings.Contains(after, want) {
			t.Fatalf("audit payload missing %s: %s", want, after)
		}
	}
	if strings.Contains(after, "super-secret-token") {
		t.Fatalf("audit payload leaked the webhook token: %s", after)
	}
}

// TestNewRequiresDB documents the constructor contract.
func TestNewRequiresDB(t *testing.T) {
	t.Parallel()
	if _, err := New(Options{}); err == nil {
		t.Fatal("New should require Options.DB")
	}
	if _, err := New(Options{DB: nil, Logger: nil}); err == nil {
		t.Fatal("New should require Options.DB even with a logger")
	}
	// A nil logger is replaced, not dereferenced later.
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-001", "Camera", "2026-03-01", false)
	scheduler, err := New(Options{
		DB:       env.db,
		Location: time.UTC,
		Now:      func() time.Time { return env.fixedNow },
		Notifier: func(string) (notify.Notifier, error) { return env.fake, nil },
	})
	if err != nil {
		t.Fatalf("jobs.New with defaults: %v", err)
	}
	if _, err := scheduler.Run(context.Background(), notify.KindOverdueDaily); err != nil {
		t.Fatalf("Run with a nil logger/audit: %v", err)
	}
}

func TestReturnedItemsAreNotDue(t *testing.T) {
	env := newJobsEnv(t)
	env.seedSettings(testWebhook, true, "en", 11, 0)
	env.insertItem("PV-RETURNED", "Returned camera", "2026-03-01", false)
	env.insertItem("PV-RENTED", "Rented camera", "2026-03-01", false)
	env.exec(`UPDATE items SET status_id=(SELECT id FROM statuses WHERE value='Feasibility') WHERE asset_tag='PV-RETURNED'`)
	result, err := env.newScheduler(0).Run(context.Background(), notify.KindOverdueDaily)
	if err != nil {
		t.Fatal(err)
	}
	if result.DueCount != 1 || len(env.fake.last().Items) != 1 || env.fake.last().Items[0].AssetTag != "PV-RENTED" {
		t.Fatalf("returned item reported overdue: %+v", result)
	}
}
