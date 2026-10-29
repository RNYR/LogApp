package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"sklad/server/internal/auth"
	"sklad/server/internal/store"
)

func TestOfflineRecoveryCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sklad.db")
	t.Setenv("SKLAD_DATA_DIR", dir)
	t.Setenv("SKLAD_DB_PATH", path)
	t.Setenv("SKLAD_ADMIN_USERNAME", "admin")
	if err := maintenance("backup", nil); err == nil {
		t.Fatal("created missing DB")
	}
	st, err := store.Open(context.Background(), store.Options{DBPath: path, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.DB().Exec(`INSERT INTO users(username,password_hash,role,created_at,updated_at) VALUES('admin','old','admin','2026-01-01','2026-01-01')`)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	if err = maintenance("reset-admin", nil); err == nil {
		t.Fatal("stdin flag not required")
	}
	input, err := os.CreateTemp(dir, "stdin-")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	input.WriteString("temporary-recovery-password\n")
	input.Seek(0, 0)
	old := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = old }()
	if err = maintenance("reset-admin", []string{"--password-stdin"}); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(context.Background(), store.Options{DBPath: path, DataDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var hash string
	if err = st.DB().QueryRow(`SELECT password_hash FROM users WHERE username='admin'`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(hash, "temporary-recovery-password"); err != nil || !ok {
		t.Fatal("CLI did not change password")
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", "*.db"))
	if err != nil || len(backups) != 1 {
		t.Fatal("missing pre-recovery backup")
	}
	if err = maintenance("backup", nil); err != nil {
		t.Fatal(err)
	}
}
