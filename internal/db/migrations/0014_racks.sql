-- Racks (FR-014): racks with height units, the devices and passive elements mounted in them,
-- per-port data (label, device plugged in) and patch cables between ports. The connections
-- of the racks become relations with source 'rack' (see internal/rack).
CREATE TABLE racks (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL,
    location   TEXT NOT NULL DEFAULT '',
    width      TEXT NOT NULL DEFAULT '19' CHECK (width IN ('19', '10')),
    height     INTEGER NOT NULL DEFAULT 42 CHECK (height BETWEEN 1 AND 60),
    numbering  TEXT NOT NULL DEFAULT 'bottom' CHECK (numbering IN ('bottom', 'top')), -- where U1 is
    notes      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- position is the lowest height unit (1 = bottom of the rack); the width is counted in sixths
-- of the rack width: col 0..5, cols 6 (full), 3 (half) or 2 (third).
CREATE TABLE rack_items (
    id          INTEGER PRIMARY KEY,
    rack_id     INTEGER NOT NULL REFERENCES racks(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('device', 'patch_panel', 'shelf', 'blank', 'cable_manager', 'pdu', 'other')),
    device_id   INTEGER UNIQUE REFERENCES devices(id) ON DELETE SET NULL,
    device_name TEXT NOT NULL DEFAULT '', -- name when mounted, shown after the device was deleted
    label       TEXT NOT NULL DEFAULT '',
    position    INTEGER NOT NULL CHECK (position >= 1),
    height      INTEGER NOT NULL DEFAULT 1 CHECK (height BETWEEN 1 AND 60),
    face        TEXT NOT NULL DEFAULT 'front' CHECK (face IN ('front', 'rear')),
    full_depth  INTEGER NOT NULL DEFAULT 0,
    col         INTEGER NOT NULL DEFAULT 0 CHECK (col BETWEEN 0 AND 5),
    cols        INTEGER NOT NULL DEFAULT 6 CHECK (cols IN (2, 3, 6)),
    port_count  INTEGER NOT NULL DEFAULT 0 CHECK (port_count BETWEEN 0 AND 128),
    port_prefix TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX rack_items_rack ON rack_items(rack_id);

-- Data of a port: a label (wall socket, purpose) and the device plugged in directly.
CREATE TABLE rack_ports (
    item_id   INTEGER NOT NULL REFERENCES rack_items(id) ON DELETE CASCADE,
    port      TEXT NOT NULL,
    label     TEXT NOT NULL DEFAULT '',
    device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
    PRIMARY KEY (item_id, port)
);
CREATE INDEX rack_ports_device ON rack_ports(device_id);

-- Patch cables between two ports (also across racks).
CREATE TABLE rack_cables (
    id         INTEGER PRIMARY KEY,
    a_item     INTEGER NOT NULL REFERENCES rack_items(id) ON DELETE CASCADE,
    a_port     TEXT NOT NULL,
    b_item     INTEGER NOT NULL REFERENCES rack_items(id) ON DELETE CASCADE,
    b_port     TEXT NOT NULL,
    label      TEXT NOT NULL DEFAULT '',
    color      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    CHECK (a_item <> b_item OR a_port <> b_port)
);
CREATE INDEX rack_cables_a ON rack_cables(a_item);
CREATE INDEX rack_cables_b ON rack_cables(b_item);
