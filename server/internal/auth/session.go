package auth

// Server-side sessions, CSRF tokens, cookies and the HTTP middleware that turns
// a session cookie into a request-scoped identity (auth.UserFrom / SessionFrom).

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"sklad/server/internal/audit"
)

// Session is a row of the sessions table. TokenHash is the SHA-256 of the raw
// bearer token held by the client cookie; the raw token never touches disk.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
	IP        string
	UserAgent string
}

// randomToken returns a URL-safe 256-bit random token.
func randomToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand does not fail in practice; fall back to a unique value so
		// the caller can still proceed instead of panicking.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// hashToken derives the storage key from a raw session token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// createSession starts a session, updates last_login_at and records the login
// audit event atomically. It returns the raw token (cookie value) and the CSRF
// token (X-CSRF-Token header value).
func (s *Service) createSession(ctx context.Context, userID int64, ip, userAgent string) (token, csrf string, err error) {
	token = randomToken()
	csrf = randomToken()
	now := s.now()
	expiresAt := now.Add(s.ttl).UTC()
	nowStr := now.UTC().Format(time.RFC3339)
	expiresStr := expiresAt.Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", fmt.Errorf("auth: begin session transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at, created_at, ip, user_agent)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hashToken(token), userID, csrf, expiresStr, nowStr, ip, userAgent,
	); err != nil {
		return "", "", fmt.Errorf("auth: insert session: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, nowStr, userID); err != nil {
		return "", "", fmt.Errorf("auth: update last login: %w", err)
	}
	if s.audit != nil {
		if err = s.audit.RecordTx(ctx, tx, audit.Record{
			Action:      audit.ActionLogin,
			EntityType:  audit.EntitySession,
			EntityID:    strconv.FormatInt(userID, 10),
			ActorUserID: &userID,
			IP:          ip,
			UserAgent:   userAgent,
		}); err != nil {
			return "", "", fmt.Errorf("auth: audit login: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return "", "", fmt.Errorf("auth: commit session: %w", err)
	}

	s.PruneExpiredSessions(ctx)
	return token, csrf, nil
}

// GetSession resolves a raw cookie token to its session and user. It returns
// (nil, nil, nil) for a missing, expired or inactive session so callers can
// treat "no session" uniformly.
func (s *Service) GetSession(ctx context.Context, rawToken string) (*Session, *User, error) {
	now := s.now().UTC().Format(time.RFC3339)
	var sess Session
	var expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, csrf_token, expires_at, ip, user_agent
		 FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		hashToken(rawToken), now,
	).Scan(&sess.TokenHash, &sess.UserID, &sess.CSRF, &expires, &sess.IP, &sess.UserAgent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	sess.ExpiresAt = parseRFC3339(expires)

	u, err := s.GetUser(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if !u.Active {
		return nil, nil, nil
	}
	return &sess, u, nil
}

// deleteSession removes a session row (logout).
func (s *Service) deleteSession(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, hashToken(rawToken))
	return err
}

// PruneExpiredSessions removes expired rows. It is opportunistic: failures are
// logged, not propagated, so a busy database cannot take down a login.
func (s *Service) PruneExpiredSessions(ctx context.Context) {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.now().UTC().Format(time.RFC3339)); err != nil {
		s.log.Warn("failed to prune expired sessions", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Cookies
// ---------------------------------------------------------------------------

func (s *Service) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     s.sess,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  s.now().Add(s.ttl),
		MaxAge:   int(s.ttl.Seconds()),
	}
}

func (s *Service) clearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name: s.sess, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0),
	}
}

func (s *Service) csrfCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name: s.csrf, Value: token, Path: "/", Secure: s.secure,
		SameSite: http.SameSiteLaxMode, Expires: s.now().Add(s.ttl), MaxAge: int(s.ttl.Seconds()),
	}
}

func (s *Service) clearCSRFCookie() *http.Cookie {
	return &http.Cookie{
		Name: s.csrf, Path: "/", Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0),
	}
}

// sessionTokenFrom reads the session bearer token from the request cookies.
func sessionTokenFrom(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ---------------------------------------------------------------------------
// Request context
// ---------------------------------------------------------------------------

type ctxKey int

const (
	sessionCtxKey ctxKey = iota
	userCtxKey
)

// WithSession stores a session in the request context.
func WithSession(ctx context.Context, s *Session) context.Context {
	return context.WithValue(ctx, sessionCtxKey, s)
}

// SessionFrom returns the session stored by RequireAuth, or nil.
func SessionFrom(ctx context.Context) *Session {
	s, _ := ctx.Value(sessionCtxKey).(*Session)
	return s
}

// WithUser stores a user in the request context.
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// UserFrom returns the authenticated user stored by RequireAuth, or nil.
func UserFrom(ctx context.Context) *User {
	u, _ := ctx.Value(userCtxKey).(*User)
	return u
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

// OptionalAuth identifies signed-in staff on otherwise public read endpoints.
func (s *Service) OptionalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token := sessionTokenFrom(r, s.sess); token != "" {
			sess, user, err := s.GetSession(r.Context(), token)
			if err != nil {
				s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "session lookup failed")
				return
			}
			if sess != nil && s.isWorkspaceAccount(user) && !user.MustChangePassword {
				r = r.WithContext(WithUser(WithSession(r.Context(), sess), user))
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth resolves the session cookie and places the session and user in
// the request context. It rejects anonymous, expired or inactive sessions and
// enforces the forced password change gate.
func (s *Service) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := sessionTokenFrom(r, s.sess)
		if token == "" {
			s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
			return
		}
		sess, user, err := s.GetSession(r.Context(), token)
		if err != nil {
			s.log.Error("session lookup failed", "error", err)
			s.http.WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
			return
		}
		if sess == nil || user == nil || !s.isWorkspaceAccount(user) {
			s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
			return
		}
		if user.MustChangePassword && !passwordChangeAllowed(r.URL.Path) {
			s.http.WriteError(w, r, http.StatusForbidden, CodePasswordChangeRequired, "password change required")
			return
		}
		ctx := WithUser(WithSession(r.Context(), sess), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// passwordChangeAllowed lists the endpoints reachable while the forced password
// change flag is set: the user can inspect themselves and change their password,
// and nothing else.
func passwordChangeAllowed(path string) bool {
	switch path {
	case "/api/v1/auth/me", "/api/v1/auth/logout", "/api/v1/auth/change-password":
		return true
	default:
		return false
	}
}

// RequireRole guards a handler behind one or more roles. It must run after
// RequireAuth (it reads the user from the request context).
func (s *Service) RequireRole(roles ...Role) func(http.Handler) http.Handler {
	allowed := make(map[Role]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFrom(r.Context())
			if u == nil {
				s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
				return
			}
			if _, ok := allowed[u.Role]; !ok {
				s.http.WriteError(w, r, http.StatusForbidden, CodeForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CSRF protects mutating verbs with the synchronizer-token pattern: the
// X-CSRF-Token header must equal the session's stored token. Safe methods
// (GET/HEAD/OPTIONS) pass through. It must run after RequireAuth.
func (s *Service) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		sess := SessionFrom(r.Context())
		if sess == nil {
			s.http.WriteError(w, r, http.StatusUnauthorized, CodeUnauthorized, "authentication required")
			return
		}
		got := r.Header.Get("X-CSRF-Token")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRF)) != 1 {
			s.http.WriteError(w, r, http.StatusForbidden, CodeCSRFFailed, "invalid or missing CSRF token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}
