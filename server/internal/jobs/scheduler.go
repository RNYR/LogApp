// Package jobs hosts Sklad's background work: today the overdue notification
// scheduler, which posts a Discord report of equipment that is due or overdue.
//
// The scheduler is deliberately conservative about sending:
//
//   - it runs at the hour configured in the database (scheduler.check_hour) in
//     the configured time zone, and catches up after a restart;
//   - an item is reported at most once per calendar day (per-item/per-day
//     dedupe), computed from the payloads of the day's successful deliveries;
//   - a delivery row is inserted as `pending` before the HTTP request and
//     updated afterwards, and its idempotency key makes a repeated run of the
//     same item set a no-op — so completed sends survive restarts and retries;
//   - runs are serialized: a tick that fires while a manual run is in flight is
//     skipped instead of queueing.
package jobs

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sklad/server/internal/statusnames"
	"sort"
	"strings"
	"sync"
	"time"

	"sklad/server/internal/audit"
	"sklad/server/internal/notify"
	"sklad/server/internal/settings"
)

// Defaults. They are exported so the deployment documentation and tests can
// refer to them instead of repeating literals.
const (
	// DefaultTick is how often the scheduler re-evaluates whether a run is due.
	DefaultTick = time.Minute
	// DefaultMaxItems caps one report; a bigger inventory must be triaged rather
	// than turned into hundreds of Discord messages.
	DefaultMaxItems = 500
	// DefaultRunTimeout bounds one full check including the HTTP delivery.
	DefaultRunTimeout = 2 * time.Minute
	// keyPrefix namespaces delivery idempotency keys. Both triggers share it on
	// purpose: per-item/per-day dedupe applies to scheduled and manual runs
	// alike, so a manual run never re-notifies today's items.
	keyPrefix = "overdue"
)

// Delivery statuses (notification_deliveries.status).
const (
	statusPending = "pending"
	statusSent    = "sent"
	statusFailed  = "failed"
)

// Sentinel errors returned by Run.
var (
	// ErrRunInProgress means another check is still running.
	ErrRunInProgress = errors.New("notification run already in progress")
	// ErrWebhookInvalid means no usable Discord webhook is configured.
	ErrWebhookInvalid = errors.New("discord webhook is not configured")
	// ErrDeliveryFailed means the report could not be delivered; the wrapped
	// notify.DeliveryError carries the details.
	ErrDeliveryFailed = errors.New("notification delivery failed")
)

// Options configures New. DB is required.
type Options struct {
	// DB is the SQLite pool.
	DB *sql.DB
	// Logger receives run summaries; nil selects a discarding logger.
	Logger *slog.Logger
	// Audit records notification attempts (nil disables auditing).
	Audit *audit.Writer
	// Location is the time zone used for the daily hour and the day boundary
	// (nil uses UTC). It should be config.Config.Location.
	Location *time.Location
	// AppURL links the report back to the app (config.Config.BaseURL).
	AppURL string
	// Now supplies the current time; nil selects time.Now.
	Now func() time.Time
	// Tick is how often the loop re-checks; nil selects DefaultTick.
	Tick time.Duration
	// MaxItems caps one report (0 selects DefaultMaxItems).
	MaxItems int
	// RunTimeout bounds one check (0 selects DefaultRunTimeout).
	RunTimeout time.Duration
	// Notifier builds the delivery client for a webhook URL; nil selects
	// notify.NewDiscord. Tests inject a recording implementation.
	Notifier func(webhookURL string) (notify.Notifier, error)
}

// Scheduler runs the overdue check on a schedule and on demand.
type Scheduler struct {
	db         *sql.DB
	log        *slog.Logger
	audit      *audit.Writer
	loc        *time.Location
	appURL     string
	now        func() time.Time
	tick       time.Duration
	maxItems   int
	runTimeout time.Duration
	notifier   func(webhookURL string) (notify.Notifier, error)

	// mu serializes runs; lastRun* remember what the loop already decided today
	// so the minute-by-minute check does not re-query the database.
	mu             sync.Mutex
	lastRunDate    string
	lastRunEnabled bool
}

// New validates the options and builds a scheduler.
func New(opts Options) (*Scheduler, error) {
	if opts.DB == nil {
		return nil, errors.New("jobs: Options.DB is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	location := opts.Location
	if location == nil {
		location = time.UTC
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	tick := opts.Tick
	if tick <= 0 {
		tick = DefaultTick
	}
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = DefaultMaxItems
	}
	runTimeout := opts.RunTimeout
	if runTimeout <= 0 {
		runTimeout = DefaultRunTimeout
	}
	notifier := opts.Notifier
	if notifier == nil {
		notifier = func(webhookURL string) (notify.Notifier, error) {
			return notify.NewDiscord(notify.DiscordOptions{WebhookURL: webhookURL, Now: now})
		}
	}
	return &Scheduler{
		db:         opts.DB,
		log:        log,
		audit:      opts.Audit,
		loc:        location,
		appURL:     opts.AppURL,
		now:        now,
		tick:       tick,
		maxItems:   maxItems,
		runTimeout: runTimeout,
		notifier:   notifier,
	}, nil
}

// RunResult describes what one overdue check did. It is the payload of
// POST /api/v1/notifications/run-now.
type RunResult struct {
	// Trigger is the recorded trigger (overdue_daily or manual).
	Trigger notify.Kind `json:"trigger"`
	// DueCount is every item inside the overdue window, before per-day dedupe.
	DueCount int `json:"due_count"`
	// NewCount is how many items this report actually contained.
	NewCount int `json:"new_items"`
	// SkippedCount is how many due items were already notified today.
	SkippedCount int `json:"skipped_items"`
	// Sent reports whether a message was delivered.
	Sent bool `json:"sent"`
	// DeliveryID identifies the notification_deliveries row, when one was used.
	DeliveryID *int64 `json:"delivery_id"`
	// Messages and Embeds describe the delivery.
	Messages int `json:"messages,omitempty"`
	Embeds   int `json:"embeds,omitempty"`
	// Truncated reports that MaxItems clipped the report.
	Truncated bool `json:"truncated,omitempty"`
	// Reason explains why nothing was sent (nothing_due, already_notified).
	Reason string `json:"reason,omitempty"`
}

// Reasons reported in RunResult.Reason.
const (
	ReasonNothingDue      = "nothing_due"
	ReasonAlreadyNotified = "already_notified"
)

// Run performs one overdue check.
//
// The trigger is recorded on the delivery row: pass notify.KindOverdueDaily from
// the loop and notify.KindManual from the admin endpoint. Per-item/per-day
// dedupe applies to both, so a manual run never re-notifies an item that was
// already reported today.
func (s *Scheduler) Run(ctx context.Context, trigger notify.Kind) (RunResult, error) {
	if !s.mu.TryLock() {
		return RunResult{Trigger: trigger}, ErrRunInProgress
	}
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, s.runTimeout)
	defer cancel()

	return s.run(ctx, trigger)
}

// Start runs the scheduler until ctx is cancelled. It checks immediately (so a
// restart after the configured hour does not lose the day's report) and then
// every Tick.
func (s *Scheduler) Start(ctx context.Context) {
	s.log.Info("overdue scheduler started",
		"tick", s.tick.String(),
		"timezone", s.loc.String(),
	)

	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	for {
		if s.tickDue(ctx) {
			s.runDaily(ctx)
		}
		select {
		case <-ctx.Done():
			s.log.Info("overdue scheduler stopped")
			return
		case <-ticker.C:
		}
	}
}

// runDaily performs the scheduled check, logging its outcome.
func (s *Scheduler) runDaily(ctx context.Context) {
	result, err := s.Run(ctx, notify.KindOverdueDaily)
	switch {
	case errors.Is(err, ErrRunInProgress):
		s.log.Debug("overdue check skipped: another run is in progress")
	case err != nil:
		s.log.Error("overdue check failed", "error", err)
	default:
		s.log.Info("overdue check completed",
			"due", result.DueCount,
			"new", result.NewCount,
			"skipped", result.SkippedCount,
			"sent", result.Sent,
			"reason", result.Reason,
		)
	}
}

// tickDue reports whether the loop should perform today's check now: the
// configured hour has arrived and this process has not already handled today
// while notifications were enabled.
func (s *Scheduler) tickDue(ctx context.Context) bool {
	current, err := settings.Load(ctx, s.db)
	if err != nil {
		s.log.Error("overdue check: cannot read settings", "error", err)
		return false
	}

	s.logSettingsProblems(current)

	now := s.now().In(s.loc)
	today := now.Format(dateLayout)

	if !current.DiscordEnabled || !current.WebhookConfigured() {
		// Remember the decision once per day so a disabled scheduler does not
		// re-read the settings every minute, but stay ready to run if the
		// administrator enables notifications later today.
		if s.lastRunDate != today || s.lastRunEnabled {
			s.lastRunDate = today
			s.lastRunEnabled = false
			s.log.Debug("overdue check disabled", "discord_enabled", current.DiscordEnabled,
				"webhook_set", current.WebhookConfigured())
		}
		return false
	}
	if now.Hour() < current.CheckHour {
		return false
	}
	if s.lastRunDate == today && s.lastRunEnabled {
		return false
	}

	s.lastRunDate = today
	s.lastRunEnabled = true
	return true
}

// logSettingsProblems warns about settings rows that could not be decoded; the
// loader substitutes defaults so an admin can repair them through the API.
func (s *Scheduler) logSettingsProblems(current settings.Settings) {
	for _, problem := range current.Problems {
		s.log.Warn("unreadable setting", "problem", problem)
	}
}

// run executes one check inside the caller's lock.
func (s *Scheduler) run(ctx context.Context, trigger notify.Kind) (RunResult, error) {
	result := RunResult{Trigger: trigger}

	current, err := settings.Load(ctx, s.db)
	if err != nil {
		return result, fmt.Errorf("jobs: load settings: %w", err)
	}
	if !current.WebhookConfigured() {
		return result, ErrWebhookInvalid
	}
	webhook, err := s.webhookURL(current)
	if err != nil {
		return result, err
	}

	s.logSettingsProblems(current)

	now := s.now().In(s.loc)
	today := now.Format(dateLayout)

	due, err := s.dueItems(ctx, current, today)
	if err != nil {
		return result, err
	}
	result.DueCount = len(due)
	if len(due) == 0 {
		result.Reason = ReasonNothingDue
		return result, nil
	}

	notified, err := s.notifiedToday(ctx, now)
	if err != nil {
		return result, err
	}
	fresh := make([]dueItem, 0, len(due))
	for _, item := range due {
		if _, already := notified[strings.ToUpper(item.assetTag)]; already {
			continue
		}
		fresh = append(fresh, item)
	}
	result.SkippedCount = len(due) - len(fresh)
	if len(fresh) == 0 {
		result.Reason = ReasonAlreadyNotified
		return result, nil
	}
	if len(fresh) > s.maxItems {
		result.Truncated = true
		fresh = fresh[:s.maxItems]
	}
	result.NewCount = len(fresh)

	report := notify.Report{
		Kind:         trigger,
		Items:        notifiedItems(fresh),
		TotalItems:   len(due),
		GeneratedAt:  now,
		Organization: current.OrganizationName,
		AppURL:       s.appURL,
		Locale:       current.DefaultLocale,
	}

	// Reserve the delivery before sending so a concurrent or restarted
	// scheduler cannot send the same report twice.
	key := idempotencyKey(today, fresh)
	payload := deliveryPayload{
		AssetTags: assetTags(fresh),
		DueCount:  result.DueCount,
		Skipped:   result.SkippedCount,
		Truncated: result.Truncated,
	}
	deliveryID, alreadySent, err := s.reserveDelivery(ctx, webhook, trigger, key, current, payload)
	if err != nil {
		return result, err
	}
	result.DeliveryID = &deliveryID
	if alreadySent {
		// Defensive: the per-day dedupe should already have filtered these items.
		// DeliveryID refers to the row of the earlier, successful delivery.
		result.Reason = ReasonAlreadyNotified
		return result, nil
	}

	notifier, err := s.notifier(webhook)
	if err != nil {
		s.completeDelivery(ctx, deliveryID, statusFailed, payload, err)
		return result, fmt.Errorf("%w: %w", ErrDeliveryFailed, &notify.DeliveryError{
			Permanent: true,
			Message:   "the stored Discord webhook is not usable",
			Err:       err,
		})
	}

	sendResult, sendErr := notifier.Send(ctx, report)
	result.Messages = sendResult.Messages
	result.Embeds = sendResult.Embeds
	payload.Messages = sendResult.Messages
	payload.MessageIDs = sendResult.MessageIDs

	if sendErr != nil {
		s.completeDelivery(ctx, deliveryID, statusFailed, payload, sendErr)
		s.auditRun(ctx, trigger, &result, sendErr)
		return result, fmt.Errorf("%w: %w", ErrDeliveryFailed, sendErr)
	}

	result.Sent = true
	s.completeDelivery(ctx, deliveryID, statusSent, payload, nil)
	s.auditRun(ctx, trigger, &result, nil)
	return result, nil
}

// webhookURL returns the enabled webhook, or ErrWebhookInvalid.
func (s *Scheduler) webhookURL(current settings.Settings) (string, error) {
	webhook := strings.TrimSpace(current.DiscordWebhookURL)
	if webhook == "" {
		return "", ErrWebhookInvalid
	}
	if err := notify.ValidateWebhookURL(webhook); err != nil {
		return "", fmt.Errorf("%w: %v", ErrWebhookInvalid, err)
	}
	return webhook, nil
}

// dueItem is one overdue inventory row, ready to be rendered.
type dueItem struct {
	id          int64
	assetTag    string
	name        string
	sn          string
	typeLabel   string
	statusLabel string
	statusColor string
	sentDate    string
	returnDate  string
	note        string
	daysOverdue int
}

// dueItems lists the items inside the overdue window (return_date <= today +
// days_ahead), most overdue first.
func (s *Scheduler) dueItems(ctx context.Context, current settings.Settings, today string) ([]dueItem, error) {
	limit := today
	if current.DaysAhead > 0 {
		parsed, err := time.Parse(dateLayout, today)
		if err != nil {
			return nil, fmt.Errorf("jobs: parse today: %w", err)
		}
		limit = parsed.AddDate(0, 0, current.DaysAhead).Format(dateLayout)
	}

	rows, err := s.db.QueryContext(ctx, `SELECT
			i.id, i.asset_tag, i.name, i.sn, i.sent_date, i.return_date, i.note,
			t.label_en, t.label_cs, t.label_it,
			s.label_en, s.label_cs, s.label_it, s.color, s.value
		FROM items i
		LEFT JOIN item_types t ON t.id = i.type_id
		LEFT JOIN statuses   s ON s.id = i.status_id
		WHERE i.archived = 0
          AND lower(COALESCE(s.value, '')) <> 'feasibility'
		  AND i.return_date IS NOT NULL
		  AND i.return_date <> ''
		  AND i.return_date <= ?
		ORDER BY i.return_date ASC, i.asset_tag ASC`, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: query overdue items: %w", err)
	}
	defer rows.Close()

	var (
		items []dueItem
		base  = parseDate(today)
	)
	for rows.Next() {
		var (
			item        dueItem
			returnDate  string
			sentDate    sql.NullString
			typeEn      sql.NullString
			typeCs      sql.NullString
			typeIt      sql.NullString
			statusEn    sql.NullString
			statusCs    sql.NullString
			statusIt    sql.NullString
			statusCol   sql.NullString
			statusValue sql.NullString
		)
		if err := rows.Scan(&item.id, &item.assetTag, &item.name, &item.sn, &sentDate, &returnDate, &item.note,
			&typeEn, &typeCs, &typeIt, &statusEn, &statusCs, &statusIt, &statusCol, &statusValue); err != nil {
			return nil, fmt.Errorf("jobs: scan overdue item: %w", err)
		}
		item.returnDate = returnDate
		item.sentDate = sentDate.String
		item.typeLabel = pickLabel(current.DefaultLocale, typeEn.String, typeCs.String, typeIt.String)
		item.statusLabel = pickLabel(current.DefaultLocale, statusEn.String, statusCs.String, statusIt.String)
		if fixed := statusnames.Fixed(statusValue.String); fixed != "" {
			item.statusLabel = fixed
		}
		item.statusColor = statusCol.String
		if due := parseDate(returnDate); !due.IsZero() && !base.IsZero() {
			item.daysOverdue = int(base.Sub(due).Hours() / 24)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterate overdue items: %w", err)
	}
	return items, nil
}

// notifiedToday returns the asset tags already reported by a successful
// delivery that started on the same local calendar day. This is the
// per-item/per-day dedupe.
func (s *Scheduler) notifiedToday(ctx context.Context, now time.Time) (map[string]struct{}, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc)
	end := start.AddDate(0, 0, 1)

	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM notification_deliveries
		WHERE status = ? AND created_at >= ? AND created_at < ?`,
		statusSent, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("jobs: query today's deliveries: %w", err)
	}
	defer rows.Close()

	notified := make(map[string]struct{})
	for rows.Next() {
		var payload sql.NullString
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("jobs: scan delivery payload: %w", err)
		}
		for _, tag := range decodePayloadTags(payload.String) {
			notified[strings.ToUpper(tag)] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: iterate deliveries: %w", err)
	}
	return notified, nil
}

// deliveryPayload is the JSON stored in notification_deliveries.payload. It is
// also the source of the per-item dedupe, so the asset tags must stay in it.
type deliveryPayload struct {
	AssetTags  []string `json:"asset_tags"`
	DueCount   int      `json:"due_count"`
	Skipped    int      `json:"skipped_count"`
	Truncated  bool     `json:"truncated,omitempty"`
	Messages   int      `json:"messages,omitempty"`
	MessageIDs []string `json:"message_ids,omitempty"`
}

// decodePayloadTags reads the asset tags from a stored payload, tolerating rows
// written by an older build or a hand-edited database.
func decodePayloadTags(payload string) []string {
	if strings.TrimSpace(payload) == "" {
		return nil
	}
	var decoded deliveryPayload
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		return nil
	}
	return decoded.AssetTags
}

// reserveDelivery upserts the Discord channel row and inserts (or reuses) the
// pending delivery for this idempotency key. Reusing the row of a failed or
// interrupted attempt is what makes a retry possible without ever creating two
// rows for the same report.
func (s *Scheduler) reserveDelivery(ctx context.Context, webhook string, trigger notify.Kind, key string, current settings.Settings, payload deliveryPayload) (int64, bool, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, false, fmt.Errorf("jobs: encode delivery payload: %w", err)
	}
	now := s.now().UTC().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, fmt.Errorf("jobs: begin delivery transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	channelID, err := ensureChannel(ctx, tx, webhook, current.DiscordEnabled, now)
	if err != nil {
		return 0, false, err
	}

	var (
		existingID     int64
		existingStatus string
	)
	err = tx.QueryRowContext(ctx,
		`SELECT id, status FROM notification_deliveries WHERE idempotency_key = ?`, key,
	).Scan(&existingID, &existingStatus)
	switch {
	case err == nil:
		// A previous attempt exists. `sent` must never be repeated; `pending`
		// means an interrupted run and `failed` a retryable one, so both are
		// reused rather than duplicated.
		if existingStatus == statusSent {
			return existingID, true, nil
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notification_deliveries SET status = ?, trigger = ?, item_count = ?, payload = ?, error = NULL, sent_at = NULL WHERE id = ?`,
			statusPending, string(trigger), len(payload.AssetTags), string(encoded), existingID,
		); err != nil {
			return 0, false, fmt.Errorf("jobs: reset delivery: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return 0, false, fmt.Errorf("jobs: commit delivery: %w", err)
		}
		return existingID, false, nil
	case errors.Is(err, sql.ErrNoRows):
		// fall through to the insert
	default:
		return 0, false, fmt.Errorf("jobs: look up delivery: %w", err)
	}

	res, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries
		(channel_id, trigger, status, idempotency_key, item_count, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		channelID, string(trigger), statusPending, key, len(payload.AssetTags), string(encoded), now)
	if err != nil {
		return 0, false, fmt.Errorf("jobs: insert delivery: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("jobs: insert delivery: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, false, fmt.Errorf("jobs: commit delivery: %w", err)
	}
	return id, false, nil
}

// completeDelivery records the outcome of a send attempt, preserving the asset
// tags the per-item dedupe depends on.
func (s *Scheduler) completeDelivery(ctx context.Context, id int64, status string, payload deliveryPayload, sendErr error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		s.log.Warn("cannot encode delivery outcome", "error", err)
		return
	}

	var (
		errorText any
		sentAt    any
	)
	if sendErr != nil {
		errorText = sendErr.Error()
	} else {
		sentAt = s.now().UTC().Format(time.RFC3339)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE notification_deliveries
		SET status = ?, error = ?, sent_at = ?, payload = ? WHERE id = ?`,
		status, errorText, sentAt, string(encoded), id); err != nil {
		s.log.Error("cannot record delivery outcome", "delivery_id", id, "error", err)
	}
}

// ensureChannel returns the id of the single Discord channel row, creating or
// refreshing it so a delivery always has a channel to point at.
func ensureChannel(ctx context.Context, tx *sql.Tx, webhook string, enabled bool, now string) (int64, error) {
	config, err := json.Marshal(map[string]string{"webhook_url": webhook})
	if err != nil {
		return 0, fmt.Errorf("jobs: encode channel config: %w", err)
	}

	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM notification_channels WHERE type = 'discord' ORDER BY id LIMIT 1`).Scan(&id)
	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx,
			`UPDATE notification_channels SET name = 'Discord', config = ?, enabled = ?, updated_at = ? WHERE id = ?`,
			string(config), boolToInt(enabled), now, id,
		); err != nil {
			return 0, fmt.Errorf("jobs: update channel: %w", err)
		}
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `INSERT INTO notification_channels
			(type, name, config, enabled, created_at, updated_at) VALUES ('discord', 'Discord', ?, ?, ?, ?)`,
			string(config), boolToInt(enabled), now, now)
		if err != nil {
			return 0, fmt.Errorf("jobs: insert channel: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, fmt.Errorf("jobs: insert channel: %w", err)
		}
		return id, nil
	default:
		return 0, fmt.Errorf("jobs: look up channel: %w", err)
	}
}

// auditRun records the attempt. The payload never contains the webhook URL.
func (s *Scheduler) auditRun(ctx context.Context, trigger notify.Kind, result *RunResult, sendErr error) {
	if s.audit == nil {
		return
	}
	after := map[string]any{
		"trigger":  string(trigger),
		"due":      result.DueCount,
		"new":      result.NewCount,
		"skipped":  result.SkippedCount,
		"sent":     result.Sent,
		"messages": result.Messages,
		"embeds":   result.Embeds,
	}
	if result.DeliveryID != nil {
		after["delivery_id"] = *result.DeliveryID
	}
	if sendErr != nil {
		after["error"] = sendErr.Error()
		after["status"] = notify.Status(sendErr)
	}
	if err := s.audit.Record(ctx, audit.Record{
		Action:     audit.ActionNotify,
		EntityType: audit.EntityNotification,
		EntityID:   string(trigger),
		After:      after,
	}); err != nil {
		s.log.Warn("cannot record notification audit", "error", err)
	}
}

// idempotencyKey derives a stable key for a report: the local day plus a hash of
// the reported asset tags. Two runs covering the same items produce the same
// key, so a restart cannot duplicate the message.
func idempotencyKey(day string, items []dueItem) string {
	tags := assetTags(items)
	sort.Strings(tags)
	sum := sha256.Sum256([]byte(day + "|" + strings.Join(tags, ",")))
	return fmt.Sprintf("%s:%s:%s", keyPrefix, day, hex.EncodeToString(sum[:16]))
}

// assetTags extracts the tags of a report.
func assetTags(items []dueItem) []string {
	tags := make([]string, 0, len(items))
	for _, item := range items {
		tags = append(tags, item.assetTag)
	}
	return tags
}

// notifiedItems converts internal rows into notification items.
func notifiedItems(items []dueItem) []notify.Item {
	out := make([]notify.Item, 0, len(items))
	for _, item := range items {
		out = append(out, notify.Item{
			AssetTag:    item.assetTag,
			Name:        item.name,
			SN:          item.sn,
			Type:        item.typeLabel,
			Status:      item.statusLabel,
			StatusColor: item.statusColor,
			SentDate:    item.sentDate,
			ReturnDate:  item.returnDate,
			Note:        item.note,
			DaysOverdue: item.daysOverdue,
		})
	}
	return out
}

// dateLayout is the business-date format from docs/contracts/schema.md.
const dateLayout = "2006-01-02"

// parseDate parses a business date; an empty or malformed value yields the zero
// time so callers can skip it instead of inventing a date.
func parseDate(value string) time.Time {
	parsed, err := time.Parse(dateLayout, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// pickLabel selects a localized option label, falling back to English and then
// to the first non-empty value. It mirrors the rule the API applies to item
// labels so a report reads the same as the inventory screen.
func pickLabel(locale, en, cs, it string) string {
	var chosen string
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "cs":
		chosen = cs
	case "it":
		chosen = it
	default:
		chosen = en
	}
	for _, candidate := range []string{chosen, en, cs, it} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// boolToInt renders a Go bool as the INTEGER the schema uses.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
