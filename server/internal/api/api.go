// Package api hosts Sklad's HTTP layer: the router, the middleware chain and
// the handlers.
//
// Every response a client sees goes through this package, which guarantees a
// uniform error envelope, a request ID and the security headers. New endpoint
// groups are mounted in API.Handler; the helpers in middleware.go are the only
// sanctioned way to write JSON or an error.
package api

import (
	"context"
	"database/sql"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
	"sklad/server/internal/config"
)

// VersionInfo is the build metadata served by GET /api/v1/version.
type VersionInfo struct {
	Version   string
	Commit    string
	BuildTime string
}

// HealthChecker reports whether an external dependency is healthy. It is wired
// by the store task once SQLite exists; nil means "nothing to check yet".
type HealthChecker func(ctx context.Context) error

// Options configures a new API instance.
type Options struct {
	Config      *config.Config
	Logger      *slog.Logger
	Assets      fs.FS
	AssetsBuilt bool
	Health      HealthChecker
	Version     VersionInfo

	// Auth is the authentication service (sessions, CSRF, RBAC). When nil, the
	// auth and domain routes are not mounted (useful for health-only tests).
	Auth *auth.Service
	// Audit is the shared audit writer used by auth and domain handlers.
	Audit *audit.Writer
	// Scheduler runs the overdue notification check on demand (the manual
	// "run now" endpoint). When nil, that endpoint reports 503.
	Scheduler OverdueRunner
	// DB is the SQLite pool backing the domain endpoints (items, options,
	// export). When nil, the domain routes are not mounted.
	DB *sql.DB
	// Now supplies the current time to handlers that need a calendar date
	// (overdue filters, timestamps). Nil selects time.Now; inject a fixed clock
	// in tests.
	Now func() time.Time
}

// API holds the dependencies shared by all handlers.
type API struct {
	cfg       *config.Config
	log       *slog.Logger
	assets    fs.FS
	built     bool
	health    HealthChecker
	version   VersionInfo
	auth      *auth.Service
	audit     *audit.Writer
	db        *sql.DB
	now       func() time.Time
	scheduler OverdueRunner
}

// New builds an API instance. Config is required; a nil logger is replaced by a
// discarding logger so handlers never have to nil-check.
func New(opts Options) *API {
	if opts.Config == nil {
		panic("api.New: Options.Config is required")
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	version := opts.Version
	if version.Version == "" {
		version.Version = "dev"
	}
	if version.Commit == "" {
		version.Commit = "none"
	}
	if version.BuildTime == "" {
		version.BuildTime = "unknown"
	}
	if version.BuildTime == "" {
		version.BuildTime = "unknown"
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	a := &API{
		cfg:       opts.Config,
		log:       log,
		assets:    opts.Assets,
		built:     opts.AssetsBuilt,
		health:    opts.Health,
		version:   version,
		auth:      opts.Auth,
		audit:     opts.Audit,
		db:        opts.DB,
		now:       now,
		scheduler: opts.Scheduler,
	}
	return a
}

// Handler returns the fully wired HTTP handler: global middleware, the /api/v1
// subrouter and the embedded SPA fallback.
func (a *API) Handler() http.Handler {
	router := chi.NewRouter()

	// Order matters. RequestID runs first so that every log line and every error
	// can carry it; AccessLog wraps the panic guard so a recovered panic is
	// still logged with a status code.
	router.Use(RequestID)
	router.Use(AccessLog(a.log, a.cfg.TrustProxy))
	router.Use(Recoverer(a.log))
	router.Use(SecurityHeaders(a.cfg.Secure))
	router.Use(CORS(a.cfg.AllowedOrigins))

	router.Route("/api/v1", func(r chi.Router) {
		// An API 404 or 405 must be JSON, never the SPA shell.
		r.NotFound(a.notFound)
		r.MethodNotAllowed(a.methodNotAllowed)
		a.mountSystemRoutes(r)

		// Inventory, options, personnel and scan lookup are public reads.
		// Rental writes have a dedicated guard; administration and exports require Admin.
		if a.db != nil {
			a.mountPublicDomainRoutes(r)
		}

		// Auth: login is public; the protected group below requires a session
		// and a CSRF token for mutating verbs. Domain routes (items, options,
		// export, bulk, settings, notifications) hang off the same group.
		if a.auth != nil {
			a.auth.MountPublic(r)
			r.Group(func(protected chi.Router) {
				protected.Use(a.auth.RequireAuth)
				protected.Use(a.auth.CSRF)
				a.auth.MountProtected(protected)
				a.mountDomainRoutes(protected)
			})
		}
	})

	// Anything outside /api/v1 is the SPA or one of its assets. serveSPA checks
	// the /api/ prefix as well, so an unrouted API path can never return HTML.
	// "/" is registered explicitly so the app shell resolves at the root
	// regardless of how the catch-all pattern is matched.
	router.Get("/", a.serveSPA)
	router.Get("/*", a.serveSPA)
	router.NotFound(a.notFound)
	router.MethodNotAllowed(a.methodNotAllowed)

	return router
}

// notFound is the JSON 404 used for unknown API routes and unknown assets.
func (a *API) notFound(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusNotFound, CodeNotFound, "resource not found")
}

// methodNotAllowed is the JSON 405 used for a known path with the wrong method.
func (a *API) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	Error(w, r, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed")
}

// mountPublicDomainRoutes registers the domain endpoints that are reachable
// without a session. Scan resolution uses POST but never changes equipment.
func (a *API) mountPublicDomainRoutes(r chi.Router) {
	a.mountPublicOptionsRoutes(r)
	r.Group(func(read chi.Router) {
		if a.auth != nil {
			read.Use(a.auth.OptionalAuth)
		}
		read.Get("/items", a.handleListItems)
		read.Get("/items/{itemId}", a.handleGetItem)
		read.Post("/items/resolve", a.handleResolveItems)
		read.Get("/personnel", a.handleListPersonnel)
		read.With(a.rentalWriteGuard).Patch("/items/{itemId}", a.handlePatchItem)
		read.With(a.rentalWriteGuard).Post("/items/bulk", a.handleBulkUpdate)
	})
}

// mountDomainRoutes is the protected surface for the domain endpoint groups
// (items, options, export, bulk, settings, notifications). Task 2.d fills this
// in; auth routes are already mounted separately by auth.MountProtected.
func (a *API) mountDomainRoutes(r chi.Router) {
	a.mountItemRoutes(r)
	a.mountExportRoutes(r)
	a.mountOptionsRoutes(r)
	a.mountPersonnelRoutes(r)
	a.mountSettingsRoutes(r)
	a.mountNotifyRoutes(r)
}

// authHTTP adapts this package's response helpers to the auth.HTTP interface so
// the auth package writes the same JSON envelope, request-ID echo, body decoder
// and client-IP resolution as every other handler, without importing api (which
// would form an import cycle).
type authHTTP struct{ cfg *config.Config }

// NewAuthHTTP returns the auth.HTTP implementation backed by this package's
// helpers, bound to cfg's proxy-trust setting.
func NewAuthHTTP(cfg *config.Config) auth.HTTP {
	if cfg == nil {
		cfg = &config.Config{}
	}
	return authHTTP{cfg: cfg}
}

func (a authHTTP) WriteJSON(w http.ResponseWriter, status int, v any) {
	WriteJSON(w, status, v)
}

func (a authHTTP) WriteError(w http.ResponseWriter, r *http.Request, status int, code, message string, details ...string) {
	Error(w, r, status, code, message, details...)
}

func (a authHTTP) NoContent(w http.ResponseWriter) {
	NoContent(w)
}

func (a authHTTP) DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	return DecodeJSON(w, r, dst)
}

func (a authHTTP) DecodeError(w http.ResponseWriter, r *http.Request, err error) {
	DecodeError(w, r, err)
}

func (a authHTTP) ClientIP(r *http.Request) string {
	return ClientIP(r, a.cfg.TrustProxy)
}
