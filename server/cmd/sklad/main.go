// Command sklad is the Sklad server: the /api/v1 HTTP API plus the embedded
// single-page application.
//
// Configuration comes from the environment (docs/contracts/config.md). TLS, the
// sklad.local hostname and the host port mapping are handled by the Caddy
// container in front of it (deploy/).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // embed the IANA time zone database so SKLAD_TZ resolves in minimal images

	"sklad/server/internal/api"
	"sklad/server/internal/audit"
	"sklad/server/internal/auth"
	"sklad/server/internal/buildinfo"
	"sklad/server/internal/config"
	"sklad/server/internal/jobs"
	"sklad/server/internal/store"
	"sklad/web"
)

// Server timeouts and limits. The API is JSON-only and small, so these are
// generous but finite: a slow client must never pin a connection forever.
const (
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 60 * time.Second
	writeTimeout      = 60 * time.Second
	idleTimeout       = 120 * time.Second
	maxHeaderBytes    = 1 << 20
	shutdownTimeout   = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "sklad: %v\n", err)
		os.Exit(1)
	}
}

// run wires the configuration, the logger, the embedded assets and the HTTP
// server, then blocks until the process is asked to stop.
func run() error {
	if len(os.Args) > 1 && (os.Args[1] == "backup" || os.Args[1] == "reset-admin") {
		return maintenance(os.Args[1], os.Args[2:])
	}
	backupInterval := 24 * time.Hour
	if raw := os.Getenv("SKLAD_BACKUP_INTERVAL"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed < time.Minute {
			return fmt.Errorf("SKLAD_BACKUP_INTERVAL must be a duration of at least 1m")
		}
		backupInterval = parsed
	}

	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("sklad %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildTime)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	log := newLogger(cfg)

	log.LogAttrs(context.Background(), slog.LevelInfo, "configuration loaded", cfg.LogAttrs()...)
	log.Info("sklad starting",
		"version", buildinfo.Version,
		"commit", buildinfo.Commit,
		"build_time", buildinfo.BuildTime,
	)
	for _, warning := range cfg.Warnings {
		log.Warn(warning)
	}

	// The store owns its directory, but creating it here means a first boot
	// works even before the database file exists.
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		log.Warn("could not create the data directory; the store will report this when it starts",
			"data_dir", cfg.DataDir,
			"error", err,
		)
	}

	// Open (and migrate) the SQLite database and keep it alive for the whole
	// process. Health reporting is delegated to a SQLite ping.
	database, err := store.Open(context.Background(), store.Options{
		DBPath:  cfg.DBPath,
		DataDir: cfg.DataDir,
		Logger:  log,
	})
	if err != nil {
		return fmt.Errorf("store: %w", err)
	}
	defer database.Close()

	// Authentication (sessions, CSRF, RBAC) and the shared audit writer.
	auditWriter := audit.New(database.DB())
	authService, err := auth.New(auth.Options{
		DB:                database.DB(),
		Audit:             auditWriter,
		HTTP:              api.NewAuthHTTP(cfg),
		Logger:            log,
		CookieSecret:      []byte(cfg.CookieSecret),
		SessionTTL:        cfg.SessionTTL,
		SessionCookieName: cfg.SessionCookieName,
		CSRFCookieName:    cfg.CSRFCookieName,
		Secure:            cfg.Secure,
		AdminUsername:     cfg.AdminUsername,
		AdminPassword:     cfg.AdminPassword,
		AdminEmail:        cfg.AdminEmail,
	})
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := authService.SeedAdmin(context.Background()); err != nil {
		return fmt.Errorf("seed admin: %w", err)
	}

	// Background work: the overdue notification scheduler. It reads its hour,
	// webhook and language from the database on every tick, so the admin API can
	// reconfigure it without a restart.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduler, err := jobs.New(jobs.Options{
		DB:       database.DB(),
		Logger:   log,
		Audit:    auditWriter,
		Location: cfg.Location,
		AppURL:   cfg.BaseURL,
	})
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}
	go scheduler.Start(ctx)
	backupCtx, cancelBackups := context.WithCancel(ctx)
	backupsDone := make(chan struct{})
	go func() { defer close(backupsDone); jobs.StartBackups(backupCtx, database, log, backupInterval) }()
	defer func() { cancelBackups(); <-backupsDone }()

	assets, built, err := web.Assets()
	if err != nil {
		return fmt.Errorf("embedded frontend: %w", err)
	}
	if !built {
		log.Warn("no frontend bundle found in web/dist; serving a placeholder page (run `npm run build` in web/)")
	}

	handler := api.New(api.Options{
		Config:      cfg,
		Logger:      log,
		Assets:      assets,
		AssetsBuilt: built,
		Version: api.VersionInfo{
			Version:   buildinfo.Version,
			Commit:    buildinfo.Commit,
			BuildTime: buildinfo.BuildTime,
		},
		Health:    database.Health(),
		Auth:      authService,
		Audit:     auditWriter,
		DB:        database.DB(),
		Scheduler: scheduler,
	}).Handler()

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	// The channel is buffered so the goroutine can always finish, even when the
	// shutdown path below wins the select.
	serveErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "base_url", cfg.BaseURL)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Info("shutdown signal received; draining connections", "timeout", shutdownTimeout)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		if err := <-serveErr; err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		log.Info("stopped")
		return nil
	}
}

// newLogger builds the structured logger from SKLAD_LOG_LEVEL and
// SKLAD_LOG_FORMAT. Source locations are only attached in debug mode.
func newLogger(cfg *config.Config) *slog.Logger {
	options := &slog.HandlerOptions{
		Level:     cfg.LogLevel.Slog(),
		AddSource: cfg.LogLevel == config.LogLevelDebug,
	}
	var handler slog.Handler
	if cfg.LogFormat == config.LogFormatText {
		handler = slog.NewTextHandler(os.Stdout, options)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, options)
	}
	return slog.New(handler)
}
