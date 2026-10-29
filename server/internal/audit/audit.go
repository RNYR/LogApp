// Package audit records tamper-evident, human-attributable audit events.
//
// Every mutation in Sklad must leave an audit_log row (see
// docs/contracts/schema.md) so that administrators can answer "who changed what,
// when, and what did it look like before". The Writer is deliberately small and
// dependency-free: it operates on a *sql.DB directly and can join an existing
// transaction so an event commits or rolls back atomically with the change it
// describes.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// Action names (schema.md: audit_log.action).
const (
	ActionLogin          = "login"
	ActionLogout         = "logout"
	ActionCreate         = "create"
	ActionUpdate         = "update"
	ActionDelete         = "delete"
	ActionBulkUpdate     = "bulk_update"
	ActionOptionsUpdate  = "options_update"
	ActionSettingsUpdate = "settings_update"
	ActionNotify         = "notify"
)

// Entity type names (schema.md: audit_log.entity_type).
const (
	EntityItem         = "item"
	EntityUser         = "user"
	EntityOptions      = "options"
	EntitySettings     = "settings"
	EntityNotification = "notification"
	EntitySession      = "session"
)

// Record is one audit event. Before and After are arbitrary values serialised
// to JSON; a nil value is stored as SQL NULL.
type Record struct {
	Action      string
	EntityType  string
	EntityID    string
	ActorUserID *int64 // nil means system or unauthenticated
	Before      any
	After       any
	IP          string
	UserAgent   string
}

// Writer appends rows to the audit_log table.
type Writer struct {
	db  *sql.DB
	now func() time.Time
}

// New returns a Writer backed by db. The zero value is not usable; always call
// New. (A nil db is tolerated until the first Record, which fails loudly.)
func New(db *sql.DB) *Writer {
	return &Writer{db: db, now: time.Now}
}

// Record inserts a standalone audit row (no surrounding transaction).
func (w *Writer) Record(ctx context.Context, r Record) error {
	return w.record(ctx, nil, r)
}

// RecordTx inserts an audit row. When tx is non-nil the insert joins the
// caller's transaction; pass the transaction from store.WithTx so the audit
// entry and the change it describes are atomic.
func (w *Writer) RecordTx(ctx context.Context, tx *sql.Tx, r Record) error {
	return w.record(ctx, tx, r)
}

func (w *Writer) record(ctx context.Context, tx *sql.Tx, r Record) error {
	if w == nil || w.db == nil {
		return fmt.Errorf("audit: writer is not initialised (call audit.New)")
	}

	before, err := marshalNullable(r.Before)
	if err != nil {
		return fmt.Errorf("audit: marshal before: %w", err)
	}
	after, err := marshalNullable(r.After)
	if err != nil {
		return fmt.Errorf("audit: marshal after: %w", err)
	}

	var actor any
	if r.ActorUserID != nil {
		actor = *r.ActorUserID
	}

	now := time.Now()
	if w.now != nil {
		now = w.now()
	}
	createdAt := now.UTC().Format(time.RFC3339)

	query := `INSERT INTO audit_log
		(actor_user_id, action, entity_type, entity_id, before, after, ip, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{actor, r.Action, r.EntityType, r.EntityID, before, after, r.IP, r.UserAgent, createdAt}

	var err2 error
	if tx != nil {
		_, err2 = tx.ExecContext(ctx, query, args...)
	} else {
		_, err2 = w.db.ExecContext(ctx, query, args...)
	}
	if err2 != nil {
		return fmt.Errorf("audit: insert: %w", err2)
	}
	return nil
}

// marshalNullable encodes v to a JSON string, or returns nil for a nil v (the
// nil any is passed straight to the driver as SQL NULL).
func marshalNullable(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
