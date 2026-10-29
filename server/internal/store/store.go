// Package store owns the SQLite persistence layer: opening and configuring the
// database, applying the embedded schema migrations, running transactions, and
// taking online backups and JSON snapshots.
//
// The schema is the frozen contract in docs/contracts/schema.md; each numbered
// file under migrations/ corresponds to one schema version and is applied
// exactly once, in order, by the migration runner below.
//
// The driver is modernc.org/sqlite, a pure-Go SQLite implementation, so the
// server binary stays CGO-free and cross-compiles for the QNAP (linux/amd64)
// without a C toolchain.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DriverName is the database/sql driver name registered by modernc.org/sqlite.
const DriverName = "sqlite"

// Backup retention defaults (see docs/contracts/config.md). They are exported
// so callers can document or override them in one place.
const (
	DefaultDBBackupRetention = 50 // VACUUM INTO database files kept
	DefaultSnapshotRetention = 100
	DefaultMaxOpenConns      = 4
	DefaultBusyTimeoutMS     = 5000
)

// schemaMigrationsTable is created by the runner (not by any migration) so that
// it can record which migration files have been applied.
const schemaMigrationsTable = "schema_migrations"

// migrationsFS embeds every *.sql file under migrations/. Files are applied in
// lexicographic order of their base name, so use zero-padded numeric prefixes.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Options configures Store.Open. DBPath and DataDir are required.
type Options struct {
	// DBPath is the SQLite file path (for example /data/sklad.db).
	DBPath string
	// DataDir is the persisted volume root; backups and snapshots are written
	// to <DataDir>/backups. Defaults to the directory containing DBPath.
	DataDir string
	// Logger receives lifecycle and migration events; nil selects a discarding
	// logger.
	Logger *slog.Logger
	// Now supplies timestamps (backup file names, snapshot metadata); nil
	// selects time.Now. Inject a fixed clock in tests.
	Now func() time.Time
}

// Store wraps a *sql.DB with the behaviour the rest of the server relies on:
// WAL journal, foreign keys, a busy timeout and immediate write transactions.
type Store struct {
	db      *sql.DB
	dataDir string
	logger  *slog.Logger
	now     func() time.Time
}

// Open creates or opens the database, configures it, applies pending migrations
// and verifies connectivity. The caller owns the returned Store and must call
// Close when done.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.DBPath == "" {
		return nil, errors.New("store: Options.DBPath is required")
	}
	if opts.DataDir == "" {
		opts.DataDir = filepath.Dir(opts.DBPath)
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}

	if err := os.MkdirAll(filepath.Dir(opts.DBPath), 0o755); err != nil {
		return nil, fmt.Errorf("store: create database directory: %w", err)
	}

	// _txlock=immediate makes BeginTx issue BEGIN IMMEDIATE, acquiring the write
	// lock up front and avoiding the deferred-upgrade SQLITE_BUSY deadlock when
	// two connections read first and then try to write.
	dsn := filepath.ToSlash(opts.DBPath) +
		"?_txlock=immediate" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=busy_timeout(" + strconv.Itoa(DefaultBusyTimeoutMS) + ")" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)"

	db, err := sql.Open(DriverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	db.SetMaxOpenConns(DefaultMaxOpenConns)
	db.SetMaxIdleConns(DefaultMaxOpenConns)
	db.SetConnMaxLifetime(0)

	s := &Store{db: db, dataDir: opts.DataDir, logger: opts.Logger, now: opts.Now}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}

	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return s, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// DB exposes the pool for advanced callers (read-only queries, domain repos in
// later tasks). Mutations should prefer WithTx.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Health returns a checker suitable for api.Options.Health: it reports whether
// SQLite answers a ping within the caller's timeout.
func (s *Store) Health() func(ctx context.Context) error {
	return s.db.PingContext
}

// Ping verifies connectivity directly (used by tests and the health endpoint).
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// WithTx runs fn inside a single write transaction. It commits when fn returns
// nil and rolls back otherwise, so a panic or a forgotten rollback can never
// leave a half-open transaction behind.
func (s *Store) WithTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

// migrate applies every pending migration in lexicographic file order inside a
// single transaction per migration. Applied versions are recorded in
// schema_migrations; re-running is a no-op. The schema is append-only: shipped
// migrations are never edited, only new numbered files are added.
func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+schemaMigrationsTable+` (
		version    TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: create %s: %w", schemaMigrationsTable, err)
	}

	applied := make(map[string]struct{})
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM `+schemaMigrationsTable)
	if err != nil {
		return fmt.Errorf("store: read %s: %w", schemaMigrationsTable, err)
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			_ = rows.Close()
			return fmt.Errorf("store: scan %s: %w", schemaMigrationsTable, err)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("store: close %s rows: %w", schemaMigrationsTable, err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, filepath.Ext(name))
		if _, ok := applied[version]; ok {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		if err := s.applyMigration(ctx, version, string(body)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		s.logger.Info("migration applied", "version", version)
	}
	return nil
}

// applyMigration runs one migration file and records it atomically. SQLite
// executes DDL transactionally, so the version row and the schema change commit
// together; a failure leaves neither behind.
func (s *Store) applyMigration(ctx context.Context, version, body string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, body); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO `+schemaMigrationsTable+` (version, applied_at) VALUES (?, ?)`,
			version, s.now().UTC().Format(time.RFC3339),
		)
		return err
	})
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

// BackupDB writes a compact, transactionally consistent copy of the database
// using VACUUM INTO and prunes old copies down to DefaultDBBackupRetention.
// It returns the absolute path of the file it created.
func (s *Store) BackupDB(ctx context.Context) (string, error) {
	dir := filepath.Join(s.dataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("store: create backup dir: %w", err)
	}
	// Reserve a unique private staging file. Only complete backups receive .db,
	// so NAS copy jobs never mistake a running VACUUM for a finished snapshot.
	pending, err := os.CreateTemp(dir, "sklad_"+s.now().UTC().Format("20060102_150405.000")+"_*.pending")
	if err != nil {
		return "", err
	}
	temp := pending.Name()
	if err := pending.Close(); err != nil {
		os.Remove(temp)
		return "", err
	}
	defer os.Remove(temp)
	query := "VACUUM INTO " + sqlLiteral(temp)
	if _, err := s.db.ExecContext(ctx, query); err != nil {
		return "", fmt.Errorf("store: backup db: %w", err)
	}
	// Flush the completed file before publishing its name.
	completed, err := os.OpenFile(temp, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	syncErr := completed.Sync()
	closeErr := completed.Close()
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	dst := strings.TrimSuffix(temp, ".pending") + ".db"
	if err := os.Rename(temp, dst); err != nil {
		return "", err
	}

	pruneFiles(dir, "sklad_*.db", DefaultDBBackupRetention)
	return dst, nil
}

// Snapshot writes a human-readable JSON dump of the durable tables (items,
// options, users, settings, notification channels) for disaster recovery and
// inspection, and prunes old snapshots down to DefaultSnapshotRetention.
func (s *Store) Snapshot(ctx context.Context) (string, error) {
	dir := filepath.Join(s.dataDir, "backups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("store: create backup dir: %w", err)
	}

	snap := struct {
		SchemaVersion int              `json:"schema_version"`
		CreatedAt     string           `json:"created_at"`
		ItemTypes     []map[string]any `json:"item_types"`
		Statuses      []map[string]any `json:"statuses"`
		Items         []map[string]any `json:"items"`
		Personnel     []map[string]any `json:"personnel"`
		Users         []map[string]any `json:"users"`
		Settings      []map[string]any `json:"settings"`
		Channels      []map[string]any `json:"notification_channels"`
	}{
		CreatedAt: s.now().UTC().Format(time.RFC3339),
	}

	var err error
	if snap.SchemaVersion, err = s.schemaVersion(ctx); err != nil {
		return "", err
	}
	if snap.ItemTypes, err = dumpTable(ctx, s.db, "item_types", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot item_types: %w", err)
	}
	if snap.Statuses, err = dumpTable(ctx, s.db, "statuses", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot statuses: %w", err)
	}
	if snap.Personnel, err = dumpTable(ctx, s.db, "personnel", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot personnel: %w", err)
	}
	if snap.Items, err = dumpTable(ctx, s.db, "items", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot items: %w", err)
	}
	if snap.Users, err = dumpTable(ctx, s.db, "users", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot users: %w", err)
	}
	if snap.Settings, err = dumpTable(ctx, s.db, "settings", "key"); err != nil {
		return "", fmt.Errorf("store: snapshot settings: %w", err)
	}
	if snap.Channels, err = dumpTable(ctx, s.db, "notification_channels", "id"); err != nil {
		return "", fmt.Errorf("store: snapshot notification_channels: %w", err)
	}

	body, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("store: marshal snapshot: %w", err)
	}

	dst := s.backupPath(dir, "snapshot_", ".json")
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		return "", fmt.Errorf("store: write snapshot: %w", err)
	}

	pruneFiles(dir, "snapshot_*.json", DefaultSnapshotRetention)
	return dst, nil
}

// schemaVersion returns the number of migrations applied.
func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+schemaMigrationsTable,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("store: read schema version: %w", err)
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// dumpTable reads every row of table (ordered by orderCol) as a generic
// map[string]any, converting SQLite's []byte TEXT/BLOB values to strings so the
// result marshals to clean JSON.
func dumpTable(ctx context.Context, db *sql.DB, table, orderCol string) ([]map[string]any, error) {
	query := "SELECT * FROM " + table
	if orderCol != "" {
		query += " ORDER BY " + orderCol
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = normalizeValue(values[i])
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// normalizeValue turns SQLite []byte values into strings for JSON output.
func normalizeValue(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	default:
		return v
	}
}

// sqlLiteral returns s as a single-quoted SQL string literal.
func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// backupPath returns a collision-free backup/snapshot path inside dir. The
// millisecond-resolution timestamp makes collisions practically impossible; the
// counter suffix guarantees uniqueness for same-millisecond re-runs.
func (s *Store) backupPath(dir, prefix, ext string) string {
	base := prefix + s.now().UTC().Format("20060102_150405.000")
	dst := filepath.Join(dir, base+ext)
	for i := 1; ; i++ {
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			return dst
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, i, ext))
	}
}

// pruneFiles keeps the newest keep files matching pattern in dir. Backup and
// snapshot names embed fixed-width, sortable timestamps, so a lexicographic
// sort orders them oldest-first.
func pruneFiles(dir, pattern string, keep int) {
	if keep <= 0 {
		return
	}
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil || len(matches) <= keep {
		return
	}
	sort.Strings(matches)
	for _, match := range matches[:len(matches)-keep] {
		_ = os.Remove(match)
	}
}
