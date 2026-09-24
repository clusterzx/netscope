-- Federation of instances: a central instance collects the data of site instances.

-- central: the connected sites (the site authenticates with its own ingest-only token)
CREATE TABLE sites (
    id              INTEGER PRIMARY KEY,
    name            TEXT NOT NULL UNIQUE COLLATE NOCASE,
    slug            TEXT NOT NULL UNIQUE,          -- used by the filter language: site:colo
    token_hash      TEXT NOT NULL UNIQUE,          -- sha256(token), hex
    token_prefix    TEXT NOT NULL,
    url             TEXT NOT NULL DEFAULT '',      -- web UI of the site for deep links ('' = as reported)
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    last_contact_at INTEGER,
    last_ip         TEXT NOT NULL DEFAULT '',
    epoch           TEXT NOT NULL DEFAULT '',      -- delivery stream of the site; a new one restarts the numbering
    last_seq        INTEGER NOT NULL DEFAULT 0,    -- highest applied item of the current stream
    down            INTEGER NOT NULL DEFAULT 0,    -- site.down was raised and site.up is pending
    status          TEXT NOT NULL DEFAULT '{}'     -- last status reported by the site (JSON)
);

-- central: device id at the site -> device here
CREATE TABLE site_devices (
    site_id   INTEGER NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
    remote_id INTEGER NOT NULL,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    PRIMARY KEY (site_id, remote_id)
) WITHOUT ROWID;
CREATE INDEX site_devices_device ON site_devices(device_id);

-- NULL = this instance; addresses are matched within the same site only
ALTER TABLE devices ADD COLUMN site_id INTEGER REFERENCES sites(id) ON DELETE CASCADE;
CREATE INDEX devices_site ON devices(site_id);

-- events delivered by a site
ALTER TABLE events ADD COLUMN site_id INTEGER REFERENCES sites(id) ON DELETE CASCADE;
CREATE INDEX events_site ON events(site_id, ts DESC);

-- site: items waiting for delivery to the central instance
CREATE TABLE federation_outbox (
    seq  INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    at   INTEGER NOT NULL,
    data TEXT NOT NULL
);
