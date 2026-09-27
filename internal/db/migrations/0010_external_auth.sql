-- Users from a directory (LDAP) or an identity provider (OIDC). They are created at their
-- first sign-in and have no local password; external_id identifies them at the source
-- (LDAP: DN, OIDC: issuer|subject).

ALTER TABLE users ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local'; -- local | ldap | oidc
ALTER TABLE users ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX users_external ON users(auth_source, external_id) WHERE external_id <> '';
