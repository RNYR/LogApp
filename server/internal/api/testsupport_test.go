package api

// Shared test harness for the API packages: a real SQLite database with the
// production migrations, the real audit writer, the real auth service, the real
// middleware chain and logged-in sessions. Tests drive the API over HTTP rather
// than calling handlers directly, so routing, CSRF, RBAC and the response
// envelope are all exercised.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
	"sklad/server/internal/config"
	"sklad/server/internal/store"
)

const (
	testAdminUser     = "admin"
	testAdminPassword = "test-password-1"
	testSessionCookie = "sklad_session"
)

// testNow is the frozen clock every API test runs on: a Sunday in March 2026,
// chosen so "today" is deterministic for the overdue filter.
var testNow = time.Date(2026, 3, 15, 11, 30, 0, 0, time.UTC)

type envConfig struct {
	dbPath    string
	now       time.Time
	scheduler func(*sql.DB) OverdueRunner
	location  *time.Location
}

type envOption func(*envConfig)

// withDB points the harness at an existing database file (used by the importer
// integration test) instead of creating a fresh one.
func withDB(path string) envOption {
	return func(c *envConfig) { c.dbPath = path }
}

// withNow freezes the clock to a specific instant.
func withNow(instant time.Time) envOption {
	return func(c *envConfig) { c.now = instant }
}

// withScheduler wires a real (or stub) overdue runner for the notification API.
func withScheduler(factory func(*sql.DB) OverdueRunner) envOption {
	return func(c *envConfig) { c.scheduler = factory }
}

// testEnv is one isolated deployment: database, API and a logged-in admin.
type testEnv struct {
	t      *testing.T
	store  *store.Store
	db     *sql.DB
	api    *API
	server *httptest.Server
	admin  *sessionClient
	now    time.Time
	cfg    *config.Config
	dir    string
}

func newTestEnv(t *testing.T, opts ...envOption) *testEnv {
	t.Helper()

	cfg := envConfig{now: testNow, location: time.UTC}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.dbPath == "" {
		cfg.dbPath = filepath.Join(t.TempDir(), "sklad.db")
	}
	dataDir := filepath.Dir(cfg.dbPath)

	database, err := store.Open(context.Background(), store.Options{
		DBPath:  cfg.dbPath,
		DataDir: dataDir,
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	appConfig := &config.Config{
		BaseURL:           "https://sklad.local",
		DataDir:           dataDir,
		DBPath:            cfg.dbPath,
		HTTPAddr:          ":8080",
		Location:          cfg.location,
		SessionTTL:        time.Hour,
		SessionCookieName: testSessionCookie,
		CSRFCookieName:    "sklad_csrf",
		AdminUsername:     testAdminUser,
		AdminPassword:     testAdminPassword,
		TrustProxy:        false,
	}

	writer := audit.New(database.DB())
	authService, err := auth.New(auth.Options{
		DB:                database.DB(),
		Audit:             writer,
		HTTP:              NewAuthHTTP(appConfig),
		SessionTTL:        time.Hour,
		SessionCookieName: testSessionCookie,
		CSRFCookieName:    "sklad_csrf",
		AdminUsername:     testAdminUser,
		AdminPassword:     testAdminPassword,
		Now:               func() time.Time { return cfg.now },
	})
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	if err := authService.SeedAdmin(context.Background()); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
	// The forced password change is a UI flow; the API tests are not about it.
	if _, err := database.DB().Exec(`UPDATE users SET must_change_password = 0`); err != nil {
		t.Fatalf("clear forced password change: %v", err)
	}

	options := Options{
		Config: appConfig,
		Logger: nil,
		DB:     database.DB(),
		Audit:  writer,
		Auth:   authService,
		Now:    func() time.Time { return cfg.now },
	}
	if cfg.scheduler != nil {
		options.Scheduler = cfg.scheduler(database.DB())
	}

	instance := New(options)
	server := httptest.NewServer(instance.Handler())
	t.Cleanup(server.Close)

	env := &testEnv{
		t:      t,
		store:  database,
		db:     database.DB(),
		api:    instance,
		server: server,
		now:    cfg.now,
		cfg:    appConfig,
		dir:    dataDir,
	}
	env.admin = env.loginAs(testAdminUser, testAdminPassword)
	return env
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// sessionClient is an authenticated browser: one cookie jar, one CSRF token.
type sessionClient struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	csrf   string
	user   auth.User
}

// loginAs authenticates a user over HTTP and returns a client bound to that
// session (login also proves the cookie/CSRF contract end to end).
func (e *testEnv) loginAs(username, password string) *sessionClient {
	e.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatalf("cookiejar: %v", err)
	}
	session := &sessionClient{t: e.t, server: e.server, client: &http.Client{Jar: jar}}

	status, body := session.do(http.MethodPost, "/api/v1/auth/login",
		map[string]string{"username": username, "password": password})
	if status != http.StatusOK {
		e.t.Fatalf("login %s: status %d body %s", username, status, body)
	}
	var payload struct {
		Data struct {
			User      auth.User `json:"user"`
			CSRFToken string    `json:"csrf_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		e.t.Fatalf("decode login response: %v (%s)", err, body)
	}
	session.csrf = payload.Data.CSRFToken
	session.user = payload.Data.User
	if session.csrf == "" {
		e.t.Fatal("login response carried no CSRF token")
	}
	return session
}

// doFull performs one request and returns the response headers as well, so
// tests can assert Content-Type and similar.
func (s *sessionClient) doFull(method, path string, body any) (int, http.Header, []byte) {
	s.t.Helper()
	status, header, payload, err := s.tryDo(method, path, body)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	return status, header, payload
}

// do performs one request, adding the CSRF header to mutating verbs.
func (s *sessionClient) do(method, path string, body any) (int, []byte) {
	s.t.Helper()
	status, _, payload := s.doFull(method, path, body)
	return status, payload
}

// tryDo is the goroutine-safe primitive: it never calls t.Fatalf, so it can be
// used from parallel goroutines (the concurrency tests do exactly that).
func (s *sessionClient) tryDo(method, path string, body any) (int, http.Header, []byte, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, s.server.URL+path, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-CSRF-Token", s.csrf)
	}
	res, err := s.client.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	payload, err := io.ReadAll(res.Body)
	if err != nil {
		return 0, nil, nil, err
	}
	return res.StatusCode, res.Header, payload, nil
}

// mustDo fails the test unless the status matches.
func (s *sessionClient) mustDo(method, path string, body any, want int) []byte {
	s.t.Helper()
	status, payload := s.do(method, path, body)
	if status != want {
		s.t.Fatalf("%s %s: expected %d, got %d (%s)", method, path, want, status, payload)
	}
	return payload
}

// decode unmarshals a payload and fails the test on error.
func decode[T any](t *testing.T, payload []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(payload, &out); err != nil {
		t.Fatalf("decode response: %v (%s)", err, payload)
	}
	return out
}

// errorCode extracts the machine-readable code from a JSON error envelope.
func errorCode(t *testing.T, payload []byte) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, payload)
	}
	return body.Error.Code
}

// errorDetails returns the details array of a JSON error envelope.
func errorDetails(t *testing.T, payload []byte) []string {
	t.Helper()
	var body struct {
		Error struct {
			Details []string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, payload)
	}
	return body.Error.Details
}

// itemEnvelope / listEnvelope keep the response shapes in one place.
type itemEnvelope struct {
	Data Item `json:"data"`
}

type itemListEnvelope struct {
	Data []Item `json:"data"`
	Meta struct {
		Page       int `json:"page"`
		PerPage    int `json:"per_page"`
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	} `json:"meta"`
}

// ---------------------------------------------------------------------------
// Fixtures and assertions
// ---------------------------------------------------------------------------

// seedOptions inserts the option sets used by most tests.
func (e *testEnv) seedOptions() {
	e.t.Helper()
	now := "2026-03-01T00:00:00Z"
	e.exec(`INSERT INTO item_types (value, label_en, label_cs, label_it, sort_order, created_at, updated_at)
		VALUES ('Camera','Camera','Kamera','Telecamera',0,?,?),
		       ('Lens','Lens','Objektiv','Obiettivo',1,?,?)`, now, now, now, now)
	e.exec(`INSERT OR IGNORE INTO statuses (value, label_en, label_cs, label_it, color, sort_order, created_at, updated_at)
		VALUES ('Feasibility','Feasibility','Proveditelnost','Fattibilità','#93c47d',0,?,?),
		       ('Loaned','Loaned','Půjčeno','Prestato','#e67e22',1,?,?)`, now, now, now, now)
	e.exec(`UPDATE statuses SET label_cs='Proveditelnost',label_it='Fattibilità' WHERE value='Feasibility'`)
}

// exec runs a statement and fails the test on error.
func (e *testEnv) exec(query string, args ...any) {
	e.t.Helper()
	if _, err := e.db.Exec(query, args...); err != nil {
		e.t.Fatalf("exec %q: %v", query, err)
	}
}

// countQuery returns the first integer of a query (row/project counts).
func (e *testEnv) countQuery(query string, args ...any) int {
	e.t.Helper()
	var count int
	if err := e.db.QueryRow(query, args...).Scan(&count); err != nil {
		e.t.Fatalf("count query %q: %v", query, err)
	}
	return count
}

// auditCount counts audit rows for an action/entity pair.
func (e *testEnv) auditCount(action, entityType string) int {
	e.t.Helper()
	return e.countQuery(`SELECT COUNT(*) FROM audit_log WHERE action = ? AND entity_type = ?`, action, entityType)
}

// createItem creates an item through the API as the admin.
func (e *testEnv) createItem(body map[string]any) Item {
	e.t.Helper()
	// Existing general-purpose fixtures represent a valid rental when they
	// supply rental details. Dedicated rental tests use raw API bodies.
	e.exec(`INSERT OR IGNORE INTO statuses(value,label_en,created_at,updated_at) VALUES('Loaned','Loaned','2026-03-01','2026-03-01')`)
	e.exec(`INSERT OR IGNORE INTO personnel(id,name,created_at,updated_at) VALUES(2,'Test colleague','2026-03-01','2026-03-01')`)
	if body["status"] == nil && (body["sent_date"] != nil || body["return_date"] != nil || body["note"] != nil) {
		body["status"] = "Loaned"
	}
	if body["status"] != nil && body["status"] != "Feasibility" && body["personnel_id"] == nil {
		body["personnel_id"] = 2
	}
	payload := e.admin.mustDo(http.MethodPost, "/api/v1/items", body, http.StatusCreated)
	return decode[itemEnvelope](e.t, payload).Data
}

// getItem fetches one item through the API.
func (e *testEnv) getItem(id int64) Item {
	e.t.Helper()
	payload := e.admin.mustDo(http.MethodGet, itemPath(id), nil, http.StatusOK)
	return decode[itemEnvelope](e.t, payload).Data
}

// listItems fetches a page of items.
func (e *testEnv) listItems(query string) itemListEnvelope {
	e.t.Helper()
	payload := e.admin.mustDo(http.MethodGet, "/api/v1/items"+query, nil, http.StatusOK)
	return decode[itemListEnvelope](e.t, payload)
}

// loginWithPassword is a helper for tests that need a second admin session after
// a password change.
func (e *testEnv) relogin(password string) *sessionClient {
	e.t.Helper()
	return e.loginAs(testAdminUser, password)
}

// itemPath renders the item endpoint for an id.
func itemPath(id int64) string {
	return "/api/v1/items/" + strconv.FormatInt(id, 10)
}
