// Package config reads and validates Sklad's runtime configuration.
//
// Every option is an environment variable; the authoritative list, its defaults
// and its semantics live in the frozen contract docs/contracts/config.md.
//
// Administrator-editable settings (Discord webhook, scheduler hour, default
// locale, …) are deliberately absent here: they live in the database `settings`
// table (see docs/contracts/schema.md) and are read through the store.
//
// Load fails fast on malformed values (an unusable log level, an unknown time
// zone, …) and reports recoverable problems through Config.Warnings, so the
// process can always start and answer /api/v1/health even when the deployment
// still needs attention.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Defaults from docs/contracts/config.md. Each one applies when its environment
// variable is unset or empty.
const (
	DefaultHTTPAddr          = ":8080"
	DefaultDataDir           = "/data"
	DefaultDBFileName        = "sklad.db"
	DefaultAdminUsername     = "admin"
	DefaultSessionCookieName = "sklad_session"
	DefaultCSRFCookieName    = "sklad_csrf"
	DefaultBaseURL           = "https://sklad.local"
	DefaultTZ                = "Europe/Prague"
	DefaultSessionTTL        = 24 * time.Hour
	DefaultLogLevel          = LogLevelInfo
	DefaultLogFormat         = LogFormatJSON
)

// Environment variable names. They are exported so that tests, scripts and the
// deployment documentation can refer to them without duplicating literals.
const EnvHTTPAddr = "SKLAD_HTTP_ADDR"
const EnvDataDir = "SKLAD_DATA_DIR"
const EnvDBPath = "SKLAD_DB_PATH"
const EnvLogLevel = "SKLAD_LOG_LEVEL"
const EnvLogFormat = "SKLAD_LOG_FORMAT"
const EnvAdminUsername = "SKLAD_ADMIN_USERNAME"
const EnvAdminPassword = "SKLAD_ADMIN_PASSWORD"
const EnvAdminEmail = "SKLAD_ADMIN_EMAIL"
const EnvCookieSecret = "SKLAD_COOKIE_SECRET"
const EnvSessionTTL = "SKLAD_SESSION_TTL"
const EnvSessionCookieName = "SKLAD_SESSION_COOKIE_NAME"
const EnvCSRFCookieName = "SKLAD_CSRF_COOKIE_NAME"
const EnvBaseURL = "SKLAD_BASE_URL"
const EnvTZ = "SKLAD_TZ"
const EnvAllowedOrigins = "SKLAD_ALLOWED_ORIGINS"
const EnvTrustProxy = "SKLAD_TRUST_PROXY"

// LogLevel is a SKLAD_LOG_LEVEL value.
type LogLevel string

const LogLevelDebug LogLevel = "debug"
const LogLevelInfo LogLevel = "info"
const LogLevelWarn LogLevel = "warn"
const LogLevelError LogLevel = "error"

// ParseLogLevel converts s into a LogLevel. An empty string selects the default.
func ParseLogLevel(s string) (LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return DefaultLogLevel, nil
	case string(LogLevelDebug):
		return LogLevelDebug, nil
	case string(LogLevelInfo):
		return LogLevelInfo, nil
	case string(LogLevelWarn):
		return LogLevelWarn, nil
	case string(LogLevelError):
		return LogLevelError, nil
	default:
		return "", fmt.Errorf("invalid log level %q (want debug, info, warn or error)", s)
	}
}

// Slog maps the level onto log/slog.
func (l LogLevel) Slog() slog.Level {
	switch l {
	case LogLevelDebug:
		return slog.LevelDebug
	case LogLevelWarn:
		return slog.LevelWarn
	case LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// LogFormat is a SKLAD_LOG_FORMAT value: json for machines, text for humans.
type LogFormat string

const LogFormatJSON LogFormat = "json"
const LogFormatText LogFormat = "text"

// ParseLogFormat converts s into a LogFormat. An empty string selects the default.
func ParseLogFormat(s string) (LogFormat, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return DefaultLogFormat, nil
	case string(LogFormatJSON):
		return LogFormatJSON, nil
	case string(LogFormatText):
		return LogFormatText, nil
	default:
		return "", fmt.Errorf("invalid log format %q (want json or text)", s)
	}
}

// LookupFunc mirrors os.LookupEnv so that tests can supply their own environment.
type LookupFunc func(key string) (string, bool)

// Config is the fully resolved and validated runtime configuration.
type Config struct {
	// Server
	HTTPAddr string
	BaseURL  string
	Secure   bool

	// Storage
	DataDir string
	DBPath  string

	// Logging
	LogLevel  LogLevel
	LogFormat LogFormat

	// First-boot admin seed
	AdminUsername string
	AdminPassword string
	AdminEmail    string

	// Sessions and CSRF
	CookieSecret      string
	SessionTTL        time.Duration
	SessionCookieName string
	CSRFCookieName    string

	// Locale and network
	TZ             string
	Location       *time.Location
	AllowedOrigins []string
	TrustProxy     bool

	// Warnings lists non-fatal configuration problems that the caller should log.
	Warnings []string
}

// Load reads the configuration from the process environment.
func Load() (*Config, error) {
	return LoadFromLookup(os.LookupEnv)
}

// LoadFromLookup resolves and validates the configuration from lookup. A nil
// lookup yields the compiled-in defaults.
func LoadFromLookup(lookup LookupFunc) (*Config, error) {
	if lookup == nil {
		lookup = func(string) (string, bool) { return "", false }
	}
	// env returns a trimmed value, or fallback when the variable is unset/blank.
	env := func(key, fallback string) string {
		if v, ok := lookup(key); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	// raw returns a trimmed value and keeps an empty string meaningful.
	raw := func(key string) string {
		if v, ok := lookup(key); ok {
			return strings.TrimSpace(v)
		}
		return ""
	}

	cfg := &Config{}

	cfg.HTTPAddr = env(EnvHTTPAddr, DefaultHTTPAddr)

	cfg.DataDir = env(EnvDataDir, DefaultDataDir)
	cfg.DBPath = env(EnvDBPath, filepath.Join(cfg.DataDir, DefaultDBFileName))

	level, err := ParseLogLevel(env(EnvLogLevel, string(DefaultLogLevel)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvLogLevel, err)
	}
	cfg.LogLevel = level

	format, err := ParseLogFormat(env(EnvLogFormat, string(DefaultLogFormat)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvLogFormat, err)
	}
	cfg.LogFormat = format

	cfg.AdminUsername = env(EnvAdminUsername, DefaultAdminUsername)
	cfg.AdminPassword = raw(EnvAdminPassword)
	cfg.AdminEmail = raw(EnvAdminEmail)

	cfg.CookieSecret = raw(EnvCookieSecret)

	ttlValue := env(EnvSessionTTL, DefaultSessionTTL.String())
	ttl, err := time.ParseDuration(ttlValue)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a valid duration (for example 24h or 30m)", EnvSessionTTL, ttlValue)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%s: must be greater than zero", EnvSessionTTL)
	}
	cfg.SessionTTL = ttl
	cfg.SessionCookieName = env(EnvSessionCookieName, DefaultSessionCookieName)
	cfg.CSRFCookieName = env(EnvCSRFCookieName, DefaultCSRFCookieName)

	base := env(EnvBaseURL, DefaultBaseURL)
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%s: %q must be an absolute http(s) URL such as https://sklad.local", EnvBaseURL, base)
	}
	cfg.BaseURL = strings.TrimRight(base, "/")
	cfg.Secure = parsed.Scheme == "https"

	cfg.TZ = env(EnvTZ, DefaultTZ)
	location, err := time.LoadLocation(cfg.TZ)
	if err != nil {
		return nil, fmt.Errorf("%s: unknown IANA time zone %q (for example Europe/Prague)", EnvTZ, cfg.TZ)
	}
	cfg.Location = location

	origins, err := parseOrigins(raw(EnvAllowedOrigins))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvAllowedOrigins, err)
	}
	cfg.AllowedOrigins = origins

	trustProxy, err := parseBool(env(EnvTrustProxy, "false"))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvTrustProxy, err)
	}
	cfg.TrustProxy = trustProxy

	cfg.Warnings = cfg.collectWarnings()
	return cfg, nil
}

// HasAdminSeed reports whether first-boot admin credentials are configured.
func (c *Config) HasAdminSeed() bool {
	return c.AdminUsername != "" && c.AdminPassword != ""
}

// LogAttrs returns the configuration as structured log attributes with every
// secret masked, ready for
//
//	logger.LogAttrs(ctx, slog.LevelInfo, "configuration loaded", cfg.LogAttrs()...)
func (c *Config) LogAttrs() []slog.Attr {
	return []slog.Attr{
		slog.String("http_addr", c.HTTPAddr),
		slog.String("base_url", c.BaseURL),
		slog.Bool("secure", c.Secure),
		slog.String("data_dir", c.DataDir),
		slog.String("db_path", c.DBPath),
		slog.String("log_level", string(c.LogLevel)),
		slog.String("log_format", string(c.LogFormat)),
		slog.String("admin_username", c.AdminUsername),
		slog.String("admin_password", mask(c.AdminPassword)),
		slog.String("admin_email", c.AdminEmail),
		slog.String("cookie_secret", mask(c.CookieSecret)),
		slog.Duration("session_ttl", c.SessionTTL),
		slog.String("session_cookie_name", c.SessionCookieName),
		slog.String("csrf_cookie_name", c.CSRFCookieName),
		slog.String("tz", c.TZ),
		slog.Bool("trust_proxy", c.TrustProxy),
		slog.Any("allowed_origins", c.AllowedOrigins),
	}
}

// collectWarnings turns "valid but suspicious" configuration into messages the
// caller can log. None of these are fatal: the API must be reachable so an
// operator can diagnose the deployment.
func (c *Config) collectWarnings() []string {
	var warnings []string
	if !c.HasAdminSeed() {
		warnings = append(warnings, EnvAdminPassword+" is not set: the first-boot admin account cannot be seeded (see docs/contracts/config.md)")
	}

	if !c.Secure {
		warnings = append(warnings, EnvBaseURL+" does not use https: browser camera access (getUserMedia) requires a secure context")
	}
	return warnings
}

// parseOrigins splits a comma-separated CORS allow-list and validates each entry.
func parseOrigins(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parsed, err := url.Parse(part)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("%q is not an absolute http(s) origin", part)
		}
		if parsed.Path != "" && parsed.Path != "/" {
			return nil, fmt.Errorf("%q must be an origin without a path", part)
		}
		origins = append(origins, strings.TrimRight(part, "/"))
	}
	if len(origins) == 0 {
		return nil, nil
	}
	return origins, nil
}

// parseBool accepts the usual truthy and falsy spellings.
func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%q is not a boolean (true or false)", s)
	}
}

// mask hides a secret behind a fixed-width placeholder.
func mask(secret string) string {
	if secret == "" {
		return ""
	}
	return "****"
}
