package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// testClock is the fixed "now" used for embed timestamps.
func testClock() time.Time {
	return time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
}

// fakeDiscord is a stand-in for a Discord webhook endpoint: it records the
// payloads and answers with a scripted sequence of statuses (the last status
// repeats), so retry behaviour can be driven deterministically.
type fakeDiscord struct {
	mu       sync.Mutex
	payloads []webhookPayload
	queries  []string
	requests int
}

func newFakeDiscord(t *testing.T, statuses []int, headers []http.Header) (*httptest.Server, *fakeDiscord) {
	t.Helper()
	fake := &fakeDiscord{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload webhookPayload
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Errorf("fake discord: decode payload: %v (%s)", err, raw)
			}
		}

		fake.mu.Lock()
		index := fake.requests
		fake.requests++
		fake.payloads = append(fake.payloads, payload)
		fake.queries = append(fake.queries, r.URL.RawQuery)
		fake.mu.Unlock()

		status := http.StatusOK
		switch {
		case len(statuses) == 0:
		case index < len(statuses):
			status = statuses[index]
		default:
			status = statuses[len(statuses)-1]
		}
		if index < len(headers) && headers[index] != nil {
			for key, values := range headers[index] {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
		}

		switch {
		case status == http.StatusTooManyRequests:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.75,"global":false}`))
		case status >= 400:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"Invalid Webhook Token","code":50027}`))
		case status == http.StatusNoContent:
			w.WriteHeader(status)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"1234567890123456789","channel_id":"42"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func (f *fakeDiscord) snapshot() ([]webhookPayload, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	payloads := append([]webhookPayload(nil), f.payloads...)
	queries := append([]string(nil), f.queries...)
	return payloads, queries
}

func webhookFor(server *httptest.Server) string {
	return server.URL + "/api/webhooks/1234567890123456789/super-secret-token"
}

// sleepRecorder captures the delays the client would wait for.
type sleepRecorder struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *sleepRecorder) sleep(ctx context.Context, delay time.Duration) error {
	r.mu.Lock()
	r.delays = append(r.delays, delay)
	r.mu.Unlock()
	return ctx.Err()
}

func (r *sleepRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.delays...)
}

func sampleItems(count int) []Item {
	items := make([]Item, count)
	for i := range items {
		items[i] = Item{
			AssetTag:    fmt.Sprintf("PV-%03d", i+1),
			Name:        "Camera " + strings.Repeat("x", 300),
			Type:        "Camera",
			Status:      "Loaned",
			StatusColor: "#ff0000",
			SN:          fmt.Sprintf("SN-%d", i),
			SentDate:    "2026-03-01",
			ReturnDate:  "2026-03-10",
			Note:        "note\nwith newline",
			DaysOverdue: 5,
		}
	}
	return items
}

// ---------------------------------------------------------------------------
// Webhook URL handling
// ---------------------------------------------------------------------------

func TestWebhookURLValidationAndMasking(t *testing.T) {
	t.Parallel()

	valid := []string{
		"https://discord.com/api/webhooks/1234567890123456789/abcDEF-token",
		"https://discordapp.com/api/webhooks/1234567890123456789/token?thread_id=99",
		"https://discord.com/api/webhooks/1234567890123456789/token/slack",
		"http://127.0.0.1:8080/api/webhooks/1234567890123456789/token",
		"http://localhost/api/webhooks/1234567890123456789/token",
		"  https://discord.com/api/webhooks/1234567890123456789/token  ",
	}
	for _, raw := range valid {
		if err := ValidateWebhookURL(raw); err != nil {
			t.Fatalf("expected %q to be valid: %v", raw, err)
		}
	}

	for name, raw := range map[string]string{
		"empty":         "",
		"not a url":     "discord.com/api/webhooks/1/t",
		"http public":   "http://discord.com/api/webhooks/1234567890123456789/token",
		"no id":         "https://discord.com/api/webhooks/abc/token",
		"short id":      "https://discord.com/api/webhooks/12/token",
		"no token":      "https://discord.com/api/webhooks/1234567890123456789",
		"wrong path":    "https://discord.com/api/hooks/1234567890123456789/token",
		"extra segment": "https://discord.com/api/webhooks/1234567890123456789/token/extra",
		"ftp scheme":    "ftp://discord.com/api/webhooks/1234567890123456789/token",
		"relative":      "/api/webhooks/1234567890123456789/token",
	} {
		if err := ValidateWebhookURL(raw); err == nil {
			t.Fatalf("%s: expected %q to be rejected", name, raw)
		}
	}

	masked := MaskWebhook("https://discord.com/api/webhooks/1234567890123456789/super-secret-token")
	if strings.Contains(masked, "super-secret-token") {
		t.Fatalf("MaskWebhook leaked the token: %q", masked)
	}
	if !strings.Contains(masked, "1234567890123456789") || !strings.Contains(masked, "****") {
		t.Fatalf("unexpected mask: %q", masked)
	}
	if MaskWebhook("") != "" {
		t.Fatalf("an empty webhook should mask to an empty string")
	}
	if MaskWebhook("not a url") != "****" {
		t.Fatalf("garbage should mask to ****, got %q", MaskWebhook("not a url"))
	}
	if id := WebhookID("https://discord.com/api/webhooks/1234567890123456789/t"); id != 1234567890123456789 {
		t.Fatalf("unexpected webhook id %d", id)
	}
	if id := WebhookID("garbage"); id != 0 {
		t.Fatalf("garbage should have no webhook id, got %d", id)
	}

	// The client normalises the URL: the /slack suffix is dropped and wait=true
	// is added so the response carries the message id.
	client, err := NewDiscord(DiscordOptions{
		WebhookURL: "https://discord.com/api/webhooks/1234567890123456789/token/slack",
		Now:        testClock,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if got := client.endpoint.Path; got != "/api/webhooks/1234567890123456789/token" {
		t.Fatalf("unexpected normalised path %q", got)
	}
	if got := client.endpoint.Query().Get("wait"); got != "true" {
		t.Fatalf("expected wait=true in the endpoint, got %q", got)
	}
	if _, err := NewDiscord(DiscordOptions{WebhookURL: "nonsense"}); err == nil {
		t.Fatal("NewDiscord should reject an unusable URL")
	}
}

// ---------------------------------------------------------------------------
// Payload shape, batching and limits
// ---------------------------------------------------------------------------

func TestDiscordSendBatchesWithinDiscordLimits(t *testing.T) {
	t.Parallel()
	server, fake := newFakeDiscord(t, []int{http.StatusOK}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Username: "Sklad", Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}

	// 25 items plus the summary embed: 26 embeds, which Discord requires to be
	// split into messages of at most 10 embeds.
	result, err := client.Send(context.Background(), Report{
		Kind:         KindOverdueDaily,
		Items:        sampleItems(25),
		Locale:       "en",
		Organization: "PEKAT VISION",
		AppURL:       "https://sklad.local",
		GeneratedAt:  testClock(),
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	payloads, queries := fake.snapshot()
	if result.Messages != 3 || len(payloads) != 3 {
		t.Fatalf("expected 3 messages, got %d/%d", result.Messages, len(payloads))
	}
	if result.Embeds != 26 {
		t.Fatalf("expected 26 embeds, got %d", result.Embeds)
	}
	if len(result.MessageIDs) != 3 || result.Attempts != 3 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(queries[0], "wait=true") {
		t.Fatalf("the webhook must be called with wait=true, got %q", queries[0])
	}

	for i, payload := range payloads {
		if len(payload.Embeds) > MaxEmbedsPerMessage {
			t.Fatalf("message %d carries %d embeds", i, len(payload.Embeds))
		}
		if payload.AllowedMentions == nil || len(payload.AllowedMentions.Parse) != 0 {
			t.Fatalf("message %d must disable mentions, got %+v", i, payload.AllowedMentions)
		}
		if payload.Username != "Sklad" {
			t.Fatalf("message %d lost the username: %q", i, payload.Username)
		}
		total := 0
		for _, embed := range payload.Embeds {
			if chars := embedChars(embed); chars > MaxEmbedTotalChars {
				t.Fatalf("embed %q exceeds the character budget (%d)", embed.Title, chars)
			}
			total += embedChars(embed)
		}
		if total > MaxEmbedTotalChars {
			t.Fatalf("message %d exceeds the aggregate budget (%d)", i, total)
		}
	}

	summary := payloads[0].Embeds[0]
	if summary.Title != "Sklad — 25 items overdue" {
		t.Fatalf("unexpected summary title %q", summary.Title)
	}
	if !strings.Contains(summary.Description, "Items: 25") || !strings.Contains(summary.Description, "Overdue: 25") {
		t.Fatalf("unexpected summary text %q", summary.Description)
	}
	if summary.Color != colorOverdue {
		t.Fatalf("summary colour should mark overdue items: %x", summary.Color)
	}
	if summary.Footer == nil || !strings.Contains(summary.Footer.Text, "PEKAT VISION") {
		t.Fatalf("summary footer should name the organisation: %+v", summary.Footer)
	}
	if summary.URL != "https://sklad.local" || summary.Timestamp != testClock().Format(time.RFC3339) {
		t.Fatalf("summary link/timestamp wrong: %q %q", summary.URL, summary.Timestamp)
	}

	item := payloads[0].Embeds[1]
	if item.Title != "PV-001" {
		t.Fatalf("item embeds are titled with the asset tag, got %q", item.Title)
	}
	if len([]rune(item.Description)) > MaxEmbedDescription {
		t.Fatalf("item description exceeds Discord's limit (%d runes)", len([]rune(item.Description)))
	}
	if item.Color != 0xff0000 {
		t.Fatalf("the status colour should accent the embed, got %x", item.Color)
	}

	names := make([]string, 0, len(item.Fields))
	for _, field := range item.Fields {
		names = append(names, field.Name)
	}
	for _, want := range []string{"Type", "Status", "Serial number", "Sent", "Return by", "Note"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing field %q in %v", want, names)
		}
	}
	overdueField := false
	for _, field := range item.Fields {
		if strings.Contains(field.Value, "Overdue by 5 day(s)") {
			overdueField = true
		}
	}
	if !overdueField {
		t.Fatalf("the overdue day count is missing from %+v", item.Fields)
	}
}

func TestDiscordSendLocalizesAndHandlesEmptyReports(t *testing.T) {
	t.Parallel()
	server, fake := newFakeDiscord(t, []int{http.StatusOK}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}

	// An empty report still sends the summary: "nothing is overdue" is worth
	// knowing, and it proves the pipeline works.
	result, err := client.Send(context.Background(), Report{Kind: KindTest, Locale: "cs", GeneratedAt: testClock()})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Messages != 1 {
		t.Fatalf("expected one summary message, got %+v", result)
	}
	payloads, _ := fake.snapshot()
	summary := payloads[0].Embeds[0]
	if summary.Title != "Sklad — testovací oznámení" || !strings.Contains(summary.Description, "Žádné") {
		t.Fatalf("Czech wording missing: %q / %q", summary.Title, summary.Description)
	}

	// Czech field names for a real item, and an invalid colour falls back.
	if _, err := client.Send(context.Background(), Report{
		Kind: KindOverdueDaily, Locale: "cs", GeneratedAt: testClock(),
		Items: []Item{{AssetTag: "PV-001", Name: "Kamera", Type: "Kamera", Status: "Půjčeno", StatusColor: "bogus"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	payloads, _ = fake.snapshot()
	item := payloads[1].Embeds[1]
	if item.Fields[0].Name != "Typ" {
		t.Fatalf("Czech field names missing: %+v", item.Fields)
	}
	if item.Color != colorFallback {
		t.Fatalf("an invalid colour should fall back, got %x", item.Color)
	}

	// Italian manual-run wording.
	if _, err := client.Send(context.Background(), Report{Kind: KindManual, Locale: "it", GeneratedAt: testClock()}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	payloads, _ = fake.snapshot()
	if title := payloads[2].Embeds[0].Title; title != "Sklad — controllo scadenze (0 articoli)" {
		t.Fatalf("Italian wording missing: %q", title)
	}
}

func TestDiscordSendSanitizesAndNeverPings(t *testing.T) {
	t.Parallel()
	server, fake := newFakeDiscord(t, []int{http.StatusOK}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}

	if _, err := client.Send(context.Background(), Report{
		Kind: KindOverdueDaily, Locale: "en", GeneratedAt: testClock(),
		Items: []Item{{
			AssetTag: "PV-001\x00",
			Name:     "@everyone please return this",
			Note:     "line\x01one\nline two",
		}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	payloads, _ := fake.snapshot()
	item := payloads[0].Embeds[1]
	if strings.ContainsRune(item.Title, 0) || strings.ContainsRune(item.Description, 1) {
		t.Fatalf("control characters leaked: %q / %q", item.Title, item.Description)
	}
	if !strings.Contains(item.Description, "@everyone") {
		t.Fatalf("the text itself should be preserved: %q", item.Description)
	}
	if payloads[0].AllowedMentions == nil || len(payloads[0].AllowedMentions.Parse) != 0 {
		t.Fatal("mentions must stay disabled so an item name cannot ping a role")
	}
}

// ---------------------------------------------------------------------------
// Retries and rate limiting
// ---------------------------------------------------------------------------

func TestDiscordRetriesRateLimitsAndBackoff(t *testing.T) {
	t.Parallel()

	// 429 with a Retry-After header: the delay is honoured.
	server, fake := newFakeDiscord(t, []int{http.StatusTooManyRequests, http.StatusOK},
		[]http.Header{{"Retry-After": []string{"0.5"}}, nil})
	recorder := &sleepRecorder{}
	client, err := NewDiscord(DiscordOptions{
		WebhookURL: webhookFor(server), Now: testClock, Sleep: recorder.sleep, BaseDelay: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err := client.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()})
	if err != nil {
		t.Fatalf("Send after 429: %v", err)
	}
	if result.Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", result.Attempts)
	}
	payloads, _ := fake.snapshot()
	if len(payloads) != 2 {
		t.Fatalf("expected the message to be retried, got %d requests", len(payloads))
	}
	if delays := recorder.recorded(); len(delays) != 1 || delays[0] != 500*time.Millisecond {
		t.Fatalf("expected Retry-After (0.5s) to be honoured, got %v", delays)
	}

	// 429 without a header: the body's retry_after is used.
	server2, _ := newFakeDiscord(t, []int{http.StatusTooManyRequests, http.StatusOK}, nil)
	recorder2 := &sleepRecorder{}
	client2, err := NewDiscord(DiscordOptions{
		WebhookURL: webhookFor(server2), Now: testClock, Sleep: recorder2.sleep, BaseDelay: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if _, err := client2.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()}); err != nil {
		t.Fatalf("Send after 429 body: %v", err)
	}
	if delays := recorder2.recorded(); len(delays) != 1 || delays[0] != 750*time.Millisecond {
		t.Fatalf("expected the body's retry_after (0.75s), got %v", delays)
	}

	// 5xx: exponential backoff, then success.
	server3, _ := newFakeDiscord(t, []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusOK}, nil)
	recorder3 := &sleepRecorder{}
	client3, err := NewDiscord(DiscordOptions{
		WebhookURL: webhookFor(server3), Now: testClock, Sleep: recorder3.sleep,
		BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err = client3.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()})
	if err != nil {
		t.Fatalf("Send after 5xx: %v", err)
	}
	if result.Attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", result.Attempts)
	}
	if delays := recorder3.recorded(); len(delays) != 2 || delays[0] != 100*time.Millisecond || delays[1] != 200*time.Millisecond {
		t.Fatalf("expected exponential backoff [100ms 200ms], got %v", delays)
	}

	// A permanent 4xx is not retried and never leaks the webhook token.
	server4, fake4 := newFakeDiscord(t, []int{http.StatusBadRequest}, nil)
	recorder4 := &sleepRecorder{}
	client4, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server4), Now: testClock, Sleep: recorder4.sleep})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err = client4.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()})
	if err == nil {
		t.Fatal("expected a permanent delivery failure")
	}
	if !IsPermanent(err) || Status(err) != http.StatusBadRequest {
		t.Fatalf("expected a permanent 400, got permanent=%v status=%d", IsPermanent(err), Status(err))
	}
	if result.Attempts != 1 || len(recorder4.recorded()) != 0 {
		t.Fatalf("a permanent failure must not retry: attempts=%d delays=%v", result.Attempts, recorder4.recorded())
	}
	if payloads, _ := fake4.snapshot(); len(payloads) != 1 {
		t.Fatalf("expected exactly one request, got %d", len(payloads))
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("the error leaked the webhook token: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid Webhook Token") {
		t.Fatalf("the error should surface Discord's message: %v", err)
	}

	// Persistent rate limiting gives up after MaxAttempts.
	server5, _ := newFakeDiscord(t, []int{http.StatusTooManyRequests}, nil)
	recorder5 := &sleepRecorder{}
	client5, err := NewDiscord(DiscordOptions{
		WebhookURL: webhookFor(server5), Now: testClock, Sleep: recorder5.sleep,
		MaxAttempts: 3, BaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if _, err := client5.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()}); err == nil {
		t.Fatal("expected rate limiting to eventually fail")
	}
	if delays := recorder5.recorded(); len(delays) != 2 {
		t.Fatalf("expected 2 retries (no sleep after the final attempt), got %v", delays)
	}

	// A cancelled context aborts the retry loop.
	server6, _ := newFakeDiscord(t, []int{http.StatusInternalServerError}, nil)
	client6, err := NewDiscord(DiscordOptions{
		WebhookURL: webhookFor(server6), Now: testClock,
		Sleep: func(context.Context, time.Duration) error { return context.Canceled },
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if _, err := client6.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()}); err == nil {
		t.Fatal("expected cancellation to abort the delivery")
	}
}

func TestDiscordHandles204TransportErrorsAndPartialDelivery(t *testing.T) {
	t.Parallel()

	// 204 (no body) counts as delivered.
	server, _ := newFakeDiscord(t, []int{http.StatusNoContent}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err := client.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()})
	if err != nil {
		t.Fatalf("Send 204: %v", err)
	}
	if result.Messages != 1 || len(result.MessageIDs) != 0 {
		t.Fatalf("unexpected 204 handling: %+v", result)
	}

	// Transport errors are transient: a refused connection is retried.
	client, err = NewDiscord(DiscordOptions{
		WebhookURL: "http://127.0.0.1:1/api/webhooks/1234567890123456789/token",
		Now:        testClock, MaxAttempts: 2, BaseDelay: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if _, err := client.Send(context.Background(), Report{Kind: KindTest, GeneratedAt: testClock()}); err == nil {
		t.Fatal("expected a transport failure")
	} else if IsPermanent(err) {
		t.Fatalf("a transport failure must be retryable, got %v", err)
	}

	// Two messages, the second fails: the first is reported as delivered and the
	// error names the failed message.
	attempts := 0
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 2 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Unknown Webhook"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	t.Cleanup(flaky.Close)

	client, err = NewDiscord(DiscordOptions{WebhookURL: webhookFor(flaky), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err = client.Send(context.Background(), Report{Kind: KindOverdueDaily, Items: sampleItems(11), GeneratedAt: testClock()})
	if err == nil {
		t.Fatal("expected the second message to fail")
	}
	if result.Messages != 1 {
		t.Fatalf("the first message should be counted as delivered: %+v", result)
	}
	if !strings.Contains(err.Error(), "message 2/2") {
		t.Fatalf("the error should identify the failed message: %v", err)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("the error leaked the webhook token: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sample report, message counting and truncation wording
// ---------------------------------------------------------------------------

func TestSampleReportAndMessageCount(t *testing.T) {
	t.Parallel()

	for locale, want := range map[string]string{
		"en": "Test message from", "cs": "Testovací zpráva", "it": "Messaggio di prova",
	} {
		report := SampleReport(locale, "PEKAT", "https://sklad.local", testClock())
		if report.Kind != KindTest || len(report.Items) != 1 {
			t.Fatalf("%s: unexpected sample report %+v", locale, report)
		}
		if !strings.Contains(report.Summary, want) {
			t.Fatalf("%s: sample summary %q", locale, report.Summary)
		}
		if report.Items[0].AssetTag != "PV-TEST" || report.Items[0].StatusColor == "" {
			t.Fatalf("%s: unexpected sample item %+v", locale, report.Items[0])
		}
	}

	server, _ := newFakeDiscord(t, []int{http.StatusOK}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	if got := client.MessageCount(SampleReport("en", "", "", testClock())); got != 1 {
		t.Fatalf("MessageCount for the sample report: %d", got)
	}
	// 21 item embeds plus the summary = 22 embeds => 10 + 10 + 2.
	if got := client.MessageCount(Report{Items: sampleItems(21)}); got != 3 {
		t.Fatalf("MessageCount for 22 embeds should be 3, got %d", got)
	}
}

func TestTruncatedSummariesStayWithinBudget(t *testing.T) {
	t.Parallel()

	report := Report{Kind: KindOverdueDaily, Locale: "en", Items: sampleItems(2), TotalItems: 5}
	for i := range report.Items { // only the truncation wording is under test here
		report.Items[i].DaysOverdue = 0
	}
	if got := report.resolveSummary(labelsFor("en")); got != "Items: 5\nShowing the first 2 of 5" {
		t.Fatalf("unexpected truncated summary: %q", got)
	}
	for locale, expected := range map[string]string{
		"cs": "Položek: 5\nZobrazeno prvních 2 z 5",
		"it": "Articoli: 5\nMostrati i primi 2 di 5",
	} {
		report.Locale = locale
		if got := report.resolveSummary(labelsFor(locale)); got != expected {
			t.Fatalf("%s summary: %q (want %q)", locale, got, expected)
		}
	}

	// A complete report says nothing extra.
	complete := Report{Kind: KindOverdueDaily, Locale: "en", Items: sampleItems(3)}
	for i := range complete.Items {
		complete.Items[i].DaysOverdue = 0
	}
	if got := complete.resolveSummary(labelsFor("en")); got != "Items: 3" {
		t.Fatalf("unexpected complete summary: %q", got)
	}

	// Even an extreme truncation must fit Discord's embed budget.
	big := Report{Kind: KindOverdueDaily, Locale: "en", Items: sampleItems(500), TotalItems: 5000}
	embed := buildEmbeds(big, testClock())[0]
	if chars := embedChars(embed); chars > MaxEmbedTotalChars {
		t.Fatalf("summary embed exceeds the budget (%d)", chars)
	}
	if !strings.Contains(embed.Description, "5000") {
		t.Fatalf("the summary should mention the real total: %q", embed.Description)
	}
}

func TestDiscordMessageIDsAndErrorsAreJoined(t *testing.T) {
	t.Parallel()

	// Every message carries its id back so the delivery log can record it.
	server, _ := newFakeDiscord(t, []int{http.StatusOK}, nil)
	client, err := NewDiscord(DiscordOptions{WebhookURL: webhookFor(server), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	result, err := client.Send(context.Background(), Report{Kind: KindOverdueDaily, Items: sampleItems(11), GeneratedAt: testClock()})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.Messages != 2 || len(result.MessageIDs) != 2 {
		t.Fatalf("expected two messages with ids, got %+v", result)
	}

	// A failure in every message is reported as a joined error.
	failing, _ := newFakeDiscord(t, []int{http.StatusForbidden}, nil)
	client, err = NewDiscord(DiscordOptions{WebhookURL: webhookFor(failing), Now: testClock})
	if err != nil {
		t.Fatalf("NewDiscord: %v", err)
	}
	_, err = client.Send(context.Background(), Report{Kind: KindOverdueDaily, Items: sampleItems(11), GeneratedAt: testClock()})
	if err == nil {
		t.Fatal("expected both messages to fail")
	}
	message := err.Error()
	if !strings.Contains(message, "message 1/2") || !strings.Contains(message, "message 2/2") {
		t.Fatalf("both failures should be reported: %v", err)
	}
	if !IsPermanent(err) {
		t.Fatalf("a 403 from Discord is a permanent failure: %v", err)
	}
}
