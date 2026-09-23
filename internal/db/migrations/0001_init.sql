-- NetScope initial schema.
-- All timestamps are unix epoch milliseconds (UTC). JSON columns hold UTF-8 JSON text.
-- Tables with first_seen/last_seen/gone_at are temporal: a row with gone_at IS NULL is
-- current state; closed rows are history (used by the diff engine and timelines).

-- ---------------------------------------------------------------- system
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,              -- JSON value
    updated_at INTEGER NOT NULL
);

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,           -- bcrypt
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    last_login_at INTEGER
);

CREATE TABLE sessions (
    id           TEXT PRIMARY KEY,         -- sha256(cookie token), hex
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    ip           TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sessions_expires ON sessions(expires_at);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
    id           INTEGER PRIMARY KEY,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    prefix       TEXT NOT NULL,            -- first characters, shown in UI
    token_hash   TEXT NOT NULL UNIQUE,     -- sha256(token), hex
    scope        TEXT NOT NULL CHECK (scope IN ('read', 'write')),
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER,
    last_used_ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);

CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY,
    ts          INTEGER NOT NULL,
    actor       TEXT NOT NULL,
    actor_type  TEXT NOT NULL CHECK (actor_type IN ('user', 'token', 'system')),
    ip          TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,             -- e.g. device.update, rule.create
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL DEFAULT '',
    summary     TEXT NOT NULL DEFAULT '',
    before      TEXT,                      -- JSON, secrets redacted
    after       TEXT                       -- JSON, secrets redacted
);
CREATE INDEX audit_log_ts ON audit_log(ts DESC);
CREATE INDEX audit_log_entity ON audit_log(entity_type, entity_id, ts DESC);

-- ---------------------------------------------------------------- vault
CREATE TABLE vault_meta (
    id          INTEGER PRIMARY KEY CHECK (id = 1),
    key_id      TEXT NOT NULL,             -- fingerprint of the active master key
    check_value BLOB NOT NULL,             -- AES-GCM(known plaintext) to verify the key
    created_at  INTEGER NOT NULL,
    rotated_at  INTEGER
);

CREATE TABLE credentials (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    type         TEXT NOT NULL,            -- ssh | password | snmp_v2c | snmp_v3 | api_token
    description  TEXT NOT NULL DEFAULT '',
    public       TEXT NOT NULL DEFAULT '{}', -- JSON, non-secret fields (username, protocols ...)
    secret       BLOB,                     -- AES-256-GCM(JSON secret fields)
    key_id       TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    last_used_at INTEGER
);

-- ---------------------------------------------------------------- network scope
CREATE TABLE subnets (
    id         INTEGER PRIMARY KEY,
    cidr       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL DEFAULT '',
    interface  TEXT NOT NULL DEFAULT '',   -- local interface for L2 scans (arp-scan -I)
    vlan       INTEGER,
    gateway    TEXT NOT NULL DEFAULT '',
    enabled    INTEGER NOT NULL DEFAULT 1,
    notes      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- ---------------------------------------------------------------- plugins
CREATE TABLE plugin_configs (
    plugin_id       TEXT PRIMARY KEY,
    enabled         INTEGER NOT NULL,
    schedule        TEXT NOT NULL DEFAULT '',  -- cron expression, '' = manual only
    timeout_s       INTEGER NOT NULL,
    retries         INTEGER NOT NULL DEFAULT 0,
    retry_backoff_s INTEGER NOT NULL DEFAULT 60,
    concurrency     INTEGER NOT NULL DEFAULT 1,
    scope           TEXT NOT NULL DEFAULT '{}',
    settings        TEXT NOT NULL DEFAULT '{}', -- JSON, non-secret schema fields
    secrets         BLOB,                       -- AES-GCM(JSON secret schema fields)
    secrets_key_id  TEXT NOT NULL DEFAULT '',
    updated_at      INTEGER NOT NULL
);

CREATE TABLE runs (
    id            INTEGER PRIMARY KEY,
    plugin_id     TEXT NOT NULL,
    trigger       TEXT NOT NULL,            -- schedule | manual | retry | device | action | startup
    status        TEXT NOT NULL,            -- queued | running | success | failed | cancelled | timeout
    attempt       INTEGER NOT NULL DEFAULT 1,
    parent_run_id INTEGER,
    scope         TEXT NOT NULL DEFAULT '{}',
    params        TEXT NOT NULL DEFAULT '{}',
    not_before    INTEGER,
    created_at    INTEGER NOT NULL,
    started_at    INTEGER,
    finished_at   INTEGER,
    duration_ms   INTEGER,
    error         TEXT NOT NULL DEFAULT '',
    stats         TEXT NOT NULL DEFAULT '{}',
    requested_by  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX runs_plugin ON runs(plugin_id, id DESC);
CREATE INDEX runs_status ON runs(status, not_before);
CREATE INDEX runs_created ON runs(created_at);

CREATE TABLE run_logs (
    id     INTEGER PRIMARY KEY,
    run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    ts     INTEGER NOT NULL,
    level  TEXT NOT NULL,
    msg    TEXT NOT NULL,
    attrs  TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX run_logs_run ON run_logs(run_id, id);
CREATE INDEX run_logs_ts ON run_logs(ts);

-- ---------------------------------------------------------------- inventory
CREATE TABLE devices (
    id                INTEGER PRIMARY KEY,
    display_name      TEXT NOT NULL DEFAULT '',   -- manual
    hostname          TEXT NOT NULL DEFAULT '',   -- effective, derived from device_facts by priority
    hostname_source   TEXT NOT NULL DEFAULT '',
    vendor            TEXT NOT NULL DEFAULT '',   -- effective
    model             TEXT NOT NULL DEFAULT '',   -- effective
    type              TEXT NOT NULL DEFAULT '',   -- effective (manual overrides guesses)
    os                TEXT NOT NULL DEFAULT '',   -- effective
    os_source         TEXT NOT NULL DEFAULT '',
    location          TEXT NOT NULL DEFAULT '',   -- manual
    owner             TEXT NOT NULL DEFAULT '',   -- manual
    notes             TEXT NOT NULL DEFAULT '',   -- manual, markdown
    criticality       TEXT NOT NULL DEFAULT 'normal'
                      CHECK (criticality IN ('low', 'normal', 'high', 'critical')),
    state             TEXT NOT NULL DEFAULT 'unknown'
                      CHECK (state IN ('unknown', 'known', 'ignored')),
    online            INTEGER NOT NULL DEFAULT 0,
    online_changed_at INTEGER,
    first_seen        INTEGER,
    last_seen         INTEGER,
    primary_ip        TEXT NOT NULL DEFAULT '',
    ip_key            BLOB NOT NULL DEFAULT x'',  -- 16-byte sortable form of primary_ip
    primary_mac       TEXT NOT NULL DEFAULT '',
    custom            TEXT NOT NULL DEFAULT '{}', -- JSON, custom field values (manual)
    created_source    TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);
CREATE INDEX devices_ip_key ON devices(ip_key);
CREATE INDEX devices_last_seen ON devices(last_seen);
CREATE INDEX devices_state ON devices(state);

CREATE TABLE device_macs (
    mac        TEXT PRIMARY KEY,               -- aa:bb:cc:dd:ee:ff
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    vendor     TEXT NOT NULL DEFAULT '',
    randomized INTEGER NOT NULL DEFAULT 0,     -- locally administered bit set
    source     TEXT NOT NULL,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL
);
CREATE INDEX device_macs_device ON device_macs(device_id);

CREATE TABLE device_ips (
    id          INTEGER PRIMARY KEY,
    device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip          TEXT NOT NULL,
    ip_key      BLOB NOT NULL,
    mac         TEXT NOT NULL DEFAULT '',      -- MAC the IP was last seen with
    subnet_id   INTEGER REFERENCES subnets(id) ON DELETE SET NULL,
    source      TEXT NOT NULL,
    first_seen  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    last_run_id INTEGER,
    gone_at     INTEGER
);
CREATE UNIQUE INDEX device_ips_active ON device_ips(device_id, ip) WHERE gone_at IS NULL;
CREATE INDEX device_ips_ip ON device_ips(ip, gone_at);
CREATE INDEX device_ips_device ON device_ips(device_id, gone_at);

-- Scalar facts per source (hostname, vendor, model, os, type, attr:*). Manual overrides use
-- source 'manual'. The effective value on devices is chosen by source priority.
CREATE TABLE device_facts (
    id         INTEGER PRIMARY KEY,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    source     TEXT NOT NULL,
    value      TEXT NOT NULL,
    extra      TEXT NOT NULL DEFAULT '{}',
    run_id     INTEGER,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    gone_at    INTEGER
);
CREATE UNIQUE INDEX device_facts_active ON device_facts(device_id, kind, source) WHERE gone_at IS NULL;
CREATE INDEX device_facts_device ON device_facts(device_id, kind, gone_at);

-- Which presence-capable plugin has seen a device, and how many of its runs missed it.
CREATE TABLE device_presence (
    device_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    plugin_id   TEXT NOT NULL,
    last_seen   INTEGER NOT NULL,
    last_run_id INTEGER,
    missed      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (device_id, plugin_id)
) WITHOUT ROWID;

-- Links between devices and foreign systems (proxmox vmid, netalertx mac, docker host ...).
CREATE TABLE external_refs (
    source     TEXT NOT NULL,
    ref        TEXT NOT NULL,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    data       TEXT NOT NULL DEFAULT '{}',
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    PRIMARY KEY (source, ref)
) WITHOUT ROWID;
CREATE INDEX external_refs_device ON external_refs(device_id);

-- Latest structured inventory per device and source (ssh host facts, snmp tables, upnp ...).
CREATE TABLE device_inventory (
    device_id    INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    source       TEXT NOT NULL,
    data         TEXT NOT NULL,
    run_id       INTEGER,
    collected_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, source)
) WITHOUT ROWID;

CREATE TABLE device_tags (
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    tag       TEXT NOT NULL,
    PRIMARY KEY (device_id, tag)
) WITHOUT ROWID;
CREATE INDEX device_tags_tag ON device_tags(tag);

CREATE TABLE groups (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    kind        TEXT NOT NULL CHECK (kind IN ('manual', 'query')),
    query       TEXT NOT NULL DEFAULT '',     -- filter query language, for kind = query
    color       TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE group_members (
    group_id  INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, device_id)
) WITHOUT ROWID;
CREATE INDEX group_members_device ON group_members(device_id);

CREATE TABLE custom_fields (
    id          INTEGER PRIMARY KEY,
    key         TEXT NOT NULL UNIQUE,
    label       TEXT NOT NULL,
    type        TEXT NOT NULL CHECK (type IN ('text', 'number', 'date', 'url', 'bool')),
    description TEXT NOT NULL DEFAULT '',
    sort_order  INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE saved_views (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    query      TEXT NOT NULL DEFAULT '',
    columns    TEXT NOT NULL DEFAULT '[]',
    sort       TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Parent/child and topology edges. parent_id is the upstream side (hypervisor, switch, host).
CREATE TABLE relations (
    id          INTEGER PRIMARY KEY,
    parent_id   INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    child_id    INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,     -- runs_on | switch_port | lldp | l3 | wireless | manual
    source      TEXT NOT NULL,     -- manual | proxmox | topology | docker ...
    parent_port TEXT NOT NULL DEFAULT '',
    child_port  TEXT NOT NULL DEFAULT '',
    label       TEXT NOT NULL DEFAULT '',
    protected   INTEGER NOT NULL DEFAULT 0,
    data        TEXT NOT NULL DEFAULT '{}',
    first_seen  INTEGER NOT NULL,
    last_seen   INTEGER NOT NULL,
    UNIQUE (parent_id, child_id, kind, source),
    CHECK (parent_id <> child_id)
);
CREATE INDEX relations_child ON relations(child_id);

-- ---------------------------------------------------------------- observations and state
CREATE TABLE observations (
    id        INTEGER PRIMARY KEY,
    run_id    INTEGER,
    plugin_id TEXT NOT NULL,
    device_id INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    target    TEXT NOT NULL DEFAULT '',
    ts        INTEGER NOT NULL,
    data      TEXT NOT NULL,       -- normalized observation JSON
    raw       TEXT                 -- plugin raw output fragment
);
CREATE INDEX observations_device ON observations(device_id, plugin_id, id DESC);
CREATE INDEX observations_run ON observations(run_id);
CREATE INDEX observations_ts ON observations(ts);

CREATE TABLE ports (
    id         INTEGER PRIMARY KEY,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip         TEXT NOT NULL,
    proto      TEXT NOT NULL CHECK (proto IN ('tcp', 'udp')),
    port       INTEGER NOT NULL,
    state      TEXT NOT NULL,
    service    TEXT NOT NULL DEFAULT '',
    product    TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    extra_info TEXT NOT NULL DEFAULT '',
    tunnel     TEXT NOT NULL DEFAULT '',
    cpes       TEXT NOT NULL DEFAULT '[]',
    source     TEXT NOT NULL,
    run_id     INTEGER,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    gone_at    INTEGER
);
CREATE UNIQUE INDEX ports_active ON ports(device_id, ip, proto, port) WHERE gone_at IS NULL;
CREATE INDEX ports_device ON ports(device_id, gone_at);
CREATE INDEX ports_port ON ports(port, proto, gone_at);

CREATE TABLE certificates (
    id             INTEGER PRIMARY KEY,
    device_id      INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip             TEXT NOT NULL,
    port           INTEGER NOT NULL,
    server_name    TEXT NOT NULL DEFAULT '',
    fingerprint    TEXT NOT NULL,           -- sha256 of leaf DER, hex
    subject_cn     TEXT NOT NULL DEFAULT '',
    sans           TEXT NOT NULL DEFAULT '[]',
    issuer         TEXT NOT NULL DEFAULT '',
    issuer_cn      TEXT NOT NULL DEFAULT '',
    serial         TEXT NOT NULL DEFAULT '',
    not_before     INTEGER NOT NULL,
    not_after      INTEGER NOT NULL,
    self_signed    INTEGER NOT NULL DEFAULT 0,
    chain_valid    INTEGER NOT NULL DEFAULT 0,
    chain_error    TEXT NOT NULL DEFAULT '',
    key_type       TEXT NOT NULL DEFAULT '',
    key_bits       INTEGER NOT NULL DEFAULT 0,
    signature_alg  TEXT NOT NULL DEFAULT '',
    tls_versions   TEXT NOT NULL DEFAULT '[]',
    cipher         TEXT NOT NULL DEFAULT '',
    weak_protocols TEXT NOT NULL DEFAULT '[]',
    weak_ciphers   TEXT NOT NULL DEFAULT '[]',
    source         TEXT NOT NULL,
    run_id         INTEGER,
    first_seen     INTEGER NOT NULL,
    last_seen      INTEGER NOT NULL,
    gone_at        INTEGER
);
CREATE UNIQUE INDEX certificates_active ON certificates(device_id, ip, port) WHERE gone_at IS NULL;
CREATE INDEX certificates_expiry ON certificates(gone_at, not_after);

CREATE TABLE http_services (
    id           INTEGER PRIMARY KEY,
    device_id    INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    ip           TEXT NOT NULL,
    port         INTEGER NOT NULL,
    scheme       TEXT NOT NULL,
    url          TEXT NOT NULL,
    status_code  INTEGER NOT NULL DEFAULT 0,
    title        TEXT NOT NULL DEFAULT '',
    server       TEXT NOT NULL DEFAULT '',
    content_type TEXT NOT NULL DEFAULT '',
    redirects    TEXT NOT NULL DEFAULT '[]',
    final_url    TEXT NOT NULL DEFAULT '',
    favicon_hash INTEGER,                 -- mmh3 of base64 favicon (Shodan compatible)
    favicon_md5  TEXT NOT NULL DEFAULT '',
    apps         TEXT NOT NULL DEFAULT '[]',
    headers      TEXT NOT NULL DEFAULT '{}',
    source       TEXT NOT NULL,
    run_id       INTEGER,
    first_seen   INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    gone_at      INTEGER
);
CREATE UNIQUE INDEX http_services_active ON http_services(device_id, ip, port) WHERE gone_at IS NULL;

CREATE TABLE packages (
    id         INTEGER PRIMARY KEY,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    manager    TEXT NOT NULL,             -- dpkg | rpm | apk
    name       TEXT NOT NULL,
    version    TEXT NOT NULL,
    arch       TEXT NOT NULL DEFAULT '',
    source     TEXT NOT NULL,
    run_id     INTEGER,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    gone_at    INTEGER
);
CREATE UNIQUE INDEX packages_active ON packages(device_id, manager, name, arch) WHERE gone_at IS NULL;
CREATE INDEX packages_device ON packages(device_id, gone_at);
CREATE INDEX packages_name ON packages(name);

CREATE TABLE containers (
    id              INTEGER PRIMARY KEY,
    device_id       INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE, -- host
    engine          TEXT NOT NULL DEFAULT 'docker',
    container_id    TEXT NOT NULL,
    name            TEXT NOT NULL,
    image           TEXT NOT NULL,
    image_id        TEXT NOT NULL DEFAULT '',
    state           TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT '',
    ports           TEXT NOT NULL DEFAULT '[]',
    networks        TEXT NOT NULL DEFAULT '[]',
    compose_project TEXT NOT NULL DEFAULT '',
    compose_service TEXT NOT NULL DEFAULT '',
    labels          TEXT NOT NULL DEFAULT '{}',
    created         INTEGER,
    source          TEXT NOT NULL,
    run_id          INTEGER,
    first_seen      INTEGER NOT NULL,
    last_seen       INTEGER NOT NULL,
    gone_at         INTEGER
);
CREATE UNIQUE INDEX containers_active ON containers(device_id, engine, name) WHERE gone_at IS NULL;
CREATE INDEX containers_device ON containers(device_id, gone_at);

CREATE TABLE container_images (
    id         INTEGER PRIMARY KEY,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    engine     TEXT NOT NULL DEFAULT 'docker',
    image_id   TEXT NOT NULL,
    tags       TEXT NOT NULL DEFAULT '[]',
    size       INTEGER NOT NULL DEFAULT 0,
    created    INTEGER,
    source     TEXT NOT NULL,
    run_id     INTEGER,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    gone_at    INTEGER
);
CREATE UNIQUE INDEX container_images_active ON container_images(device_id, engine, image_id) WHERE gone_at IS NULL;

-- ---------------------------------------------------------------- events and rules
CREATE TABLE events (
    id        INTEGER PRIMARY KEY,
    ts        INTEGER NOT NULL,
    type      TEXT NOT NULL,
    category  TEXT NOT NULL,
    severity  TEXT NOT NULL CHECK (severity IN ('info', 'low', 'medium', 'high', 'critical')),
    device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    plugin_id TEXT NOT NULL DEFAULT '',
    run_id    INTEGER,
    title     TEXT NOT NULL,
    message   TEXT NOT NULL DEFAULT '',
    payload   TEXT NOT NULL DEFAULT '{}',
    dedup_key TEXT NOT NULL DEFAULT '',
    acked_at  INTEGER,
    acked_by  TEXT NOT NULL DEFAULT '',
    ack_note  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX events_ts ON events(ts DESC);
CREATE INDEX events_device ON events(device_id, ts DESC);
CREATE INDEX events_type ON events(type, ts DESC);
CREATE INDEX events_open ON events(acked_at, severity, ts DESC);
CREATE INDEX events_dedup ON events(dedup_key, ts DESC) WHERE dedup_key <> '';

CREATE TABLE rules (
    id          INTEGER PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled     INTEGER NOT NULL DEFAULT 1,
    sort_order  INTEGER NOT NULL DEFAULT 0,
    stop        INTEGER NOT NULL DEFAULT 0,  -- stop evaluating later rules after a match
    conditions  TEXT NOT NULL,               -- JSON
    actions     TEXT NOT NULL,               -- JSON array
    builtin     TEXT NOT NULL DEFAULT '',    -- key of a seeded default rule
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);

CREATE TABLE notifications (
    id            INTEGER PRIMARY KEY,
    rule_id       INTEGER REFERENCES rules(id) ON DELETE SET NULL,
    action_index  INTEGER NOT NULL DEFAULT 0,
    publisher_id  TEXT NOT NULL,
    kind          TEXT NOT NULL,             -- event | escalation | test | report
    priority      TEXT NOT NULL,
    status        TEXT NOT NULL,             -- pending | sending | sent | failed | skipped
    group_key     TEXT NOT NULL DEFAULT '',
    event_ids     TEXT NOT NULL DEFAULT '[]',
    title         TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    deliver_after INTEGER NOT NULL,
    created_at    INTEGER NOT NULL,
    sent_at       INTEGER,
    attempts      INTEGER NOT NULL DEFAULT 0,
    error         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX notifications_pending ON notifications(status, deliver_after);
CREATE INDEX notifications_group ON notifications(group_key, status);
CREATE INDEX notifications_created ON notifications(created_at DESC);

CREATE TABLE rule_throttle (
    rule_id INTEGER NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    key     TEXT NOT NULL,
    last_at INTEGER NOT NULL,
    PRIMARY KEY (rule_id, key)
) WITHOUT ROWID;

CREATE TABLE escalations (
    id           INTEGER PRIMARY KEY,
    event_id     INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    rule_id      INTEGER NOT NULL REFERENCES rules(id) ON DELETE CASCADE,
    action_index INTEGER NOT NULL,
    due_at       INTEGER NOT NULL,
    done_at      INTEGER,
    UNIQUE (event_id, rule_id, action_index)
);
CREATE INDEX escalations_due ON escalations(done_at, due_at);

-- ---------------------------------------------------------------- health checks
CREATE TABLE health_checks (
    id                INTEGER PRIMARY KEY,
    device_id         INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    type              TEXT NOT NULL CHECK (type IN ('tcp', 'http', 'tls', 'icmp')),
    target            TEXT NOT NULL DEFAULT '',  -- host/IP; empty = device primary IP
    port              INTEGER NOT NULL DEFAULT 0,
    config            TEXT NOT NULL DEFAULT '{}',
    interval_s        INTEGER NOT NULL DEFAULT 60,
    timeout_s         INTEGER NOT NULL DEFAULT 10,
    fail_threshold    INTEGER NOT NULL DEFAULT 3,
    recover_threshold INTEGER NOT NULL DEFAULT 2,
    enabled           INTEGER NOT NULL DEFAULT 1,
    state             TEXT NOT NULL DEFAULT 'unknown'
                      CHECK (state IN ('unknown', 'up', 'down', 'degraded')),
    state_since       INTEGER,
    consecutive_fail  INTEGER NOT NULL DEFAULT 0,
    consecutive_ok    INTEGER NOT NULL DEFAULT 0,
    last_check_at     INTEGER,
    last_ok           INTEGER,
    last_latency_ms   REAL,
    last_error        TEXT NOT NULL DEFAULT '',
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
);
CREATE INDEX health_checks_device ON health_checks(device_id);

CREATE TABLE health_outages (
    id         INTEGER PRIMARY KEY,
    check_id   INTEGER NOT NULL REFERENCES health_checks(id) ON DELETE CASCADE,
    state      TEXT NOT NULL CHECK (state IN ('down', 'degraded')),
    started_at INTEGER NOT NULL,
    ended_at   INTEGER,
    reason     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX health_outages_check ON health_outages(check_id, started_at DESC);

-- ---------------------------------------------------------------- time series
CREATE TABLE ts_series (
    id         INTEGER PRIMARY KEY,
    metric     TEXT NOT NULL,              -- e.g. icmp.rtt_ms, icmp.loss_pct, health.latency_ms
    device_id  INTEGER REFERENCES devices(id) ON DELETE CASCADE,
    key        TEXT NOT NULL DEFAULT '',
    unit       TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX ts_series_uniq ON ts_series(metric, IFNULL(device_id, 0), key);
CREATE INDEX ts_series_device ON ts_series(device_id);

CREATE TABLE ts_raw (
    series_id INTEGER NOT NULL REFERENCES ts_series(id) ON DELETE CASCADE,
    ts        INTEGER NOT NULL,
    min       REAL NOT NULL,
    avg       REAL NOT NULL,
    max       REAL NOT NULL,
    PRIMARY KEY (series_id, ts)
) WITHOUT ROWID;
CREATE INDEX ts_raw_ts ON ts_raw(ts);

CREATE TABLE ts_5m (
    series_id INTEGER NOT NULL REFERENCES ts_series(id) ON DELETE CASCADE,
    bucket    INTEGER NOT NULL,
    min       REAL NOT NULL,
    avg       REAL NOT NULL,
    max       REAL NOT NULL,
    count     INTEGER NOT NULL,
    PRIMARY KEY (series_id, bucket)
) WITHOUT ROWID;
CREATE INDEX ts_5m_bucket ON ts_5m(bucket);

CREATE TABLE ts_1h (
    series_id INTEGER NOT NULL REFERENCES ts_series(id) ON DELETE CASCADE,
    bucket    INTEGER NOT NULL,
    min       REAL NOT NULL,
    avg       REAL NOT NULL,
    max       REAL NOT NULL,
    count     INTEGER NOT NULL,
    PRIMARY KEY (series_id, bucket)
) WITHOUT ROWID;
CREATE INDEX ts_1h_bucket ON ts_1h(bucket);

-- ---------------------------------------------------------------- vulnerabilities
CREATE TABLE nvd_cves (
    id            TEXT PRIMARY KEY,          -- CVE-2024-12345
    published     INTEGER,
    last_modified INTEGER,
    status        TEXT NOT NULL DEFAULT '',
    cvss_score    REAL,
    cvss_vector   TEXT NOT NULL DEFAULT '',
    cvss_version  TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    refs          TEXT NOT NULL DEFAULT '[]',
    cwes          TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX nvd_cves_score ON nvd_cves(cvss_score);

CREATE TABLE nvd_cpe_matches (
    cve_id     TEXT NOT NULL,
    part       TEXT NOT NULL,                -- a | o | h
    vendor     TEXT NOT NULL,
    product    TEXT NOT NULL,
    version    TEXT NOT NULL,                -- '*' | '-' | exact version
    upd        TEXT NOT NULL DEFAULT '*',
    start_incl TEXT NOT NULL DEFAULT '',
    start_excl TEXT NOT NULL DEFAULT '',
    end_incl   TEXT NOT NULL DEFAULT '',
    end_excl   TEXT NOT NULL DEFAULT '',
    vulnerable INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX nvd_cpe_matches_vp ON nvd_cpe_matches(vendor, product);
CREATE INDEX nvd_cpe_matches_product ON nvd_cpe_matches(product);
CREATE INDEX nvd_cpe_matches_cve ON nvd_cpe_matches(cve_id);

CREATE TABLE nvd_feeds (
    name          TEXT PRIMARY KEY,          -- 2024, modified ...
    last_modified TEXT NOT NULL DEFAULT '',
    sha256        TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    cve_count     INTEGER NOT NULL DEFAULT 0,
    synced_at     INTEGER,
    status        TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT ''
);

CREATE TABLE device_cves (
    id         INTEGER PRIMARY KEY,
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    cve_id     TEXT NOT NULL,
    cpe        TEXT NOT NULL,                -- device-side CPE that matched
    source     TEXT NOT NULL,                -- where the CPE came from: nmap | ssh | http ...
    product    TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',
    match_type TEXT NOT NULL,                -- exact | range | heuristic
    cvss_score REAL,
    first_seen INTEGER NOT NULL,
    last_seen  INTEGER NOT NULL,
    gone_at    INTEGER
);
CREATE UNIQUE INDEX device_cves_active ON device_cves(device_id, cve_id, cpe) WHERE gone_at IS NULL;
CREATE INDEX device_cves_cve ON device_cves(cve_id);
CREATE INDEX device_cves_device ON device_cves(device_id, gone_at);

CREATE TABLE cve_ignores (
    device_id  INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    cve_id     TEXT NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_by TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, cve_id)
) WITHOUT ROWID;
