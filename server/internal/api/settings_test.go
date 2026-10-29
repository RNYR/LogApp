package api

// Settings and notification configuration: the admin-only endpoints that store
// the Discord webhook. Two properties matter most here: the secret never leaves
// the process unmasked (not in responses, not in audit rows), and an unusable
// configuration fails loudly.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeWebhook is a stand-in for a Discord webhook endpoint.
type fakeWebhook struct {
	mu       sync.Mutex
	payloads []map[string]any
	queries  []string
}

func newFakeWebhook(t *testing.T) (*httptest.Server, *fakeWebhook) {
	t.Helper()
	fake := &fakeWebhook{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		fake.mu.Lock()
		fake.payloads = append(fake.payloads, payload)
		fake.queries = append(fake.queries, r.URL.RawQuery)
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"9876543210987654321"}`))
	}))
	t.Cleanup(server.Close)
	return server, fake
}

func (f *fakeWebhook) snapshot() ([]map[string]any, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.payloads...), append([]string(nil), f.queries...)
}

// webhookURLFor builds a loopback webhook URL (HTTP is allowed for loopback so
// the test can run entirely offline).
func webhookURLFor(server *httptest.Server) string {
	return server.URL + "/api/webhooks/1234567890123456789/super-secret-token"
}

func TestSettingsDefaultsMaskingAndUpdates(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// Defaults, straight from docs/contracts/schema.md.
	payload := env.admin.mustDo(http.MethodGet, "/api/v1/settings", nil, http.StatusOK)
	current := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, payload).Data
	if current.Discord.WebhookSet || current.Discord.WebhookPreview != "" || current.Discord.Enabled {
		t.Fatalf("unexpected discord defaults: %+v", current.Discord)
	}
	if current.Scheduler.CheckHour != 11 || current.Scheduler.DaysAhead != 0 {
		t.Fatalf("unexpected scheduler defaults: %+v", current.Scheduler)
	}
	if current.Locale.Default != "en" || current.General.OrganizationName != "" {
		t.Fatalf("unexpected locale/general defaults: %+v %+v", current.Locale, current.General)
	}

	// Store a webhook and the rest of the runtime configuration.
	const secret = "super-secret-token"
	updated := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"discord":   map[string]any{"webhook_url": testWebhookURL, "enabled": true},
		"scheduler": map[string]any{"check_hour": 8, "days_ahead": 3},
		"locale":    map[string]any{"default": "cs"},
		"general":   map[string]any{"organization_name": "PEKAT VISION"},
	}, http.StatusOK)).Data

	if !updated.Discord.WebhookSet || !updated.Discord.Enabled {
		t.Fatalf("discord settings not stored: %+v", updated.Discord)
	}
	if strings.Contains(updated.Discord.WebhookPreview, secret) {
		t.Fatalf("the response leaked the webhook token: %q", updated.Discord.WebhookPreview)
	}
	if !strings.Contains(updated.Discord.WebhookPreview, "****") ||
		!strings.Contains(updated.Discord.WebhookPreview, "1234567890123456789") {
		t.Fatalf("unexpected webhook preview: %q", updated.Discord.WebhookPreview)
	}
	if updated.Scheduler.CheckHour != 8 || updated.Scheduler.DaysAhead != 3 ||
		updated.Locale.Default != "cs" || updated.General.OrganizationName != "PEKAT VISION" {
		t.Fatalf("settings not stored: %+v", updated)
	}

	// The raw secret is persisted, and the audit trail must not contain it.
	var stored string
	if err := env.db.QueryRow(`SELECT value FROM settings WHERE key = 'discord.webhook_url'`).Scan(&stored); err != nil {
		t.Fatalf("read stored webhook: %v", err)
	}
	if !strings.Contains(stored, secret) {
		t.Fatalf("the webhook was not stored: %q", stored)
	}
	var snapshot string
	if err := env.db.QueryRow(`SELECT after FROM audit_log WHERE action = 'settings_update'`).Scan(&snapshot); err != nil {
		t.Fatalf("read settings audit row: %v", err)
	}
	if strings.Contains(snapshot, secret) {
		t.Fatalf("the audit snapshot leaked the webhook token: %s", snapshot)
	}
	if !strings.Contains(snapshot, `"webhook_set":true`) || !strings.Contains(snapshot, `"webhook_updated":true`) {
		t.Fatalf("the audit snapshot should flag the masked change: %s", snapshot)
	}

	// A partial update leaves everything else alone and reports no secret change.
	partial := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"locale": map[string]any{"default": "it"},
	}, http.StatusOK)).Data
	if partial.Locale.Default != "it" || partial.Scheduler.CheckHour != 8 || !partial.Discord.WebhookSet {
		t.Fatalf("a partial update clobbered other settings: %+v", partial)
	}
	if err := env.db.QueryRow(`SELECT after FROM audit_log WHERE action = 'settings_update' ORDER BY id DESC LIMIT 1`).
		Scan(&snapshot); err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if !strings.Contains(snapshot, `"webhook_updated":false`) {
		t.Fatalf("a touchless update must report webhook_updated=false: %s", snapshot)
	}

	// An empty webhook_url keeps the stored secret (the contract's documented
	// behaviour) …
	kept := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"discord": map[string]any{"webhook_url": ""},
	}, http.StatusOK)).Data
	if !kept.Discord.WebhookSet {
		t.Fatalf("an empty webhook_url must keep the stored secret: %+v", kept.Discord)
	}
	// … while clear_webhook removes it (the additive extension).
	cleared := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"discord": map[string]any{"clear_webhook": true},
	}, http.StatusOK)).Data
	if cleared.Discord.WebhookSet || cleared.Discord.WebhookPreview != "" {
		t.Fatalf("clear_webhook did not remove the secret: %+v", cleared.Discord)
	}
}

func TestSettingsValidation(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	for name, body := range map[string]map[string]any{
		"hour too high":          {"scheduler": map[string]any{"check_hour": 24}},
		"hour negative":          {"scheduler": map[string]any{"check_hour": -1}},
		"negative days":          {"scheduler": map[string]any{"days_ahead": -1}},
		"days unreasonably high": {"scheduler": map[string]any{"days_ahead": 99999}},
		"unknown locale":         {"locale": map[string]any{"default": "de"}},
		"not a webhook":          {"discord": map[string]any{"webhook_url": "https://example.com/not-a-webhook"}},
		"cleartext webhook":      {"discord": map[string]any{"webhook_url": "http://discord.com/api/webhooks/1234567890123456789/token"}},
		"newline in name":        {"general": map[string]any{"organization_name": "line\nbreak"}},
		"contradiction": {"discord": map[string]any{
			"clear_webhook": true, "webhook_url": testWebhookURL,
		}},
	} {
		status, payload := env.admin.do(http.MethodPut, "/api/v1/settings", body)
		if status != http.StatusBadRequest {
			t.Fatalf("%s: expected 400, got %d (%s)", name, status, payload)
		}
		if code := errorCode(t, payload); code != CodeValidationFailed {
			t.Fatalf("%s: expected %s, got %s", name, CodeValidationFailed, code)
		}
		if details := errorDetails(t, payload); len(details) == 0 {
			t.Fatalf("%s: validation errors should explain themselves", name)
		}
	}

	// Nothing was stored by the rejected requests.
	payload := env.admin.mustDo(http.MethodGet, "/api/v1/settings", nil, http.StatusOK)
	current := decode[struct {
		Data settingsResponse `json:"data"`
	}](t, payload).Data
	if current.Discord.WebhookSet || current.Scheduler.CheckHour != 11 || current.Locale.Default != "en" {
		t.Fatalf("a rejected update changed the settings: %+v", current)
	}
}

func TestTestNotificationEndpoint(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// Without a webhook the endpoint is unprocessable, not a server error.
	status, payload := env.admin.do(http.MethodPost, "/api/v1/notifications/test", map[string]any{})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 without a webhook, got %d (%s)", status, payload)
	}
	if code := errorCode(t, payload); code != codeDiscordNotConfigured {
		t.Fatalf("expected %s, got %s", codeDiscordNotConfigured, code)
	}

	// Configure a loopback webhook (a test double) and send a test message.
	server, fake := newFakeWebhook(t)
	env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"discord": map[string]any{"webhook_url": webhookURLFor(server), "enabled": false},
		"locale":  map[string]any{"default": "cs"},
		"general": map[string]any{"organization_name": "PEKAT VISION"},
	}, http.StatusOK)

	result := decode[struct {
		Data struct {
			OK       bool `json:"ok"`
			Messages int  `json:"messages"`
			Embeds   int  `json:"embeds"`
		} `json:"data"`
	}](t, env.admin.mustDo(http.MethodPost, "/api/v1/notifications/test", map[string]any{}, http.StatusOK)).Data
	if !result.OK || result.Messages != 1 || result.Embeds != 2 {
		t.Fatalf("unexpected test result: %+v", result)
	}

	// The delivered message is the localized sample, posted safely.
	payloads, queries := fake.snapshot()
	if len(payloads) != 1 {
		t.Fatalf("expected one delivered message, got %d", len(payloads))
	}
	if len(queries) != 1 || !strings.Contains(queries[0], "wait=true") {
		t.Fatalf("the webhook must be called with wait=true, got %v", queries)
	}
	mentions, ok := payloads[0]["allowed_mentions"].(map[string]any)
	if !ok {
		t.Fatalf("allowed_mentions missing: %v", payloads[0])
	}
	if parse, ok := mentions["parse"].([]any); !ok || len(parse) != 0 {
		t.Fatalf("mentions must be disabled: %v", mentions)
	}
	embeds, ok := payloads[0]["embeds"].([]any)
	if !ok || len(embeds) != 2 {
		t.Fatalf("expected a summary and a sample embed, got %v", payloads[0]["embeds"])
	}
	summary := embeds[0].(map[string]any)
	if !strings.Contains(summary["title"].(string), "testovací") {
		t.Fatalf("the test message should use the configured locale: %v", summary["title"])
	}
	if footer, ok := summary["footer"].(map[string]any); !ok ||
		!strings.Contains(footer["text"].(string), "PEKAT VISION") {
		t.Fatalf("the footer should carry the organisation: %v", summary["footer"])
	}
	if item := embeds[1].(map[string]any); item["title"].(string) != "PV-TEST" {
		t.Fatalf("unexpected sample item: %v", item["title"])
	}

	// The attempt is audited without the secret.
	var snapshot string
	if err := env.db.QueryRow(`SELECT after FROM audit_log WHERE action = 'notify' ORDER BY id DESC LIMIT 1`).Scan(&snapshot); err != nil {
		t.Fatalf("read notify audit row: %v", err)
	}
	if !strings.Contains(snapshot, `"ok":true`) || !strings.Contains(snapshot, `"trigger":"test"`) {
		t.Fatalf("unexpected notify audit row: %s", snapshot)
	}
	if strings.Contains(snapshot, "super-secret-token") {
		t.Fatalf("the audit row leaked the webhook token: %s", snapshot)
	}

	// A webhook Discord refuses is a configuration problem (422), and the failed
	// attempt is still recorded.
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Invalid Webhook Token","code":50027}`))
	}))
	t.Cleanup(rejecting.Close)
	env.admin.mustDo(http.MethodPut, "/api/v1/settings", map[string]any{
		"discord": map[string]any{"webhook_url": webhookURLFor(rejecting)},
	}, http.StatusOK)

	status, payload = env.admin.do(http.MethodPost, "/api/v1/notifications/test", map[string]any{})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for a rejected webhook, got %d (%s)", status, payload)
	}
	if code := errorCode(t, payload); code != codeDiscordRejected {
		t.Fatalf("expected %s, got %s", codeDiscordRejected, code)
	}
	details := errorDetails(t, payload)
	if !containsSubstring(details, "Invalid Webhook Token") {
		t.Fatalf("Discord's message should reach the administrator: %v", details)
	}
	if strings.Contains(strings.Join(details, " "), "super-secret-token") {
		t.Fatalf("the error leaked the webhook token: %v", details)
	}
	if err := env.db.QueryRow(`SELECT after FROM audit_log WHERE action = 'notify' ORDER BY id DESC LIMIT 1`).Scan(&snapshot); err != nil {
		t.Fatalf("read notify audit row: %v", err)
	}
	if !strings.Contains(snapshot, `"ok":false`) {
		t.Fatalf("a failed test send must be audited too: %s", snapshot)
	}

	// Settings endpoints are admin-only.
	viewer := &sessionClient{t: t, server: env.server, client: newAnonymousClient(t)}
	for name, call := range map[string]func() (int, []byte){
		"get settings": func() (int, []byte) { return viewer.do(http.MethodGet, "/api/v1/settings", nil) },
		"put settings": func() (int, []byte) {
			return viewer.do(http.MethodPut, "/api/v1/settings", map[string]any{"locale": map[string]any{"default": "cs"}})
		},
		"test send": func() (int, []byte) {
			return viewer.do(http.MethodPost, "/api/v1/notifications/test", map[string]any{})
		},
	} {
		if status, payload := call(); status != http.StatusUnauthorized {
			t.Fatalf("viewer %s: expected 403, got %d (%s)", name, status, payload)
		}
	}
}
