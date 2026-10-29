package api

// Middleware, request helpers and the JSON error envelope live in this file.
// Client-facing error messages are plain English; the SPA localises them from
// the machine-readable error code (the `errors` namespace in
// web/src/locales/en.json is the authoritative mapping).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"path"
	"runtime/debug"
	"strings"
	"time"
)

// Machine-readable error codes. They are part of the API contract
// (openapi/openapi.yaml) and must not change without a version bump.
const CodeBadRequest = "bad_request"
const CodeUnauthorized = "unauthorized"
const CodeForbidden = "forbidden"
const CodeNotFound = "not_found"
const CodeMethodNotAllowed = "method_not_allowed"
const CodeConflict = "version_conflict"
const CodeValidationFailed = "validation_failed"
const CodeUnprocessable = "unprocessable"
const CodePayloadTooLarge = "payload_too_large"
const CodeRateLimited = "rate_limited"
const CodeInternal = "internal_error"

// HeaderRequestID is both accepted from and returned to clients so that a
// request can be traced through Caddy, the API and the logs.
const HeaderRequestID = "X-Request-ID"

const (
	maxRequestIDLen  = 64
	maxJSONBodyBytes = 1 << 20
)

// ErrorBody is the `error` object of the frozen error envelope.
type ErrorBody struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

// ErrorEnvelope is the single error shape returned by every endpoint.
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type contextKey int

const requestIDKey contextKey = iota

// RequestIDFrom returns the request ID attached by the RequestID middleware, or
// an empty string when the middleware is absent.
func RequestIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestID accepts a trustworthy X-Request-ID from the client (or generates
// one) and exposes it to handlers, the access log and the response headers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(HeaderRequestID))
		if !validRequestID(id) {
			id = newRequestID()
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// newRequestID returns a random 128-bit identifier in hex.
func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// crypto/rand does not fail in practice; stay traceable if it ever does.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

// validRequestID accepts short, printable, non-empty client-supplied IDs so the
// value can never be used to forge log lines or response headers.
func validRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// responseRecorder captures the status code and body size for the access log
// without changing the response.
type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rec *responseRecorder) WriteHeader(status int) {
	if rec.status == 0 {
		rec.status = status
		rec.ResponseWriter.WriteHeader(status)
	}
}

func (rec *responseRecorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := rec.ResponseWriter.Write(p)
	rec.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying ResponseWriter, so
// flushing and hijacking keep working behind this middleware.
func (rec *responseRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

// statusCode reports the status that was written, defaulting to 200.
func (rec *responseRecorder) statusCode() int {
	if rec.status == 0 {
		return http.StatusOK
	}
	return rec.status
}

// AccessLog writes one structured line per request once the handler returns, so
// every response — including panics recovered by Recoverer — is accounted for.
// Static assets are logged at debug level to keep production logs readable.
func AccessLog(log *slog.Logger, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)

			status := rec.statusCode()
			attrs := []slog.Attr{
				slog.String("request_id", RequestIDFrom(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", rec.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("ip", ClientIP(r, trustProxy)),
			}
			if agent := r.UserAgent(); agent != "" {
				attrs = append(attrs, slog.String("user_agent", agent))
			}

			switch {
			case status >= http.StatusInternalServerError:
				log.LogAttrs(r.Context(), slog.LevelError, "request failed", attrs...)
			case status >= http.StatusBadRequest:
				log.LogAttrs(r.Context(), slog.LevelWarn, "request rejected", attrs...)
			case isStaticRequest(r.URL.Path):
				log.LogAttrs(r.Context(), slog.LevelDebug, "asset served", attrs...)
			default:
				log.LogAttrs(r.Context(), slog.LevelInfo, "request served", attrs...)
			}
		})
	}
}

// Recoverer turns a panic into a logged 500 with the standard error envelope
// instead of a dropped connection and a stack trace on stdout.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				// http.ErrAbortHandler is the documented way for a handler to
				// abort a response; let net/http deal with it.
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}
				log.Error("panic recovered",
					"request_id", RequestIDFrom(r.Context()),
					"method", r.Method,
					"path", r.URL.Path,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				Error(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets conservative headers for every response.
//
// The CSP is deliberately explicit because the SPA needs three allowances the
// defaults would forbid: `wasm-unsafe-eval` for the ZXing scanner fallback,
// `blob:` for camera streams and scanner workers, and `'unsafe-inline'` for
// Tailwind/inline style attributes. Tighten this list, never widen it, without
// recorded evidence.
func SecurityHeaders(secure bool) func(http.Handler) http.Handler {
	policy := strings.Join([]string{
		"default-src 'self'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
		"object-src 'none'",
		"img-src 'self' data: blob:",
		"media-src 'self' blob:",
		"font-src 'self' data:",
		"style-src 'self' 'unsafe-inline'",
		"script-src 'self' 'wasm-unsafe-eval'",
		"worker-src 'self' blob:",
		"connect-src 'self'",
	}, "; ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("Content-Security-Policy", policy)
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("X-Frame-Options", "DENY")
			header.Set("Referrer-Policy", "no-referrer")
			header.Set("Cross-Origin-Opener-Policy", "same-origin")
			// Camera access is the app's core feature; everything else is denied.
			header.Set("Permissions-Policy", "camera=(self), microphone=(), geolocation=(), payment=()")
			if secure {
				header.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORS applies the SKLAD_ALLOWED_ORIGINS allow-list. An empty list means
// same-origin only, which is the production default: Caddy serves the SPA from
// the same origin as the API.
func CORS(allowed []string) func(http.Handler) http.Handler {
	allow := make(map[string]struct{}, len(allowed))
	for _, origin := range allowed {
		allow[normalizeOrigin(origin)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			// Responses vary by Origin even when the origin is not allowed.
			w.Header().Add("Vary", "Origin")
			preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""

			if _, ok := allow[normalizeOrigin(origin)]; !ok {
				if preflight {
					Error(w, r, http.StatusForbidden, CodeForbidden, "origin not allowed")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			header := w.Header()
			header.Set("Access-Control-Allow-Origin", origin)
			header.Set("Access-Control-Allow-Credentials", "true")
			header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			header.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, "+HeaderRequestID)
			header.Set("Access-Control-Max-Age", "600")
			if preflight {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// normalizeOrigin lower-cases an origin and drops a trailing slash so that
// comparisons are stable.
func normalizeOrigin(origin string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(origin), "/"))
}

// ClientIP returns the client address for logging and rate limiting. Forwarded
// headers are only trusted when SKLAD_TRUST_PROXY is enabled (Caddy sets them).
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			if ip := net.ParseIP(strings.TrimSpace(first)); ip != nil {
				return ip.String()
			}
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			if ip := net.ParseIP(real); ip != nil {
				return ip.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// WriteJSON serialises v with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	// The status line is already sent; a failure here can only truncate the body.
	_ = encoder.Encode(v)
}

// NoContent writes an empty 204 response.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Error writes the frozen error envelope with the given HTTP status.
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string, details ...string) {
	body := ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}}
	if len(details) > 0 {
		body.Error.Details = details
	}
	if r != nil {
		if id := RequestIDFrom(r.Context()); id != "" {
			w.Header().Set(HeaderRequestID, id)
		}
	}
	WriteJSON(w, status, body)
}

// InternalError logs the underlying error and returns a generic 500 so internal
// details never reach the client.
func InternalError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	if log != nil {
		log.Error("request failed",
			"error", err,
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
		)
	}
	Error(w, r, http.StatusInternalServerError, CodeInternal, "internal server error")
}

// DecodeJSON reads a single JSON object from the request body, rejecting
// oversized bodies and non-JSON content types. Unknown fields are ignored so
// that older clients keep working after the schema grows.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return fmt.Errorf("expected Content-Type application/json, got %q", contentType)
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

// DecodeError maps a DecodeJSON failure onto the error envelope: 413 for bodies
// over the limit, 400 with the offending code for anything else.
func DecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		Error(w, r, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			fmt.Sprintf("request body must not exceed %d bytes", maxJSONBodyBytes))
		return
	}
	Error(w, r, http.StatusBadRequest, CodeBadRequest, "malformed JSON request body", err.Error())
}

// Chain applies middleware to h in the order given: Chain(h, a, b) runs a
// before b. It exists for tests and for handlers mounted outside the router.
func Chain(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}

// isStaticRequest reports whether p addresses a bundled asset rather than the
// API, so the access log can keep asset noise at debug level.
func isStaticRequest(p string) bool {
	if strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/resources/") {
		return true
	}
	switch strings.ToLower(path.Ext(p)) {
	case ".js", ".mjs", ".css", ".map", ".svg", ".png", ".jpg", ".jpeg", ".gif",
		".webp", ".avif", ".ico", ".woff", ".woff2", ".ttf", ".otf", ".wasm", ".webmanifest":
		return true
	default:
		return false
	}
}
