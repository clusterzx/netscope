-- Several users with freely definable roles (permission sets), a second factor (TOTP,
-- passkeys, recovery codes) that a role can require, disabled accounts and a forced
-- password change for accounts an administrator created.

CREATE TABLE roles (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE COLLATE NOCASE,
    description TEXT NOT NULL DEFAULT '',
    permissions TEXT NOT NULL DEFAULT '[]',   -- JSON list of permission keys
    require_2fa INTEGER NOT NULL DEFAULT 0,
    builtin     TEXT NOT NULL DEFAULT '',     -- 'admin' = every permission, not editable
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

INSERT INTO roles(id, name, description, permissions, builtin, created_at, updated_at) VALUES
    (1, 'Administrator', 'Alle Rechte – auch Benutzer, Rollen, System, Credentials und Backups', '[]', 'admin',
        CAST(strftime('%s', 'now') AS INTEGER) * 1000, CAST(strftime('%s', 'now') AS INTEGER) * 1000),
    (2, 'Bearbeiter', 'Pflegt das Inventar, startet Scans, quittiert Events und verwaltet Health-Checks und Regeln',
        '["devices.edit","devices.delete","devices.scan","devices.actions","inventory.config","events.ack","health.manage","vulns.manage","rules.manage","reports.send","credentials.view","tokens.create"]', '',
        CAST(strftime('%s', 'now') AS INTEGER) * 1000, CAST(strftime('%s', 'now') AS INTEGER) * 1000),
    (3, 'Betrachter', 'Sieht Inventar, Topologie, Events, Health und Schwachstellen, ändert nichts', '[]', '',
        CAST(strftime('%s', 'now') AS INTEGER) * 1000, CAST(strftime('%s', 'now') AS INTEGER) * 1000);

ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
-- NULL only in between: SQLite does not allow a non-NULL default on a new REFERENCES column
ALTER TABLE users ADD COLUMN role_id INTEGER REFERENCES roles(id);
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_secret BLOB;                -- vault-encrypted, active second factor
ALTER TABLE users ADD COLUMN totp_pending BLOB;               -- vault-encrypted, until the first code confirms it
ALTER TABLE users ADD COLUMN totp_enabled_at INTEGER;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0; -- a code is accepted once
ALTER TABLE users ADD COLUMN webauthn_id BLOB;                -- random user handle for passkeys

-- the existing (admin) account keeps every right
UPDATE users SET role_id = 1;

CREATE TABLE user_recovery_codes (
    id        INTEGER PRIMARY KEY,
    user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL,                  -- sha256 of the normalised code, hex
    used_at   INTEGER
);
CREATE INDEX user_recovery_codes_user ON user_recovery_codes(user_id);

CREATE TABLE user_passkeys (
    id            INTEGER PRIMARY KEY,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    credential_id BLOB NOT NULL UNIQUE,
    rp_id         TEXT NOT NULL,              -- host name the passkey is bound to
    data          TEXT NOT NULL,              -- JSON webauthn.Credential (public key, flags, counter)
    created_at    INTEGER NOT NULL,
    last_used_at  INTEGER
);
CREATE INDEX user_passkeys_user ON user_passkeys(user_id);
