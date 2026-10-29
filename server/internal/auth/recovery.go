package auth

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// RecoverAdmin is an offline operator operation, never exposed over HTTP.
// Stop the server first so in-flight authenticated requests cannot race recovery.
func RecoverAdmin(ctx context.Context, db *sql.DB, username, password string) error {
	if len(password) < 12 || len(password) > 1024 || strings.TrimSpace(password) == "" {
		return fmt.Errorf("temporary password must be 12–1024 bytes and not blank")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE username=? COLLATE NOCASE AND role='admin'`, username).Scan(&id); err != nil {
		return fmt.Errorf("configured Admin account not found: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=?,active=1,must_change_password=1,updated_at=? WHERE id=?`, hash, now, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_log(action,entity_type,entity_id,after,created_at) VALUES('admin_recovery','user',?, '{"offline_recovery":true,"sessions_revoked":true}',?)`, fmt.Sprint(id), now); err != nil {
		return err
	}
	return tx.Commit()
}
