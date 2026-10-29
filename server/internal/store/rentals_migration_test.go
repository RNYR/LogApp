package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestRentalMigrationPreservesHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sklad.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	original, err := migrationsFS.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(original)); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY,applied_at TEXT NOT NULL);
 INSERT INTO schema_migrations VALUES('0001_init','2026-01-01');
 INSERT INTO users(id,username,password_hash,role,created_at,updated_at) VALUES(1,'editor','historical','editor','2026-01-01','2026-01-01');
 INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at,created_at) VALUES('old-session',1,'old-csrf','2099-01-01','2026-01-01');
 INSERT INTO items(asset_tag,name,note,created_by,created_at,updated_at) VALUES('PV-001','Existing camera','Keep history',1,'2026-01-01','2026-01-01');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(context.Background(), Options{DBPath: path, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var note string
	var status, person sql.NullInt64
	var creator int
	if err = s.DB().QueryRow(`SELECT note,status_id,personnel_id,created_by FROM items WHERE asset_tag='PV-001'`).Scan(&note, &status, &person, &creator); err != nil {
		t.Fatal(err)
	}
	if note != "Keep history" || status.Valid || person.Valid || creator != 1 {
		t.Fatalf("history changed: %q %v %v %d", note, status, person, creator)
	}
	var active, sessions, customer int
	s.DB().QueryRow(`SELECT active FROM users WHERE id=1`).Scan(&active)
	s.DB().QueryRow(`SELECT count(*) FROM sessions`).Scan(&sessions)
	s.DB().QueryRow(`SELECT count(*) FROM personnel WHERE is_customer=1 AND active=1`).Scan(&customer)
	if active != 0 || sessions != 0 || customer != 1 {
		t.Fatalf("migration state active=%d sessions=%d customer=%d", active, sessions, customer)
	}
}
