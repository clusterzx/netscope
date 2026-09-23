-- How a subnet is reached: direct (attached, ARP scans), routed (through a router, no
-- layer 2) or wireguard (NetScope's own tunnel, credential of type wireguard).
ALTER TABLE subnets ADD COLUMN access TEXT NOT NULL DEFAULT 'direct';
ALTER TABLE subnets ADD COLUMN tunnel_credential_id INTEGER REFERENCES credentials(id) ON DELETE SET NULL;
