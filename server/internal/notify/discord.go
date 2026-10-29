package notify

// Discord webhook delivery: embeds, batching and rate-limit aware retries.
//
// The client is intentionally conservative about what it sends: it never posts
// mentions (so an item name cannot ping a role), it clamps every field to
// Discord's documented limits, and it never puts the webhook URL or token into
// an error message.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Discord's embed limits (https://discord.com/developers/docs/resources/message).
const (
	MaxEmbedsPerMessage   = 10
	MaxEmbedTotalChars    = 6000
	MaxEmbedTitleLen      = 256
	MaxEmbedDescription   = 4096
	MaxEmbedFields        = 25
	MaxEmbedFieldNameLen  = 256
	MaxEmbedFieldValueLen = 1024
	MaxEmbedFooterLen     = 2048
)

// Delivery defaults.
const (
	DefaultTimeout     = 15 * time.Second
	DefaultMaxAttempts = 4
	DefaultBaseDelay   = 500 * time.Millisecond
	DefaultMaxDelay    = 30 * time.Second

	// maxErrorBodyBytes caps how much of an error response is read.
	maxErrorBodyBytes = 32 << 10
)

// Embed accent colours per report kind, and the neutral fallback.
const (
	colorOverdue  = 0xE74C3C
	colorManual   = 0xE67E22
	colorTest     = 0x3498DB
	colorFallback = 0x5865F2
)

// userAgent identifies Sklad to Discord.
const userAgent = "Sklad-Discord-Webhook/1.0"

// webhookIDPattern matches Discord's numeric webhook id segment.
var webhookIDPattern = regexp.MustCompile(`^[0-9]{5,32}$`)

// DiscordOptions configures NewDiscord. WebhookURL is required.
type DiscordOptions struct {
	// WebhookURL is the channel webhook, e.g.
	// https://discord.com/api/webhooks/<id>/<token>. It is a secret.
	WebhookURL string

	// Username overrides the webhook's display name; empty keeps the channel's.
	Username string

	// Timeout bounds one HTTP attempt (default DefaultTimeout).
	Timeout time.Duration
	// MaxAttempts is the total number of attempts per message
	// (default DefaultMaxAttempts).
	MaxAttempts int
	// BaseDelay is the first retry delay; it doubles per attempt
	// (default DefaultBaseDelay).
	BaseDelay time.Duration
	// MaxDelay caps a single retry delay (default DefaultMaxDelay).
	MaxDelay time.Duration

	// HTTPClient overrides the transport (default: a client with Timeout).
	HTTPClient *http.Client
	// Sleep waits between attempts; tests inject a recorder (nil uses a
	// context-aware timer).
	Sleep func(ctx context.Context, delay time.Duration) error
	// Now supplies timestamps (nil uses time.Now).
	Now func() time.Time
}

// Discord posts reports to a Discord channel webhook.
type Discord struct {
	endpoint    *url.URL
	username    string
	client      *http.Client
	maxAttempts int
	baseDelay   time.Duration
	maxDelay    time.Duration
	sleep       func(ctx context.Context, delay time.Duration) error
	now         func() time.Time
}

// NewDiscord validates the webhook URL and builds a client.
func NewDiscord(opts DiscordOptions) (*Discord, error) {
	endpoint, err := parseWebhookURL(opts.WebhookURL)
	if err != nil {
		return nil, fmt.Errorf("notify: discord webhook: %w", err)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxAttempts
	}
	baseDelay := opts.BaseDelay
	if baseDelay <= 0 {
		baseDelay = DefaultBaseDelay
	}
	maxDelay := opts.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Discord{
		endpoint:    endpoint,
		username:    strings.TrimSpace(opts.Username),
		client:      client,
		maxAttempts: maxAttempts,
		baseDelay:   baseDelay,
		maxDelay:    maxDelay,
		sleep:       sleep,
		now:         now,
	}, nil
}

// Name implements Notifier.
func (d *Discord) Name() string { return ChannelDiscord }

// MessageCount reports how many messages a report would need, so callers can
// log or budget the delivery without sending it.
func (d *Discord) MessageCount(report Report) int {
	return len(chunkEmbeds(buildEmbeds(report, d.now())))
}

// Send delivers the report, splitting it into as many messages as Discord's
// limits require.
//
// Every message is attempted even if an earlier one failed (a partially
// delivered report is better than none); the returned error aggregates the
// failures, and the Result describes what did get through.
func (d *Discord) Send(ctx context.Context, report Report) (Result, error) {
	generatedAt := report.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = d.now()
	}
	messages := chunkEmbeds(buildEmbeds(report, generatedAt))

	var (
		result Result
		failed []error
	)
	for index, batch := range messages {
		ids, attempts, err := d.postMessage(ctx, webhookPayload{
			Username:        d.username,
			Embeds:          batch,
			AllowedMentions: &allowedMentions{Parse: []string{}},
		})
		result.Attempts += attempts
		if err != nil {
			failed = append(failed, fmt.Errorf("message %d/%d: %w", index+1, len(messages), err))
			continue
		}
		result.Messages++
		result.Embeds += len(batch)
		result.MessageIDs = append(result.MessageIDs, ids...)
	}
	if len(failed) > 0 {
		return result, errors.Join(failed...)
	}
	return result, nil
}

// postMessage posts one message, retrying transient failures with exponential
// backoff and honouring Discord's Retry-After on 429 responses.
func (d *Discord) postMessage(ctx context.Context, payload webhookPayload) ([]string, int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, &DeliveryError{Permanent: true, Message: "could not encode the payload", Err: err}
	}

	attempts := 0
	var lastErr error
	for attempts < d.maxAttempts {
		attempts++
		ids, retryAfter, err := d.attempt(ctx, body)
		if err == nil {
			return ids, attempts, nil
		}
		if IsPermanent(err) {
			return nil, attempts, err
		}
		lastErr = err
		if attempts >= d.maxAttempts {
			break
		}

		delay := retryAfter
		if delay <= 0 {
			delay = d.backoff(attempts)
		}
		if delay > d.maxDelay {
			delay = d.maxDelay
		}
		if sleepErr := d.sleep(ctx, delay); sleepErr != nil {
			return nil, attempts, errors.Join(lastErr, sleepErr)
		}
	}
	return nil, attempts, fmt.Errorf("gave up after %d attempts: %w", attempts, lastErr)
}

// attempt performs a single HTTP request and classifies the outcome.
func (d *Discord) attempt(ctx context.Context, body []byte) ([]string, time.Duration, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return nil, 0, &DeliveryError{Permanent: true, Message: "could not build the request", Err: err}
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)

	response, err := d.client.Do(request)
	if err != nil {
		// Timeouts, connection resets and DNS hiccups are worth retrying.
		return nil, 0, &DeliveryError{Message: "transport error", Err: err}
	}
	defer func() { _ = response.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return messageIDs(raw), 0, nil
	}

	status := response.StatusCode
	retryAfter := parseRetryAfter(response.Header.Get("Retry-After"), raw)
	permanent := status >= 400 && status < 500 && status != http.StatusTooManyRequests && status != http.StatusRequestTimeout

	return nil, retryAfter, &DeliveryError{
		Status:    status,
		Permanent: permanent,
		Message:   discordMessage(raw, status),
	}
}

// backoff returns the delay before the given attempt (1-based).
func (d *Discord) backoff(attempt int) time.Duration {
	delay := d.baseDelay
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= d.maxDelay {
			return d.maxDelay
		}
	}
	return delay
}

// ---------------------------------------------------------------------------
// Payload types
// ---------------------------------------------------------------------------

type webhookPayload struct {
	Username        string           `json:"username,omitempty"`
	Embeds          []embed          `json:"embeds"`
	AllowedMentions *allowedMentions `json:"allowed_mentions,omitempty"`
}

// allowedMentions with an empty parse list means "never ping anyone": an item
// name containing "@everyone" must not notify the whole server.
type allowedMentions struct {
	Parse []string `json:"parse"`
}

type embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	URL         string       `json:"url,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []embedField `json:"fields,omitempty"`
	Footer      *embedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

type embedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type embedFooter struct {
	Text string `json:"text"`
}

// buildEmbeds renders a report as a summary embed followed by one embed per
// item.
func buildEmbeds(report Report, generatedAt time.Time) []embed {
	l := labelsFor(report.Locale)

	footer := l.Footer
	if organization := strings.TrimSpace(report.Organization); organization != "" {
		footer = organization + " · " + footer
	}

	summary := embed{
		Title:       clamp(report.resolveTitle(l), MaxEmbedTitleLen),
		Description: clamp(report.resolveSummary(l), MaxEmbedDescription),
		Color:       kindColor(report.Kind),
		Footer:      &embedFooter{Text: clamp(footer, MaxEmbedFooterLen)},
		Timestamp:   generatedAt.UTC().Format(time.RFC3339),
	}
	if appURL := strings.TrimSpace(report.AppURL); appURL != "" {
		summary.URL = appURL
	}

	embeds := []embed{summary}
	for _, item := range report.Items {
		embeds = append(embeds, itemEmbed(item, l, report.AppURL))
	}
	return embeds
}

// itemEmbed renders one inventory item as an embed.
func itemEmbed(item Item, l labels, appURL string) embed {
	accent := parseColor(item.StatusColor)

	rendered := embed{
		Title:       clamp(item.AssetTag, MaxEmbedTitleLen),
		Description: clamp(item.Name, MaxEmbedDescription),
		Color:       accent,
		Timestamp:   "",
	}
	if appURL := strings.TrimSpace(appURL); appURL != "" {
		rendered.URL = appURL
	}

	addField := func(name, value string, inline bool) {
		value = clamp(value, MaxEmbedFieldValueLen)
		if value == "" || len(rendered.Fields) >= MaxEmbedFields {
			return
		}
		rendered.Fields = append(rendered.Fields, embedField{
			Name:   clamp(name, MaxEmbedFieldNameLen),
			Value:  value,
			Inline: inline,
		})
	}

	addField(l.FieldType, item.Type, true)
	addField(l.FieldStatus, item.Status, true)
	addField(l.FieldSN, item.SN, true)
	addField(l.FieldSent, item.SentDate, true)
	addField(l.FieldReturn, item.ReturnDate, true)
	if item.DaysOverdue > 0 {
		addField(l.FieldReturn, fmt.Sprintf(l.DaysOverdue, item.DaysOverdue), true)
	}
	addField(l.FieldNote, item.Note, false)

	return trimEmbed(rendered)
}

// trimEmbed enforces Discord's per-embed character budget by shortening the
// description and then dropping trailing fields.
func trimEmbed(e embed) embed {
	for embedChars(e) > MaxEmbedTotalChars {
		switch {
		case e.Description != "":
			excess := embedChars(e) - MaxEmbedTotalChars
			e.Description = clamp(e.Description, maxRunes(e.Description)-excess-1)
		case len(e.Fields) > 0:
			e.Fields = e.Fields[:len(e.Fields)-1]
		default:
			return e
		}
	}
	return e
}

// embedChars counts the characters Discord charges against an embed's budget.
func embedChars(e embed) int {
	total := utf8.RuneCountInString(e.Title) + utf8.RuneCountInString(e.Description)
	for _, field := range e.Fields {
		total += utf8.RuneCountInString(field.Name) + utf8.RuneCountInString(field.Value)
	}
	if e.Footer != nil {
		total += utf8.RuneCountInString(e.Footer.Text)
	}
	return total
}

// chunkEmbeds packs embeds into messages respecting the embed count and total
// character limits.
func chunkEmbeds(embeds []embed) [][]embed {
	if len(embeds) == 0 {
		return nil
	}
	var (
		messages [][]embed
		current  []embed
		chars    int
	)
	for _, e := range embeds {
		size := embedChars(e)
		if len(current) > 0 && (len(current) >= MaxEmbedsPerMessage || chars+size > MaxEmbedTotalChars) {
			messages = append(messages, current)
			current = nil
			chars = 0
		}
		current = append(current, e)
		chars += size
	}
	if len(current) > 0 {
		messages = append(messages, current)
	}
	return messages
}

// kindColor returns the summary accent for a report kind.
func kindColor(kind Kind) int {
	switch kind {
	case KindOverdueDaily:
		return colorOverdue
	case KindManual:
		return colorManual
	case KindTest:
		return colorTest
	default:
		return colorFallback
	}
}

// parseColor converts "#rrggbb" into Discord's integer colour, falling back to
// the neutral accent for anything unparsable.
func parseColor(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return colorFallback
	}
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return colorFallback
	}
	parsed, err := strconv.ParseInt(value, 16, 32)
	if err != nil {
		return colorFallback
	}
	return int(parsed)
}

// ---------------------------------------------------------------------------
// Response parsing
// ---------------------------------------------------------------------------

// messageIDs extracts the posted message ids (present when the webhook was
// called with wait=true).
func messageIDs(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ID == "" {
		return nil
	}
	return []string{payload.ID}
}

// parseRetryAfter reads the retry delay from the Retry-After header or the
// response body, both expressed in (possibly fractional) seconds.
func parseRetryAfter(header string, raw []byte) time.Duration {
	if header != "" {
		if seconds, err := strconv.ParseFloat(strings.TrimSpace(header), 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	if len(raw) > 0 {
		var payload struct {
			RetryAfter float64 `json:"retry_after"`
		}
		if err := json.Unmarshal(raw, &payload); err == nil && payload.RetryAfter > 0 {
			return time.Duration(payload.RetryAfter * float64(time.Second))
		}
	}
	return 0
}

// discordMessage extracts Discord's human-readable error, falling back to the
// HTTP status text. The body never contains the webhook token, but it is
// clamped anyway so a proxy error page cannot bloat the log line.
func discordMessage(raw []byte, status int) string {
	if len(raw) > 0 {
		var payload struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Message) != "" {
			return clamp(sanitize(payload.Message), 300)
		}
	}
	return "unexpected response from Discord: " + http.StatusText(status)
}

// ---------------------------------------------------------------------------
// Webhook URL handling
// ---------------------------------------------------------------------------

// ValidateWebhookURL reports whether raw is a usable Discord webhook URL.
func ValidateWebhookURL(raw string) error {
	_, err := parseWebhookURL(raw)
	return err
}

// MaskWebhook returns a display-safe preview of a webhook URL: host and webhook
// id stay readable, the token is replaced.
func MaskWebhook(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "****"
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 4 || !webhookIDPattern.MatchString(segments[2]) {
		return parsed.Scheme + "://" + parsed.Host + "/****"
	}
	return fmt.Sprintf("%s://%s/api/webhooks/%s/****", parsed.Scheme, parsed.Host, segments[2])
}

// WebhookID returns the numeric webhook id of raw, or 0 when there is none. It
// is safe to log (the id is not the secret).
func WebhookID(raw string) int64 {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 3 || !webhookIDPattern.MatchString(segments[2]) {
		return 0
	}
	id, err := strconv.ParseInt(segments[2], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// parseWebhookURL validates a webhook URL and returns its canonical form with
// ?wait=true, so the response body carries the message id.
func parseWebhookURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("is not configured")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("is not a valid URL")
	}
	if parsed.Host == "" {
		return nil, errors.New("must be an absolute URL such as https://discord.com/api/webhooks/<id>/<token>")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
		return nil, errors.New("must use https")
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	// A trailing /slack selects Slack's payload format, which this client does
	// not send; accept the paste but post to the Discord endpoint.
	if len(segments) == 5 && segments[4] == "slack" {
		segments = segments[:4]
	}
	if len(segments) != 4 || segments[0] != "api" || segments[1] != "webhooks" {
		return nil, errors.New("must look like https://discord.com/api/webhooks/<id>/<token>")
	}
	if !webhookIDPattern.MatchString(segments[2]) {
		return nil, errors.New("must contain a numeric webhook id")
	}
	if segments[3] == "" {
		return nil, errors.New("must contain a webhook token")
	}

	query := parsed.Query()
	if query.Get("wait") == "" {
		query.Set("wait", "true")
	}
	return &url.URL{
		Scheme:   parsed.Scheme,
		Host:     parsed.Host,
		Path:     "/" + strings.Join(segments, "/"),
		RawQuery: query.Encode(),
	}, nil
}

// isLoopbackHost reports whether host is a loopback address or name, which is
// the only case where a plain-HTTP webhook is accepted (local testing).
func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.Trim(host, "[]")) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return strings.HasPrefix(host, "127.")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// clamp trims a value to at most max runes (adding an ellipsis) and removes
// characters Discord rejects.
func clamp(value string, max int) string {
	value = sanitize(strings.TrimSpace(value))
	if max <= 0 {
		return value
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max == 1 {
		return string(runes[:1])
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

// sanitize drops control characters other than newline and tab, which would
// otherwise make Discord reject or mangle the message.
func sanitize(value string) string {
	if !strings.ContainsFunc(value, isDisallowedControl) {
		return value
	}
	return strings.Map(func(r rune) rune {
		if isDisallowedControl(r) {
			return -1
		}
		return r
	}, value)
}

// isDisallowedControl reports whether r is a control character Discord rejects.
func isDisallowedControl(r rune) bool {
	if r == '\n' || r == '\t' {
		return false
	}
	return r < 0x20 || r == 0x7f
}

// maxRunes returns the rune length of s.
func maxRunes(s string) int {
	return utf8.RuneCountInString(s)
}

// sleepContext waits for delay or until ctx is done.
func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
