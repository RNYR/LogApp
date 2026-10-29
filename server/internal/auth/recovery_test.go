package auth

import (
	"context"
	"testing"
)

func TestRecoverAdminPreservesDataAndRevokesSessions(t *testing.T) {
	env := newAuthEnv(t, nil)
	env.seedAdmin(t)
	env.login(t, adminUser, adminPass)
	ctx := context.Background()
	if _, err := env.db.Exec(`INSERT INTO items(asset_tag,name,created_at,updated_at) VALUES('PV-RECOVERY','Keep me','2026-01-01','2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	if err := RecoverAdmin(ctx, env.db, adminUser, "short"); err == nil {
		t.Fatal("weak recovery password accepted")
	}
	if err := RecoverAdmin(ctx, env.db, "missing", "temporary-recovery-pass"); err == nil {
		t.Fatal("missing admin accepted")
	}
	if err := RecoverAdmin(ctx, env.db, adminUser, "temporary-recovery-pass"); err != nil {
		t.Fatal(err)
	}
	var sessions, items, audits int
	if err := env.db.QueryRow(`SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(`SELECT count(*) FROM items WHERE asset_tag='PV-RECOVERY'`).Scan(&items); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(`SELECT count(*) FROM audit_log WHERE action='admin_recovery'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 || items != 1 || audits != 1 {
		t.Fatalf("sessions=%d items=%d audit=%d", sessions, items, audits)
	}
	if _, err := env.service.Authenticate(ctx, adminUser, adminPass, "10.0.0.8", "test"); err == nil {
		t.Fatal("old password accepted")
	}
	result := env.login(t, adminUser, "temporary-recovery-pass")
	if !result.User.MustChangePassword {
		t.Fatal("recovered account must change password")
	}
}
