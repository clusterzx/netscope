-- NetScope agents: installation tokens (install command) and the agents that enrolled with
-- them. Agents connect out to the instance; only the hashes of their secrets are stored.

CREATE TABLE agent_enrollments (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL,              -- label, e.g. "Colo-VMs"
    token_hash   TEXT NOT NULL UNIQUE,       -- sha256(token), hex
    token_prefix TEXT NOT NULL,
    tags         TEXT NOT NULL DEFAULT '[]', -- JSON list, set on devices enrolled with the token
    max_uses     INTEGER,                    -- NULL = unlimited
    uses         INTEGER NOT NULL DEFAULT 0,
    expires_at   INTEGER,                    -- NULL = never
    revoked_at   INTEGER,
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   INTEGER NOT NULL
);

CREATE TABLE agents (
    id                INTEGER PRIMARY KEY AUTOINCREMENT, -- never reused: "agent-<id>" identifies hosts without machine id
    device_id         INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    enrollment_id     INTEGER REFERENCES agent_enrollments(id) ON DELETE SET NULL,
    machine_id        TEXT NOT NULL DEFAULT '',
    hostname          TEXT NOT NULL DEFAULT '',
    os                TEXT NOT NULL DEFAULT '',
    arch              TEXT NOT NULL DEFAULT '',
    kernel            TEXT NOT NULL DEFAULT '',
    version           TEXT NOT NULL DEFAULT '',
    docker            INTEGER NOT NULL DEFAULT 0,
    token_hash        TEXT NOT NULL UNIQUE,
    ip                TEXT NOT NULL DEFAULT '',  -- address the agent last connected from
    enrolled_at       INTEGER NOT NULL,
    last_seen_at      INTEGER,
    last_inventory_at INTEGER,
    last_metrics_at   INTEGER,
    last_error        TEXT NOT NULL DEFAULT '',
    offline           INTEGER NOT NULL DEFAULT 0, -- agent.offline raised
    refresh           INTEGER NOT NULL DEFAULT 0, -- inventory requested from the UI
    full_disks        TEXT NOT NULL DEFAULT '[]'  -- mounts above the threshold (disk.full raised)
);
CREATE INDEX agents_device ON agents(device_id);
CREATE INDEX agents_machine ON agents(machine_id);
