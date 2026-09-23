-- Per-device certificate lookups (device list: earliest expiry per device) used the
-- expiry index and scanned all active certificates for every device.
CREATE INDEX IF NOT EXISTS certificates_device ON certificates(device_id, not_after) WHERE gone_at IS NULL;
