-- Public rental workflow. Historical accounts remain for audit foreign keys,
-- but only the configured Admin may authenticate.
DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE role <> 'admin');
UPDATE users SET active = 0 WHERE role <> 'admin';
CREATE TABLE personnel (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 name TEXT NOT NULL CHECK(length(trim(name)) > 0),
 active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
 is_customer INTEGER NOT NULL DEFAULT 0 CHECK(is_customer IN (0,1)),
 version INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX personnel_customer ON personnel(is_customer) WHERE is_customer = 1;
INSERT INTO personnel(name,is_customer,created_at,updated_at) VALUES ('Customer',1,datetime('now'),datetime('now'));
ALTER TABLE items ADD COLUMN personnel_id INTEGER REFERENCES personnel(id) ON DELETE RESTRICT;
INSERT OR IGNORE INTO statuses(value,label_en,label_cs,label_it,color,created_at,updated_at)
VALUES('Feasibility','Feasibility','Feasibility','Feasibility','#93c47d',datetime('now'),datetime('now'));
-- Preserve legacy status values: migration must not imply a verified physical return.
-- Feasibility is an invariant, not a configurable replacement status.
CREATE TRIGGER protect_feasibility_delete BEFORE DELETE ON statuses WHEN lower(OLD.value)='feasibility'
BEGIN SELECT RAISE(ABORT,'Feasibility cannot be deleted'); END;
CREATE TRIGGER protect_feasibility_rename BEFORE UPDATE OF value ON statuses WHEN lower(OLD.value)='feasibility' AND NEW.value <> 'Feasibility'
BEGIN SELECT RAISE(ABORT,'Feasibility cannot be renamed'); END;
