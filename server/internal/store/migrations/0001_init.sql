-- 0001_init: initial schema (frozen contract v1, docs/contracts/schema.md).
-- Applied verbatim from the canonical DDL in docs/contracts/schema.md.

PRAGMA foreign_keys = ON;

-- ---------------------------------------------------------------------------
-- Identity & access
-- ---------------------------------------------------------------------------
CREATE TABLE users (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    username              TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    display_name          TEXT    NOT NULL DEFAULT '',
    email                 TEXT    NOT NULL DEFAULT '',
    password_hash         TEXT    NOT NULL,               -- argon2id PHC string
    role                  TEXT    NOT NULL DEFAULT 'viewer'
                          CHECK (role IN ('admin','editor','viewer')),
    must_change_password  INTEGER NOT NULL DEFAULT 1,
    active                INTEGER NOT NULL DEFAULT 1,
    last_login_at         TEXT,
    created_at            TEXT    NOT NULL,
    updated_at            TEXT    NOT NULL
);

-- Server-side sessions. token_hash = hex(sha256(raw_token)); the raw token is sent
-- to the client only inside an HttpOnly+Secure+SameSite=Lax cookie.
CREATE TABLE sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_token   TEXT    NOT NULL,
    expires_at   TEXT    NOT NULL,
    created_at   TEXT    NOT NULL,
    ip           TEXT,
    user_agent   TEXT
);
CREATE INDEX idx_sessions_user    ON sessions(user_id);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

-- ---------------------------------------------------------------------------
-- Options (types & statuses) — user-configurable, localized
-- ---------------------------------------------------------------------------
CREATE TABLE item_types (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    value      TEXT    NOT NULL UNIQUE COLLATE NOCASE,   -- machine key, e.g. "Camera"
    label_en   TEXT    NOT NULL,
    label_cs   TEXT    NOT NULL DEFAULT '',
    label_it   TEXT    NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

CREATE TABLE statuses (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    value      TEXT    NOT NULL UNIQUE COLLATE NOCASE,   -- machine key, e.g. "Loaned"
    label_en   TEXT    NOT NULL,
    label_cs   TEXT    NOT NULL DEFAULT '',
    label_it   TEXT    NOT NULL DEFAULT '',
    color      TEXT    NOT NULL DEFAULT '#93c47d',       -- hex color used as badge
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

-- ---------------------------------------------------------------------------
-- Inventory
-- ---------------------------------------------------------------------------
CREATE TABLE items (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    asset_tag       TEXT    NOT NULL UNIQUE COLLATE NOCASE,  -- CODE128 payload, "PV-###"
    name            TEXT    NOT NULL DEFAULT '',
    sn              TEXT    NOT NULL DEFAULT '',
    type_id         INTEGER REFERENCES item_types(id) ON DELETE SET NULL,
    status_id       INTEGER REFERENCES statuses(id)   ON DELETE SET NULL,
    sent_date       TEXT,                                   -- YYYY-MM-DD or NULL
    return_date     TEXT,                                   -- YYYY-MM-DD or NULL
    note            TEXT    NOT NULL DEFAULT '',
    version         INTEGER NOT NULL DEFAULT 1,             -- optimistic concurrency
    archived        INTEGER NOT NULL DEFAULT 0,
    label_printed_at TEXT,
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL,
    created_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,
    updated_by      INTEGER REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX idx_items_status   ON items(status_id);
CREATE INDEX idx_items_type     ON items(type_id);
CREATE INDEX idx_items_archived ON items(archived);
CREATE INDEX idx_items_updated  ON items(updated_at);

-- ---------------------------------------------------------------------------
-- Audit
-- ---------------------------------------------------------------------------
CREATE TABLE audit_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    action        TEXT    NOT NULL,        -- create | update | delete | bulk_update | login | logout | options_update | settings_update | notify
    entity_type   TEXT    NOT NULL,        -- item | user | options | settings | notification | session
    entity_id     TEXT    NOT NULL,        -- asset_tag / username / row id, as string
    before        TEXT,                    -- JSON snapshot (nullable)
    after         TEXT,                    -- JSON snapshot (nullable)
    ip            TEXT,
    user_agent    TEXT,
    created_at    TEXT    NOT NULL
);
CREATE INDEX idx_audit_entity  ON audit_log(entity_type, entity_id);
CREATE INDEX idx_audit_created ON audit_log(created_at);

-- ---------------------------------------------------------------------------
-- Settings (JSON values, key/value)
-- ---------------------------------------------------------------------------
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT    NOT NULL,           -- JSON-encoded value
    updated_at TEXT    NOT NULL,
    updated_by INTEGER REFERENCES users(id) ON DELETE SET NULL
);

-- ---------------------------------------------------------------------------
-- Notifications (Discord webhook)
-- ---------------------------------------------------------------------------
CREATE TABLE notification_channels (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    type       TEXT    NOT NULL CHECK (type IN ('discord')),
    name       TEXT    NOT NULL DEFAULT '',
    config     TEXT    NOT NULL,           -- JSON, e.g. {"webhook_url":"https://..."}
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL,
    updated_at TEXT    NOT NULL
);

CREATE TABLE notification_deliveries (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id      INTEGER NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    trigger         TEXT    NOT NULL,      -- overdue_daily | manual | test
    status          TEXT    NOT NULL,      -- pending | sent | failed
    idempotency_key TEXT    NOT NULL UNIQUE,
    item_count      INTEGER NOT NULL DEFAULT 0,
    payload         TEXT,                  -- JSON
    error           TEXT,
    created_at      TEXT    NOT NULL,
    sent_at         TEXT
);
CREATE INDEX idx_deliveries_channel ON notification_deliveries(channel_id);
CREATE INDEX idx_deliveries_created ON notification_deliveries(created_at);

-- ---------------------------------------------------------------------------
-- Scan events (client scan telemetry for troubleshooting/audit)
-- ---------------------------------------------------------------------------
CREATE TABLE scan_events (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
    asset_tag     TEXT    NOT NULL,
    item_id       INTEGER REFERENCES items(id) ON DELETE SET NULL,
    result        TEXT    NOT NULL,        -- matched | unknown | duplicate
    created_at    TEXT    NOT NULL
);
CREATE INDEX idx_scan_created ON scan_events(created_at);
