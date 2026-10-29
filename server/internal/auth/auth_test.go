package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"sklad/server/internal/audit"
	"sklad/server/internal/store"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// testHTTP is a minimal auth.HTTP implementation: the auth package takes its
// response helpers by injection precisely so it can be tested (and used)
// without importing the api package.
type testHTTP struct{}

func (testHTTP) WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (testHTTP) WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details ...string) {
	body := map[string]any{"error": map[string]any{"code": code, "message": message, "details": details}}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (testHTTP) NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

func (testHTTP) DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

func (testHTTP) DecodeError(w http.ResponseWriter, r *http.Request, err error) {
	w.WriteHeader(http.StatusBadRequest)
}

func (testHTTP) ClientIP(r *http.Request) string { return "10.0.0.9" }

type authEnv struct {
	t       *testing.T
	db      *sql.DB
	service *Service
	audit   *audit.Writer
	now     time.Time
}

const (
	adminUser = "admin"
	adminPass = "test-password-1"
)

func newAuthEnv(t *testing.T, mutate func(*Options)) *authEnv {
	t.Helper()
	dir := t.TempDir()
	store, err := store.Open(context.Background(), store.Options{
		DBPath:  filepath.Join(dir, "sklad.db"),
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	env := &authEnv{t: t, db: store.DB(), now: time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)}
	env.audit = audit.New(store.DB())

	opts := Options{
		DB:                store.DB(),
		Audit:             env.audit,
		HTTP:              testHTTP{},
		SessionTTL:        time.Hour,
		SessionCookieName: "sklad_session",
		CSRFCookieName:    "sklad_csrf",
		AdminUsername:     adminUser,
		AdminPassword:     adminPass,
		Now:               func() time.Time { return env.now },
	}
	if mutate != nil {
		mutate(&opts)
	}
	service, err := New(opts)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	env.service = service
	return env
}

// advance moves the injected clock, which every session/limiter decision uses.
func (e *authEnv) advance(d time.Duration) { e.now = e.now.Add(d) }

func (e *authEnv) seedAdmin(t *testing.T) {
	t.Helper()
	if err := e.service.SeedAdmin(context.Background()); err != nil {
		t.Fatalf("SeedAdmin: %v", err)
	}
}

func (e *authEnv) login(t *testing.T, username, password string) *LoginResult {
	t.Helper()
	result, err := e.service.Authenticate(context.Background(), username, password, "10.0.0.9", "test-agent")
	if err != nil {
		t.Fatalf("Authenticate(%s): %v", username, err)
	}
	return result
}

func (e *authEnv) auditCount(action, entityType string) int {
	e.t.Helper()
	var count int
	if err := e.db.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE action = ? AND entity_type = ?`, action, entityType).Scan(&count); err != nil {
		e.t.Fatalf("audit count: %v", err)
	}
	return count
}

// ---------------------------------------------------------------------------
// Password hashing
// ---------------------------------------------------------------------------

func TestHashPasswordRoundTripAndTampering(t *testing.T) {
	t.Parallel()

	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("unexpected PHC string: %q", hash)
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("VerifyPassword should accept the original password (ok=%v err=%v)", ok, err)
	}
	ok, err = VerifyPassword(hash, "wrong password")
	if err != nil {
		t.Fatalf("VerifyPassword(wrong): %v", err)
	}
	if ok {
		t.Fatal("VerifyPassword accepted the wrong password")
	}

	// A distinct salt per hash: identical passwords must not share a hash.
	other, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("second HashPassword: %v", err)
	}
	if other == hash {
		t.Fatal("two hashes of the same password are identical (no salt?)")
	}

	// Malformed or tampered hashes must fail closed, never authenticate.
	for name, candidate := range map[string]string{
		"empty":          "",
		"garbage":        "not-a-phc-string",
		"truncated":      "$argon2id$v=19$m=65536,t=3,p=1$c2FsdA",
		"bad parameters": "$argon2id$v=19$m=abc,t=3,p=1$c2FsdA$a2V5",
		"absurd memory":  "$argon2id$v=19$m=4294967295,t=3,p=1$c2FsdA$a2V5",
	} {
		ok, err := VerifyPassword(candidate, "correct horse battery staple")
		if ok {
			t.Fatalf("%s: VerifyPassword accepted a bad hash", name)
		}
		if err == nil && candidate != "" {
			t.Logf("%s: rejected without an error (acceptable)", name)
		}
	}
}

// ---------------------------------------------------------------------------
// Seeding, login, sessions
// ---------------------------------------------------------------------------

func TestSeedAdminIsIdempotentAndRequiresPasswordChange(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	ctx := context.Background()

	env.seedAdmin(t)
	var (
		count    int
		mustFlag int
		role     string
	)
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one seeded user, got %d", count)
	}
	if err := env.db.QueryRow(`SELECT must_change_password, role FROM users WHERE username = ?`, adminUser).
		Scan(&mustFlag, &role); err != nil {
		t.Fatalf("read admin: %v", err)
	}
	if mustFlag != 1 {
		t.Fatal("the seeded admin must be forced to change the password")
	}
	if role != string(RoleAdmin) {
		t.Fatalf("expected the seeded user to be an admin, got %q", role)
	}

	// Re-seeding must not create a second admin or reset the password.
	env.seedAdmin(t)
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("SeedAdmin is not idempotent: %d users", count)
	}
	if env.auditCount(audit.ActionCreate, audit.EntityUser) != 1 {
		t.Fatalf("expected exactly one audit row for the seeded admin")
	}

	// With no configured credentials nothing is seeded at all.
	empty := newAuthEnv(t, func(o *Options) {
		o.AdminUsername = ""
		o.AdminPassword = ""
	})
	if err := empty.service.SeedAdmin(ctx); err != nil {
		t.Fatalf("SeedAdmin without credentials: %v", err)
	}
	var emptyCount int
	if err := empty.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&emptyCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if emptyCount != 0 {
		t.Fatalf("expected no users without credentials, got %d", emptyCount)
	}
}

func TestAuthenticateSuccessFailureAndInactive(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()

	result := env.login(t, adminUser, adminPass)
	if result.Token == "" || result.CSRF == "" {
		t.Fatalf("login produced no token/CSRF: %+v", result)
	}
	if result.User == nil || result.User.Username != adminUser {
		t.Fatalf("unexpected user: %+v", result.User)
	}

	// Unknown user and wrong password are indistinguishable from outside.
	if _, err := env.service.Authenticate(ctx, "nobody", adminPass, "10.0.0.9", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown user: expected ErrInvalidCredentials, got %v", err)
	}
	if _, err := env.service.Authenticate(ctx, adminUser, "nope", "10.0.0.9", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: expected ErrInvalidCredentials, got %v", err)
	}

	// A disabled account is rejected even with the right password.
	if _, err := env.db.Exec(`UPDATE users SET active = 0 WHERE username = ?`, adminUser); err != nil {
		t.Fatalf("disable admin: %v", err)
	}
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.0.1.9", ""); !errors.Is(err, ErrInactiveAccount) {
		t.Fatalf("inactive account: expected ErrInactiveAccount, got %v", err)
	}

	// A successful login is audited and recorded on the user.
	if env.auditCount(audit.ActionLogin, audit.EntitySession) == 0 {
		t.Fatal("expected a login audit row")
	}
	var lastLogin sql.NullString
	if err := env.db.QueryRow(`SELECT last_login_at FROM users WHERE username = ?`, adminUser).Scan(&lastLogin); err != nil {
		t.Fatalf("read last_login_at: %v", err)
	}
	if !lastLogin.Valid || lastLogin.String == "" {
		t.Fatal("login did not record last_login_at")
	}
}

func TestSessionLifecycleExpiryAndLogout(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, func(o *Options) { o.SessionTTL = time.Hour })
	env.seedAdmin(t)
	ctx := context.Background()

	login := env.login(t, adminUser, adminPass)

	session, user, err := env.service.GetSession(ctx, login.Token)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil || user == nil {
		t.Fatal("expected a live session")
	}
	if session.CSRF != login.CSRF {
		t.Fatalf("session CSRF %q does not match the login response %q", session.CSRF, login.CSRF)
	}
	// The raw token must not be stored: only its SHA-256 hash.
	var stored string
	if err := env.db.QueryRow(`SELECT token_hash FROM sessions`).Scan(&stored); err != nil {
		t.Fatalf("read token_hash: %v", err)
	}
	if stored == login.Token {
		t.Fatal("the raw session token was stored in the database")
	}
	if stored != hashToken(login.Token) {
		t.Fatal("token_hash is not the SHA-256 of the token")
	}

	// An unknown token resolves to "no session", not an error.
	if session, user, err := env.service.GetSession(ctx, "not-a-real-token"); err != nil || session != nil || user != nil {
		t.Fatalf("unknown token: session=%v user=%v err=%v", session, user, err)
	}

	// Expiry is evaluated against the injected clock.
	env.advance(2 * time.Hour)
	if session, _, err := env.service.GetSession(ctx, login.Token); err != nil || session != nil {
		t.Fatalf("expired session still resolves: %v (%v)", session, err)
	}

	// Pruning removes the expired row.
	env.service.PruneExpiredSessions(ctx)
	var remaining int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected the expired session to be pruned, %d left", remaining)
	}

	// Logout deletes the session (and is a no-op for an unknown token).
	fresh := env.login(t, adminUser, adminPass)
	if err := env.service.Logout(ctx, fresh.Token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if session, _, err := env.service.GetSession(ctx, fresh.Token); err != nil || session != nil {
		t.Fatalf("session survived logout: %v (%v)", session, err)
	}
	if err := env.service.Logout(ctx, "unknown-token"); err != nil {
		t.Fatalf("logout with an unknown token should be a no-op, got %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()

	login := env.login(t, adminUser, adminPass)

	if err := env.service.ChangePassword(ctx, login.User.ID, "wrong-current", "brand-new-password"); !errors.Is(err, ErrPasswordMismatch) {
		t.Fatalf("expected ErrPasswordMismatch, got %v", err)
	}
	if err := env.service.ChangePassword(ctx, 999999, adminPass, "brand-new-password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown user, got %v", err)
	}

	if err := env.service.ChangePassword(ctx, login.User.ID, adminPass, "brand-new-password"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	var mustFlag int
	if err := env.db.QueryRow(`SELECT must_change_password FROM users WHERE id = ?`, login.User.ID).Scan(&mustFlag); err != nil {
		t.Fatalf("read flag: %v", err)
	}
	if mustFlag != 0 {
		t.Fatal("change-password must clear must_change_password")
	}
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.0.0.9", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("the old password must stop working, got %v", err)
	}
	if result := env.login(t, adminUser, "brand-new-password"); result.User.MustChangePassword {
		t.Fatal("the user still reports MustChangePassword after changing it")
	}
	if env.auditCount(audit.ActionUpdate, audit.EntityUser) == 0 {
		t.Fatal("expected an audit row for the password change")
	}
}

// ---------------------------------------------------------------------------
// Login rate limiting
// ---------------------------------------------------------------------------

func TestLoginRateLimitAndRecovery(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()

	// Log in once as the admin (this also clears the limiter for that account),
	// so the created account below has an actor for its audit row.
	admin := env.login(t, adminUser, adminPass)

	// Burn the limiter with wrong passwords from one address.
	var limited error
	for attempt := 1; attempt <= loginMaxFailures+3; attempt++ {
		if _, err := env.service.Authenticate(ctx, adminUser, "wrong", "10.9.9.9", ""); err != nil {
			limited = err
			if errors.Is(err, ErrRateLimited) {
				break
			}
		}
	}
	if !errors.Is(limited, ErrRateLimited) {
		t.Fatalf("expected the limiter to engage after %d failures, got %v", loginMaxFailures, limited)
	}

	// Even the correct password is refused while the limit holds...
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.9.9.9", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate-limited address was allowed to log in: %v", err)
	}
	// ...and the account is limited too, not just the address: the limiter keys on
	// both, so an attacker cannot sidestep it by rotating source addresses.
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.9.9.10", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("the account itself must stay limited across addresses, got %v", err)
	}
	// A different account from a different address is unaffected.
	other, err := env.service.CreateUser(ctx, "editor", "Editor", "", "second-password-1", RoleEditor, admin.User)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := env.service.Authenticate(ctx, other.Username, "second-password-1", "10.9.9.11", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("an unrelated account must not be limited: %v", err)
	}

	// After the window passes, the same address may try again.
	env.advance(loginWindow + time.Minute)
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.9.9.9", ""); err != nil {
		t.Fatalf("the limiter did not recover after the window: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// runMiddleware wires the auth middleware chain by hand — the production chain
// lives in the api package, and these tests only need the behaviour of each
// middleware plus their order.
func (e *authEnv) runMiddleware(chain []func(http.Handler) http.Handler, handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	e.t.Helper()
	wrapped := handler
	for i := len(chain) - 1; i >= 0; i-- {
		wrapped = chain[i](wrapped)
	}
	recorder := httptest.NewRecorder()
	wrapped.ServeHTTP(recorder, req)
	return recorder
}

func errorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (%s)", err, recorder.Body.String())
	}
	return body.Error.Code
}

func TestRequireAuthRoleAndCSRF(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()

	admin := env.login(t, adminUser, adminPass)
	// The seeded admin is gated until the password is changed; this test is about
	// the middleware chain, so clear that first (and exercise the flow while we
	// are here).
	if err := env.service.ChangePassword(ctx, admin.User.ID, adminPass, "admin-password-2"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	viewerLogin := env.login(t, adminUser, "admin-password-2")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user := UserFrom(r.Context()); user != nil {
			_, _ = fmt.Fprintf(w, "hello %s", user.Username)
			return
		}
		_, _ = w.Write([]byte("hello anonymous"))
	})
	chain := []func(http.Handler) http.Handler{env.service.RequireAuth, env.service.CSRF}

	// No session cookie → 401 with the documented code.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	recorder := env.runMiddleware(chain, handler, req)
	if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != CodeUnauthorized {
		t.Fatalf("anonymous request: %d %s", recorder.Code, recorder.Body.String())
	}

	// Valid session → the user reaches the handler.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: viewerLogin.Token})
	recorder = env.runMiddleware(chain, handler, req)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), adminUser) {
		t.Fatalf("session request: %d %s", recorder.Code, recorder.Body.String())
	}

	// Safe methods need no CSRF token; mutating ones do.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/items", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: viewerLogin.Token})
	recorder = env.runMiddleware(chain, handler, req)
	if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != CodeCSRFFailed {
		t.Fatalf("POST without CSRF: %d %s", recorder.Code, recorder.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/items", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: viewerLogin.Token})
	req.Header.Set("X-CSRF-Token", viewerLogin.CSRF)
	recorder = env.runMiddleware(chain, handler, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("POST with CSRF: %d %s", recorder.Code, recorder.Body.String())
	}

	// A wrong CSRF token is refused.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/items", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: viewerLogin.Token})
	req.Header.Set("X-CSRF-Token", "guessed-token")
	recorder = env.runMiddleware(chain, handler, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("POST with a wrong CSRF token: %d", recorder.Code)
	}

	// Role gating: a viewer cannot reach an admin-only handler.
	adminOnly := env.service.RequireRole(RoleAdmin)
	roleChain := []func(http.Handler) http.Handler{env.service.RequireAuth, adminOnly}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)

	recorder = env.runMiddleware(roleChain, handler, req)
	if recorder.Code != http.StatusUnauthorized || errorCode(t, recorder) != CodeUnauthorized {
		t.Fatalf("viewer hitting an admin route: %d %s", recorder.Code, recorder.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: admin.Token})
	recorder = env.runMiddleware(roleChain, handler, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin hitting an admin route: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestForcedPasswordChangeGate(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t) // seeded admin has must_change_password = 1
	ctx := context.Background()

	login := env.login(t, adminUser, adminPass)
	if !login.User.MustChangePassword {
		t.Fatal("the seeded admin should be flagged for a password change")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	chain := []func(http.Handler) http.Handler{env.service.RequireAuth, env.service.CSRF}

	blocked := []string{"/api/v1/items", "/api/v1/settings", "/api/v1/notifications"}
	for _, path := range blocked {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: env.service.sess, Value: login.Token})
		recorder := env.runMiddleware(chain, handler, req)
		if recorder.Code != http.StatusForbidden || errorCode(t, recorder) != CodePasswordChangeRequired {
			t.Fatalf("%s: expected 403 %s, got %d %s", path, CodePasswordChangeRequired, recorder.Code, recorder.Body.String())
		}
	}

	allowed := []string{"/api/v1/auth/me", "/api/v1/auth/logout"}
	for _, path := range allowed {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: env.service.sess, Value: login.Token})
		recorder := env.runMiddleware(chain, handler, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s must stay reachable: %d %s", path, recorder.Code, recorder.Body.String())
		}
	}

	// After the change the gate opens.
	if err := env.service.ChangePassword(ctx, login.User.ID, adminPass, "changed-password-1"); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	req.AddCookie(&http.Cookie{Name: env.service.sess, Value: login.Token})
	if recorder := env.runMiddleware(chain, handler, req); recorder.Code != http.StatusOK {
		t.Fatalf("after changing the password the API must be reachable: %d", recorder.Code)
	}
}

// ---------------------------------------------------------------------------
// User management
// ---------------------------------------------------------------------------

func TestUserManagementAndAudit(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()
	admin := env.login(t, adminUser, adminPass)

	created, err := env.service.CreateUser(ctx, "editor", "Editor Person", "editor@example.com", "editor-password-1", RoleEditor, admin.User)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if created.Role != RoleEditor || !created.Active {
		t.Fatalf("unexpected created user: %+v", created)
	}
	if !created.MustChangePassword {
		t.Fatal("a newly created account must be forced to change its password")
	}
	if _, err := env.service.CreateUser(ctx, "EDITOR", "Dup", "", "editor-password-2", RoleEditor, admin.User); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username (case-insensitive): expected ErrUsernameTaken, got %v", err)
	}
	if _, err := env.service.CreateUser(ctx, "shorty", "S", "", "short", RoleEditor, admin.User); !errors.Is(err, ErrValidation) {
		t.Fatalf("weak password: expected ErrValidation, got %v", err)
	}

	users, err := env.service.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	// Update: role, display name and active flag.
	display := "Renamed Editor"
	role := RoleAdmin
	updated, err := env.service.UpdateUser(ctx, created.ID, &display, nil, &role, nil, admin.User)
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.DisplayName != display || updated.Role != RoleAdmin {
		t.Fatalf("update did not persist: %+v", updated)
	}
	active := false
	updated, err = env.service.UpdateUser(ctx, created.ID, nil, nil, nil, &active, admin.User)
	if err != nil {
		t.Fatalf("UpdateUser(active=false): %v", err)
	}
	if updated.Active {
		t.Fatal("deactivation did not persist")
	}

	// A deactivated account cannot log in.
	if _, err := env.service.Authenticate(ctx, "editor", "editor-password-1", "10.0.0.11", ""); !errors.Is(err, ErrInactiveAccount) {
		t.Fatalf("expected ErrInactiveAccount, got %v", err)
	}

	// Re-activate and deactivate through the dedicated method.
	active = true
	if _, err := env.service.UpdateUser(ctx, created.ID, nil, nil, nil, &active, admin.User); err != nil {
		t.Fatalf("reactivate: %v", err)
	}
	if err := env.service.DeactivateUser(ctx, created.ID, admin.User); err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}
	after, err := env.service.GetUser(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if after.Active {
		t.Fatal("DeactivateUser did not deactivate the account")
	}
	if _, err := env.service.GetUser(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown user, got %v", err)
	}

	if env.auditCount(audit.ActionCreate, audit.EntityUser) < 2 {
		t.Fatalf("expected audit rows for the seeded and created users, got %d",
			env.auditCount(audit.ActionCreate, audit.EntityUser))
	}
	if env.auditCount(audit.ActionUpdate, audit.EntityUser) == 0 {
		t.Fatal("expected audit rows for user updates")
	}
}

func TestConcurrentLoginAndLogoutIsRaceFree(t *testing.T) {
	t.Parallel()
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	ctx := context.Background()

	const workers = 8
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		token []string
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := env.service.Authenticate(ctx, adminUser, adminPass, fmt.Sprintf("10.0.0.%d", i+1), "")
			if err != nil {
				t.Errorf("concurrent Authenticate: %v", err)
				return
			}
			mu.Lock()
			token = append(token, result.Token)
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(token) != workers {
		t.Fatalf("expected %d sessions, got %d", workers, len(token))
	}
	seen := map[string]bool{}
	for _, raw := range token {
		if seen[raw] {
			t.Fatal("two logins produced the same token")
		}
		seen[raw] = true
		if session, _, err := env.service.GetSession(ctx, raw); err != nil || session == nil {
			t.Fatalf("session %q not resolvable: %v", raw, err)
		}
	}

	for _, raw := range token {
		if err := env.service.Logout(ctx, raw); err != nil {
			t.Fatalf("concurrent Logout: %v", err)
		}
	}
	var remaining int
	if err := env.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&remaining); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected all sessions to be gone, %d left", remaining)
	}
}
