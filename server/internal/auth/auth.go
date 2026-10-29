// Package auth implements Sklad's identity and access control: argon2id
// password hashing, server-side sessions, double-submit CSRF protection,
// Admin authentication and public Editor rental access, first-boot admin seeding
// with a forced password change, login rate-limiting, and the HTTP surface for
// /api/v1/auth/* and /api/v1/users.
//
// The package deliberately does not import the api package (that would form an
// import cycle). Instead, HTTP response concerns (the JSON error envelope, the
// request decoder, client IP resolution) are injected through the HTTP
// interface, which the api package implements with a thin adapter.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/argon2"

	"sklad/server/internal/audit"
)

// ---------------------------------------------------------------------------
// Roles
// ---------------------------------------------------------------------------

// Role is a user's permission level (schema.md: users.role).
type Role string

const (
	RoleAdmin  Role = "admin"
	RoleEditor Role = "editor"
)

// ValidRoles lists every role, ordered from most to least privileged.
var ValidRoles = []Role{RoleAdmin, RoleEditor}

// ParseRole validates a role string.
func ParseRole(s string) (Role, error) {
	r := Role(strings.ToLower(strings.TrimSpace(s)))
	for _, valid := range ValidRoles {
		if r == valid {
			return r, nil
		}
	}
	return "", fmt.Errorf("invalid role %q (want admin or editor)", s)
}

// ---------------------------------------------------------------------------
// Error codes (values are frozen in openapi/openapi.yaml)
// ---------------------------------------------------------------------------

const (
	CodeUnauthorized           = "unauthorized"
	CodeForbidden              = "forbidden"
	CodeNotFound               = "not_found"
	CodeBadRequest             = "bad_request"
	CodeValidationFailed       = "validation_failed"
	CodeConflict               = "conflict"
	CodeRateLimited            = "rate_limited"
	CodeInternal               = "internal_error"
	CodePasswordChangeRequired = "password_change_required"
	CodeCSRFFailed             = "csrf_failed"
)

// Sentinel errors returned by Service methods; handlers map them onto HTTP
// statuses and codes.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrRateLimited        = errors.New("too many login attempts")
	ErrInactiveAccount    = errors.New("account is disabled")
	ErrPasswordMismatch   = errors.New("current password is incorrect")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrNotFound           = errors.New("not found")
)

// ---------------------------------------------------------------------------
// User model
// ---------------------------------------------------------------------------

// User is the JSON-facing user representation (the User schema in
// openapi/openapi.yaml). It never carries the password hash.
type User struct {
	ID                 int64      `json:"id"`
	Username           string     `json:"username"`
	DisplayName        string     `json:"display_name"`
	Email              string     `json:"email"`
	Role               Role       `json:"role"`
	MustChangePassword bool       `json:"must_change_password"`
	Active             bool       `json:"active"`
	LastLoginAt        *time.Time `json:"last_login_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// userColumns is the SELECT column list shared by every user query.
const userColumns = "id, username, display_name, email, role, must_change_password, active, last_login_at, created_at, updated_at"

// scanUser reads a User from the standard column list.
func scanUser(scanner interface{ Scan(dest ...any) error }) (*User, error) {
	var u User
	var role string
	var mustChange, active int
	var lastLogin sql.NullString
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&u.ID, &u.Username, &u.DisplayName, &u.Email, &role,
		&mustChange, &active, &lastLogin, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	u.Role = Role(role)
	u.MustChangePassword = mustChange != 0
	u.Active = active != 0
	if lastLogin.Valid {
		if t, err := time.Parse(time.RFC3339, lastLogin.String); err == nil {
			u.LastLoginAt = &t
		}
	}
	u.CreatedAt = parseRFC3339(createdAt)
	u.UpdatedAt = parseRFC3339(updatedAt)
	return &u, nil
}

// parseRFC3339 returns t parsed from s, or the zero time when s is malformed.
// Stored timestamps are written by this package and always valid; the zero
// value is a safe fallback.
func parseRFC3339(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ---------------------------------------------------------------------------
// Password hashing (argon2id, PHC string)
// ---------------------------------------------------------------------------

const (
	argon2Time    = 3
	argon2Memory  = 64 * 1024 // KiB
	argon2Threads = 1
	saltLen       = 16
	keyLen        = 32
)

// HashPassword returns an argon2id PHC string for password.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: read salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, keyLen)
	return encodeArgon2(salt, key), nil
}

// VerifyPassword reports whether password matches the PHC string phc.
func VerifyPassword(phc, password string) (bool, error) {
	salt, key, params, err := decodeArgon2(phc)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(key)))
	return subtle.ConstantTimeCompare(got, key) == 1, nil
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

func encodeArgon2(salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func decodeArgon2(phc string) (salt, key []byte, params argon2Params, err error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return nil, nil, params, errors.New("auth: unsupported password hash")
	}
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, nil, params, errors.New("auth: malformed password hash")
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return nil, nil, params, errors.New("auth: malformed password hash")
		}
		switch k {
		case "m":
			params.memory = uint32(n)
		case "t":
			params.time = uint32(n)
		case "p":
			params.threads = uint8(n)
		}
	}
	// Bounds protect against a tampered hash requesting absurd resources.
	if params.memory < 8 || params.memory > 1<<20 ||
		params.time < 1 || params.time > 100 ||
		params.threads < 1 || params.threads > 16 {
		return nil, nil, params, errors.New("auth: password hash parameters out of range")
	}
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil {
		return nil, nil, params, errors.New("auth: malformed password hash salt")
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil {
		return nil, nil, params, errors.New("auth: malformed password hash")
	}
	if len(salt) < 8 || len(key) < 16 {
		return nil, nil, params, errors.New("auth: password hash too short")
	}
	return salt, key, params, nil
}

// ---------------------------------------------------------------------------
// Login rate limiter (in-memory; per process, adequate for a single instance)
// ---------------------------------------------------------------------------

const (
	loginWindow       = 15 * time.Minute
	loginMaxFailures  = 10
	limiterMaxEntries = 10000 // bound memory under a sustained attack
)

type loginLimiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{failures: make(map[string][]time.Time)}
}

// allow records an attempt and reports whether it is permitted. Keys should be
// "ip:<addr>" and "user:<lowercased username>".
func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := now.Add(-loginWindow)
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.failures[key] = kept
	if len(kept) >= loginMaxFailures {
		return false
	}
	l.failures[key] = append(kept, now)
	if len(l.failures) > limiterMaxEntries {
		for k := range l.failures {
			if len(l.failures[k]) == 0 {
				delete(l.failures, k)
			}
		}
	}
	return true
}

func (l *loginLimiter) clear(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}

// ---------------------------------------------------------------------------
// HTTP response helper interface (implemented by the api package)
// ---------------------------------------------------------------------------

// HTTP injects the api layer's response helpers so that the auth package writes
// the exact same JSON envelope, request-ID echo and body decoding as every
// other handler, without importing api.
type HTTP interface {
	WriteJSON(w http.ResponseWriter, status int, v any)
	WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details ...string)
	NoContent(w http.ResponseWriter)
	DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error
	DecodeError(w http.ResponseWriter, r *http.Request, err error)
	ClientIP(r *http.Request) string
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

// Options configures New. DB and HTTP are required; nil values fail at New.
type Options struct {
	DB     *sql.DB
	Audit  *audit.Writer
	HTTP   HTTP
	Logger *slog.Logger

	CookieSecret      []byte // reserved for future signed cookies; server-side sessions do not need it
	SessionTTL        time.Duration
	SessionCookieName string
	CSRFCookieName    string
	Secure            bool

	AdminUsername string
	AdminPassword string
	AdminEmail    string

	Now func() time.Time // injectable clock; nil selects time.Now
}

// Service owns authentication, sessions, CSRF and user management.
type Service struct {
	db     *sql.DB
	audit  *audit.Writer
	http   HTTP
	log    *slog.Logger
	ttl    time.Duration
	sess   string // session cookie name
	csrf   string // csrf cookie name
	secure bool

	adminUsername string
	adminPassword string
	adminEmail    string

	now      func() time.Time
	limiter  *loginLimiter
	dummyPHC string // equalises login timing when the username does not exist
}

// New builds a Service. It returns an error when a required option is missing.
func New(opts Options) (*Service, error) {
	if opts.DB == nil {
		return nil, errors.New("auth: Options.DB is required")
	}
	if opts.HTTP == nil {
		return nil, errors.New("auth: Options.HTTP is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(nilWriter{}, nil))
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	sessionCookie := opts.SessionCookieName
	if sessionCookie == "" {
		sessionCookie = "sklad_session"
	}
	csrfCookie := opts.CSRFCookieName
	if csrfCookie == "" {
		csrfCookie = "sklad_csrf"
	}
	ttl := opts.SessionTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	dummyPHC, _ := HashPassword("sklad-dummy-password-for-timing")

	return &Service{
		db:            opts.DB,
		audit:         opts.Audit,
		http:          opts.HTTP,
		log:           log,
		ttl:           ttl,
		sess:          sessionCookie,
		csrf:          csrfCookie,
		secure:        opts.Secure,
		adminUsername: opts.AdminUsername,
		adminPassword: opts.AdminPassword,
		adminEmail:    opts.AdminEmail,
		now:           now,
		limiter:       newLoginLimiter(),
		dummyPHC:      dummyPHC,
	}, nil
}

// nilWriter discards log output when no logger is supplied.
type nilWriter struct{}

func (nilWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

// SeedAdmin creates the first admin account when no admin exists yet. It is
// idempotent and safe to call on every boot. When admin credentials are not
// configured it returns nil (the configuration warning covers that case).
func (s *Service) SeedAdmin(ctx context.Context) error {
	if s.adminUsername == "" || s.adminPassword == "" {
		return nil
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&count); err != nil {
		return fmt.Errorf("auth: count admins: %w", err)
	}
	if count > 0 {
		return nil
	}
	hash, err := HashPassword(s.adminPassword)
	if err != nil {
		return fmt.Errorf("auth: hash admin password: %w", err)
	}
	now := s.now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, email, password_hash, role, must_change_password, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'admin', 1, 1, ?, ?)`,
		s.adminUsername, s.adminUsername, s.adminEmail, hash, now, now,
	)
	if err != nil {
		return fmt.Errorf("auth: insert admin: %w", err)
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.Record{
			Action: audit.ActionCreate, EntityType: audit.EntityUser, EntityID: s.adminUsername,
			After: map[string]any{"role": "admin", "seeded": true},
		})
	}
	s.log.Info("seeded first admin account", "username", s.adminUsername)
	return nil
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

// LoginResult is the outcome of a successful Authenticate.
type LoginResult struct {
	Token string // raw session token (goes into the HttpOnly cookie)
	CSRF  string // CSRF token for the X-CSRF-Token header
	User  *User
}

// isWorkspaceAccount limits authenticated access to the bootstrap Admin. Legacy records remain stored but cannot establish sessions.
func (s *Service) isWorkspaceAccount(u *User) bool {
	return u != nil && (u.Role == RoleAdmin && strings.EqualFold(u.Username, s.adminUsername))
}

// Authenticate verifies credentials, enforces the rate limit, starts a session
// and records the login. It returns one of ErrInvalidCredentials,
// ErrInactiveAccount or ErrRateLimited on failure.
func (s *Service) Authenticate(ctx context.Context, username, password, ip, userAgent string) (*LoginResult, error) {
	userKey := strings.ToLower(strings.TrimSpace(username))
	now := s.now()
	if !s.limiter.allow("ip:"+ip, now) || !s.limiter.allow("user:"+userKey, now) {
		return nil, ErrRateLimited
	}

	u, hash, err := s.findUserWithHash(ctx, username)
	if err != nil {
		// Burn the same CPU as a real verify to avoid username enumeration.
		_, _ = VerifyPassword(s.dummyPHC, password)
		return nil, ErrInvalidCredentials
	}
	ok, err := VerifyPassword(hash, password)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	if !u.Active {
		return nil, ErrInactiveAccount
	}

	if !s.isWorkspaceAccount(u) {
		return nil, ErrInvalidCredentials
	}

	s.limiter.clear("ip:" + ip)
	s.limiter.clear("user:" + userKey)

	token, csrf, err := s.createSession(ctx, u.ID, ip, userAgent)
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}
	return &LoginResult{Token: token, CSRF: csrf, User: u}, nil
}

// findUserWithHash returns the user and their password hash, or a wrapped
// sql.ErrNoRows when absent.
func (s *Service) findUserWithHash(ctx context.Context, username string) (*User, string, error) {
	var u User
	var role string
	var mustChange, active int
	var lastLogin sql.NullString
	var createdAt, updatedAt, hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, display_name, email, role, must_change_password, active, last_login_at, created_at, updated_at, password_hash
		 FROM users WHERE username = ? COLLATE NOCASE`, strings.TrimSpace(username),
	).Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &role, &mustChange, &active, &lastLogin, &createdAt, &updatedAt, &hash)
	if err != nil {
		return nil, "", err
	}
	u.Role = Role(role)
	u.MustChangePassword = mustChange != 0
	u.Active = active != 0
	if lastLogin.Valid {
		if t, err := time.Parse(time.RFC3339, lastLogin.String); err == nil {
			u.LastLoginAt = &t
		}
	}
	u.CreatedAt = parseRFC3339(createdAt)
	u.UpdatedAt = parseRFC3339(updatedAt)
	return &u, hash, nil
}

// Logout deletes the session identified by rawToken.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	return s.deleteSession(ctx, rawToken)
}

// ChangePassword updates the user's password (clearing the forced-change flag)
// after verifying currentPassword.
func (s *Service) ChangePassword(ctx context.Context, userID int64, currentPassword, newPassword string) error {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&hash)
	if err != nil {
		return ErrNotFound
	}
	ok, err := VerifyPassword(hash, currentPassword)
	if err != nil || !ok {
		return ErrPasswordMismatch
	}
	newHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?`,
		newHash, now, userID,
	)
	if err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, audit.Record{
			Action: audit.ActionUpdate, EntityType: audit.EntityUser, EntityID: strconv.FormatInt(userID, 10),
			ActorUserID: &userID,
			After:       map[string]any{"password_changed": true},
		})
	}
	return nil
}

// ---------------------------------------------------------------------------
// User management (admin)
// ---------------------------------------------------------------------------

// GetUser returns a user by id.
func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// ListUsers returns all users ordered by username.
func (s *Service) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY username COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// CreateUser inserts a new user.
func (s *Service) CreateUser(ctx context.Context, username, displayName, email, password string, role Role, actor *User) (*User, error) {
	role, err := ParseRole(string(role))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(username) == "" {
		return nil, ErrUsernameTaken // reuse: empty username is treated as taken
	}
	if len(password) < 8 {
		return nil, fmt.Errorf("%w: password must be at least 8 characters", ErrValidation)
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (username, display_name, email, password_hash, role, must_change_password, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1, 1, ?, ?)`,
		strings.TrimSpace(username), displayName, email, hash, string(role), now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrUsernameTaken
		}
		return nil, fmt.Errorf("auth: insert user: %w", err)
	}
	id, _ := res.LastInsertId()
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	s.auditUser(ctx, audit.ActionCreate, u.Username, actor, nil, map[string]any{"role": string(role)})
	return u, nil
}

// UpdateUser changes a user's profile and/or role.
func (s *Service) UpdateUser(ctx context.Context, id int64, displayName, email *string, role *Role, active *bool, actor *User) (*User, error) {
	before, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	set := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if displayName != nil {
		set = append(set, "display_name = ?")
		args = append(args, *displayName)
	}
	if email != nil {
		set = append(set, "email = ?")
		args = append(args, *email)
	}
	if role != nil {
		r, err := ParseRole(string(*role))
		if err != nil {
			return nil, err
		}
		set = append(set, "role = ?")
		args = append(args, string(r))
	}
	if active != nil {
		set = append(set, "active = ?")
		args = append(args, boolToInt(*active))
	}
	if len(set) == 0 {
		return before, nil
	}
	set = append(set, "updated_at = ?")
	args = append(args, s.now().UTC().Format(time.RFC3339))
	args = append(args, id)
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET `+strings.Join(set, ", ")+` WHERE id = ?`, args...); err != nil {
		return nil, fmt.Errorf("auth: update user: %w", err)
	}
	after, err := s.GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	s.auditUser(ctx, audit.ActionUpdate, before.Username, actor, userAudit(before), userAudit(after))
	return after, nil
}

// DeactivateUser sets active=0 (users are never hard-deleted).
func (s *Service) DeactivateUser(ctx context.Context, id int64, actor *User) error {
	before, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE users SET active = 0, updated_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("auth: deactivate user: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.auditUser(ctx, audit.ActionDelete, before.Username, actor, map[string]any{"active": true}, map[string]any{"active": false})
	return nil
}

func (s *Service) auditUser(ctx context.Context, action, entityID string, actor *User, before, after any) {
	if s.audit == nil {
		return
	}
	var actorID *int64
	if actor != nil {
		actorID = &actor.ID
	}
	_ = s.audit.Record(ctx, audit.Record{
		Action: action, EntityType: audit.EntityUser, EntityID: entityID,
		ActorUserID: actorID, Before: before, After: after,
	})
}

func userAudit(u *User) map[string]any {
	return map[string]any{
		"username": u.Username, "display_name": u.DisplayName, "email": u.Email,
		"role": string(u.Role), "active": u.Active, "must_change_password": u.MustChangePassword,
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation detects SQLite's UNIQUE constraint error.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ErrValidation is wrapped by validation failures.
var ErrValidation = errors.New("validation failed")

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	User      *User  `json:"user"`
	CSRFToken string `json:"csrf_token"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// MountPublic registers the endpoints that do not require a session.
func (s *Service) MountPublic(r chi.Router) {
	r.Post("/auth/login", s.handleLogin)
}

// MountProtected registers endpoints that require a session (and, for mutating
// verbs, a valid CSRF token via the CSRF middleware).
func (s *Service) MountProtected(r chi.Router) {
	r.Post("/auth/logout", s.handleLogout)
	r.Get("/auth/me", s.handleMe)
	r.Post("/auth/change-password", s.handleChangePassword)
	r.Get("/users", s.requireAdmin(s.handleListUsers))
	r.Post("/users", s.requireAdmin(s.handleCreateUser))
	r.Patch("/users/{userId}", s.requireAdmin(s.handleUpdateUser))
	r.Delete("/users/{userId}", s.requireAdmin(s.handleDeleteUser))
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := s.http.DecodeJSON(w, r, &req); err != nil {
		s.http.DecodeError(w, r, err)
		return
	}
	if strings.TrimSpace(req.Username) == "" || req.Password == "" {
		s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "username and password are required")
		return
	}
	result, err := s.Authenticate(r.Context(), req.Username, req.Password, s.http.ClientIP(r), r.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, ErrRateLimited):
			s.http.WriteError(w, r, http.StatusTooManyRequests, CodeRateLimited, "too many login attempts; try again later")
		case errors.Is(err, ErrInactiveAccount):
			s.http.WriteError(w, r, http.StatusForbidden, CodeForbidden, "account is disabled")
		default:
			s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "invalid username or password")
		}
		return
	}
	http.SetCookie(w, s.sessionCookie(result.Token))
	http.SetCookie(w, s.csrfCookie(result.CSRF))
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": loginResponse{User: result.User, CSRFToken: result.CSRF}})
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if token := sessionTokenFrom(r, s.sess); token != "" {
		_ = s.Logout(r.Context(), token)
	}
	http.SetCookie(w, s.clearSessionCookie())
	http.SetCookie(w, s.clearCSRFCookie())
	s.http.NoContent(w)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	u := UserFrom(r.Context())
	if u == nil {
		s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
		return
	}
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": u})
}

func (s *Service) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	u := UserFrom(r.Context())
	if u == nil {
		s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
		return
	}
	var req changePasswordRequest
	if err := s.http.DecodeJSON(w, r, &req); err != nil {
		s.http.DecodeError(w, r, err)
		return
	}
	if len(req.NewPassword) < 8 {
		s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "new password must be at least 8 characters")
		return
	}
	if err := s.ChangePassword(r.Context(), u.ID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, ErrPasswordMismatch) {
			s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "current password is incorrect")
			return
		}
		if errors.Is(err, ErrNotFound) {
			s.http.WriteError(w, r, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		s.log.Error("change password failed", "error", err)
		return
	}
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.ListUsers(r.Context())
	if err != nil {
		s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}
	accounts := make([]*User, 0, 2)
	for _, user := range users {
		if s.isWorkspaceAccount(user) {
			accounts = append(accounts, user)
		}
	}
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": accounts})
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
}

func (s *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	s.http.WriteError(w, r, http.StatusForbidden, CodeForbidden, "Editor is public; additional accounts cannot be created")
}

type updateUserRequest struct {
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email"`
	Role        *string `json:"role"`
	Active      *bool   `json:"active"`
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := userIDFromPath(r)
	if err != nil {
		s.http.WriteError(w, r, http.StatusBadRequest, CodeBadRequest, "invalid user id")
		return
	}
	currentUser, loadErr := s.GetUser(r.Context(), id)
	if loadErr != nil || !s.isWorkspaceAccount(currentUser) {
		s.http.WriteError(w, r, http.StatusForbidden, CodeForbidden, "only Admin is an authenticated account")
		return
	}
	var req updateUserRequest
	if err := s.http.DecodeJSON(w, r, &req); err != nil {
		s.http.DecodeError(w, r, err)
		return
	}
	var role *Role
	if req.Role != nil {
		parsed, err := ParseRole(*req.Role)
		if err != nil {
			s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, err.Error())
			return
		}
		current, err := s.GetUser(r.Context(), id)
		if err != nil || parsed != current.Role {
			s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "account roles are fixed")
			return
		}
		role = &parsed
	}
	actor := UserFrom(r.Context())
	u, err := s.UpdateUser(r.Context(), id, req.DisplayName, req.Email, role, req.Active, actor)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.http.WriteError(w, r, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		s.log.Error("update user failed", "error", err)
		return
	}
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": u})
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := userIDFromPath(r)
	if err != nil {
		s.http.WriteError(w, r, http.StatusBadRequest, CodeBadRequest, "invalid user id")
		return
	}
	actor := UserFrom(r.Context())
	if actor != nil && actor.ID == id {
		s.http.WriteError(w, r, http.StatusBadRequest, CodeValidationFailed, "you cannot deactivate your own account")
		return
	}
	if err := s.DeactivateUser(r.Context(), id, actor); err != nil {
		if errors.Is(err, ErrNotFound) {
			s.http.WriteError(w, r, http.StatusNotFound, CodeNotFound, "user not found")
			return
		}
		s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}
	s.http.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"ok": true}})
}

// requireAdmin wraps a handler and rejects non-admin callers.
func (s *Service) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := UserFrom(r.Context())
		if u == nil || u.Role != RoleAdmin {
			s.http.WriteError(w, r, http.StatusForbidden, CodeForbidden, "admin role required")
			return
		}
		next(w, r)
	}
}

func userIDFromPath(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "userId"), 10, 64)
}
