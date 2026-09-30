-- Setup wizard (FR-013): a new installation is set up in the web interface before any
-- plugin runs. Installations that already have users count as set up.
INSERT INTO settings(key, value, updated_at)
SELECT 'setup', '{"completed":true,"mode":"migration"}', CAST(strftime('%s', 'now') AS INTEGER) * 1000
WHERE EXISTS (SELECT 1 FROM users)
ON CONFLICT(key) DO NOTHING;
