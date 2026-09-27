-- NetScope agents on Windows: the operating system family decides which binary an agent is
-- offered and how its inventory is parsed.
ALTER TABLE agents ADD COLUMN platform TEXT NOT NULL DEFAULT 'linux'; -- linux | windows
