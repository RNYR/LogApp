package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore opens a store on a fresh temp directory.
func newTestStore(t *testing.T, now func() time.Time) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	opts := Options{
		DBPath:  filepath.Join(dir, "sklad.db"),
		DataDir: dir,
		Now:     now,
	}
	store, err := Open(context.Background(), opts)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

func TestOpenRequiresDBPath(t *testing.T) {
	t.Parallel()
	if _, err := Open(context.Background(), Options{}); err == nil {
		t.Fatal("expected an error when Options.DBPath is empty")
	}
}

// TestMigrationsApplyOnceAndSurviveReopen is the single most important store
// guarantee: opening the database creates the schema, and opening it again
// neither loses data nor re-applies a migration.
func TestMigrationsApplyOnceAndSurviveReopen(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store, dir := newTestStore(t, func() time.Time { return fixed })
	ctx := context.Background()

	// Every table from docs/contracts/schema.md must exist.
	wantTables := []string{
		"users", "sessions", "item_types", "statuses", "items", "audit_log",
		"settings", "notification_channels", "notification_deliveries",
		"scan_events", schemaMigrationsTable,
	}
	for _, table := range wantTables {
		var name string
		err := store.DB().QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %q missing: %v", table, err)
		}
	}

	var applied int
	if err := store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+schemaMigrationsTable).Scan(&applied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if applied == 0 {
		t.Fatal("expected at least one applied migration")
	}
	versionBefore, err := store.schemaVersion(ctx)
	if err != nil {
		t.Fatalf("schemaVersion: %v", err)
	}

	// Data written through the store must survive a reopen.
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO item_types (value, label_en, sort_order, created_at, updated_at)
		 VALUES ('Camera', 'Camera', 0, ?, ?)`, "2026-03-15T10:00:00Z", "2026-03-15T10:00:00Z"); err != nil {
		t.Fatalf("insert item type: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO items (asset_tag, name, sn, version, archived, created_at, updated_at)
		 VALUES ('PV-001', 'Camera A', 'SN-1', 1, 0, ?, ?)`, "2026-03-15T10:00:00Z", "2026-03-15T10:00:00Z"); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(ctx, Options{
		DBPath:  filepath.Join(dir, "sklad.db"),
		DataDir: dir,
		Now:     func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	var name string
	if err := reopened.DB().QueryRowContext(ctx,
		`SELECT name FROM items WHERE asset_tag = 'PV-001'`).Scan(&name); err != nil {
		t.Fatalf("data lost after reopen: %v", err)
	}
	if name != "Camera A" {
		t.Fatalf("unexpected item name after reopen: %q", name)
	}
	versionAfter, err := reopened.schemaVersion(ctx)
	if err != nil {
		t.Fatalf("schemaVersion after reopen: %v", err)
	}
	if versionAfter != versionBefore {
		t.Fatalf("migrations re-applied on reopen: %d -> %d", versionBefore, versionAfter)
	}
	var appliedAfter int
	if err := reopened.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM `+schemaMigrationsTable).Scan(&appliedAfter); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if appliedAfter != applied {
		t.Fatalf("migration rows duplicated: %d -> %d", applied, appliedAfter)
	}
}

// TestPragmasAndForeignKeys pins the SQLite configuration the rest of the
// application relies on (WAL for concurrent reads, FKs for referential
// integrity, a busy timeout instead of instant SQLITE_BUSY errors).
func TestPragmasAndForeignKeys(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t, nil)
	ctx := context.Background()

	var mode string
	if err := store.DB().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}

	var foreignKeys int
	if err := store.DB().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("expected foreign_keys=1, got %d", foreignKeys)
	}

	var timeout int
	if err := store.DB().QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	if timeout != DefaultBusyTimeoutMS {
		t.Fatalf("expected busy_timeout=%d, got %d", DefaultBusyTimeoutMS, timeout)
	}

	// Enforcement, not just the setting: an item may not reference a type that
	// does not exist.
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO items (asset_tag, name, sn, type_id, version, archived, created_at, updated_at)
		 VALUES ('PV-999', 'Bad FK', '', 4242, 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`); err == nil {
		t.Fatal("expected a foreign-key violation for a non-existent type_id")
	}

	// ON DELETE SET NULL keeps items alive when an option disappears.
	var typeID int64
	res, err := store.DB().ExecContext(ctx,
		`INSERT INTO item_types (value, label_en, sort_order, created_at, updated_at)
		 VALUES ('Lens', 'Lens', 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`)
	if err != nil {
		t.Fatalf("insert type: %v", err)
	}
	typeID, _ = res.LastInsertId()
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO items (asset_tag, name, sn, type_id, version, archived, created_at, updated_at)
		 VALUES ('PV-002', 'Lens A', 'SN-2', ?, 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`, typeID); err != nil {
		t.Fatalf("insert item: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM item_types WHERE id = ?`, typeID); err != nil {
		t.Fatalf("delete type: %v", err)
	}
	var nullType sql.NullInt64
	if err := store.DB().QueryRowContext(ctx,
		`SELECT type_id FROM items WHERE asset_tag = 'PV-002'`).Scan(&nullType); err != nil {
		t.Fatalf("read item: %v", err)
	}
	if nullType.Valid {
		t.Fatalf("expected type_id to be NULL after the option was deleted, got %v", nullType.Int64)
	}
}

// TestWithTxCommitsAndRollsBack covers the transaction helper every writer uses.
func TestWithTxCommitsAndRollsBack(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t, nil)
	ctx := context.Background()

	if err := store.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO items (asset_tag, name, sn, version, archived, created_at, updated_at)
			 VALUES ('PV-010', 'Committed', '', 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`)
		return err
	}); err != nil {
		t.Fatalf("WithTx commit: %v", err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE asset_tag = 'PV-010'`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected the committed row, got %d", count)
	}

	sentinel := sql.ErrNoRows
	err := store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (asset_tag, name, sn, version, archived, created_at, updated_at)
			 VALUES ('PV-011', 'Rolled back', '', 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`); err != nil {
			return err
		}
		return sentinel
	})
	if err != sentinel {
		t.Fatalf("expected the callback error to be returned, got %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE asset_tag = 'PV-011'`).Scan(&count); err != nil {
		t.Fatalf("count after rollback: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the rolled-back row to be gone, got %d", count)
	}
}

// TestBackupUsesDistinctPathsWithinTheSameSecond is a regression test: backup
// names used to have second resolution, so two backups in the same second made
// VACUUM INTO fail with "output file already exists" (which broke the importer's
// --pre-snapshot on a re-run).
func TestBackupUsesDistinctPathsWithinTheSameSecond(t *testing.T) {
	t.Parallel()
	frozen := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store, dir := newTestStore(t, func() time.Time { return frozen })
	ctx := context.Background()

	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO items (asset_tag, name, sn, version, archived, created_at, updated_at)
		 VALUES ('PV-001', 'Camera A', 'SN-1', 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`); err != nil {
		t.Fatalf("insert item: %v", err)
	}

	first, err := store.BackupDB(ctx)
	if err != nil {
		t.Fatalf("first BackupDB: %v", err)
	}
	second, err := store.BackupDB(ctx)
	if err != nil {
		t.Fatalf("second BackupDB within the same second: %v", err)
	}
	if first == second {
		t.Fatalf("both backups share the path %q", first)
	}
	for _, path := range []string{first, second} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("backup %q missing: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("backup %q is empty", path)
		}
		if !strings.HasPrefix(filepath.Base(path), "sklad_") || !strings.HasSuffix(path, ".db") {
			t.Fatalf("unexpected backup name %q", path)
		}
		if !strings.HasPrefix(path, filepath.Join(dir, "backups")) {
			t.Fatalf("backup %q is outside the data directory", path)
		}
	}

	// The copy must be a usable database holding the same rows.
	backup, err := sql.Open(DriverName, "file:"+filepath.ToSlash(first)+"?mode=ro")
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer func() { _ = backup.Close() }()
	var name string
	if err := backup.QueryRowContext(ctx, `SELECT name FROM items WHERE asset_tag = 'PV-001'`).Scan(&name); err != nil {
		t.Fatalf("query backup: %v", err)
	}
	if name != "Camera A" {
		t.Fatalf("unexpected row in backup: %q", name)
	}
}

// TestPruneFilesKeepsNewest covers the retention helper used by backups and
// snapshots.
func TestPruneFilesKeepsNewest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	names := []string{"snapshot_20260101_000000.json", "snapshot_20260102_000000.json",
		"snapshot_20260103_000000.json", "snapshot_20260104_000000.json"}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	// Unrelated files must survive pruning.
	unrelated := filepath.Join(dir, "keep-me.txt")
	if err := os.WriteFile(unrelated, []byte("x"), 0o600); err != nil {
		t.Fatalf("write unrelated: %v", err)
	}

	pruneFiles(dir, "snapshot_*.json", 2)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var kept []string
	for _, entry := range entries {
		kept = append(kept, entry.Name())
	}
	if len(kept) != 3 { // two snapshots + the unrelated file
		t.Fatalf("expected 3 files to remain, got %v", kept)
	}
	for _, want := range []string{names[2], names[3], "keep-me.txt"} {
		found := false
		for _, name := range kept {
			if name == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q to survive pruning, got %v", want, kept)
		}
	}

	// A keep count of zero (or negative) is a no-op, not "delete everything".
	pruneFiles(dir, "snapshot_*.json", 0)
	entries, _ = os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("pruneFiles with keep=0 must not delete anything, got %d files", len(entries))
	}
}

// TestSnapshotDumpsDurableTables covers the human-readable snapshot used for
// disaster recovery and inspection.
func TestSnapshotDumpsDurableTables(t *testing.T) {
	t.Parallel()
	frozen := time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC)
	store, _ := newTestStore(t, func() time.Time { return frozen })
	ctx := context.Background()

	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO item_types (value, label_en, sort_order, created_at, updated_at)
		 VALUES ('Camera', 'Camera', 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`); err != nil {
		t.Fatalf("insert type: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO items (asset_tag, name, sn, version, archived, created_at, updated_at)
		 VALUES ('PV-001', 'Camera A', 'SN-1', 1, 0, '2026-03-15T10:00:00Z', '2026-03-15T10:00:00Z')`); err != nil {
		t.Fatalf("insert item: %v", err)
	}

	path, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}

	var snapshot struct {
		SchemaVersion int              `json:"schema_version"`
		CreatedAt     string           `json:"created_at"`
		ItemTypes     []map[string]any `json:"item_types"`
		Statuses      []map[string]any `json:"statuses"`
		Items         []map[string]any `json:"items"`
		Users         []map[string]any `json:"users"`
		Settings      []map[string]any `json:"settings"`
		Channels      []map[string]any `json:"notification_channels"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v (%s)", err, body)
	}
	if snapshot.SchemaVersion == 0 {
		t.Fatalf("snapshot has no schema version: %s", body)
	}
	if snapshot.CreatedAt != frozen.Format(time.RFC3339) {
		t.Fatalf("snapshot timestamp should come from the injected clock, got %q", snapshot.CreatedAt)
	}
	if len(snapshot.Items) != 1 || snapshot.Items[0]["asset_tag"] != "PV-001" {
		t.Fatalf("expected the item in the snapshot, got %+v", snapshot.Items)
	}
	if len(snapshot.ItemTypes) != 1 {
		t.Fatalf("expected the item type in the snapshot, got %+v", snapshot.ItemTypes)
	}
	// Tables without rows must serialise as null/empty, not break the export.
	if len(snapshot.Users) != 0 || len(snapshot.Settings) != 0 {
		t.Fatalf("unexpected rows in a fresh database: users=%v settings=%v", snapshot.Users, snapshot.Settings)
	}
}

// TestHealthReportsFailuresAfterClose documents that the health checker wired
// into GET /api/v1/health actually surfaces a dead database.
func TestHealthReportsFailuresAfterClose(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t, nil)
	ctx := context.Background()

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping on a fresh store: %v", err)
	}
	check := store.Health()
	if check == nil {
		t.Fatal("Health() returned nil")
	}
	if err := check(ctx); err != nil {
		t.Fatalf("Health() on a live store: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := check(ctx); err == nil {
		t.Fatal("expected Health() to fail after the store was closed")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got %v", err)
	}
}

func TestBackupRestoresPersonnelAndRejectsPartialPublication(t *testing.T) {
	st, dir := newTestStore(t, nil)
	ctx := context.Background()
	_, err := st.DB().Exec(`INSERT INTO personnel(id,name,created_at,updated_at) VALUES(9,'Office colleague','2026-01-01','2026-01-01');
 INSERT INTO items(asset_tag,name,personnel_id,created_at,updated_at) VALUES('PV-BACKUP','Camera',9,'2026-01-01','2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	path, err := st.BackupDB(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("backup not private")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := st.BackupDB(cancelled); err == nil {
		t.Fatal("cancelled backup succeeded")
	}
	pending, _ := filepath.Glob(filepath.Join(dir, "backups", "*.pending"))
	if len(pending) != 0 {
		t.Fatal("failed backup left staging files")
	}
	complete, _ := filepath.Glob(filepath.Join(dir, "backups", "*.db"))
	if len(complete) != 1 {
		t.Fatal("failed backup published")
	}
	restoredDir := t.TempDir()
	restoredPath := filepath.Join(restoredDir, "sklad.db")
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(restoredPath, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(ctx, Options{DBPath: restoredPath, DataDir: restoredDir})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var person string
	if err = restored.DB().QueryRow(`SELECT p.name FROM items i JOIN personnel p ON p.id=i.personnel_id WHERE i.asset_tag='PV-BACKUP'`).Scan(&person); err != nil || person != "Office colleague" {
		t.Fatal("renter reference not restored", err)
	}
	var integrity string
	if err = restored.DB().QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("invalid restored database", err)
	}
}
