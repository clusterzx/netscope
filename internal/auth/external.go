package auth

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

// Sign-in through a directory (LDAP) or an identity provider (OIDC). Their users are
// created at the first successful sign-in; they have no local password, and their role
// comes from their groups (ordered group → role mapping, else a default role, else no
// access). A local user of the same name always wins: an external sign-in never takes over
// an existing local account.

// Sources of a user account.
const (
	SourceLocal = "local"
	SourceLDAP  = "ldap"
	SourceOIDC  = "oidc"
)

const (
	keyOIDC       = "auth.oidc"
	keyOIDCSecret = "auth.oidc.secret" // client secret; re-encrypted by the vault key rotation
	keyLDAP       = "auth.ldap"
	keyLDAPSecret = "auth.ldap.secret" // bind password; re-encrypted by the vault key rotation
)

// ErrNoRole reports an external user whose groups grant no role.
var ErrNoRole = errors.New("Kein Zugriff: Das Konto gehört keiner Gruppe an, der in NetScope eine Rolle zugeordnet ist")

// RoleMapping maps a group of the directory or provider to a NetScope role.
type RoleMapping struct {
	// Group: OIDC group claim value; LDAP group DN or its first RDN value (CN).
	Group  string `json:"group"`
	RoleID int64  `json:"roleId"`
}

// Provisioning decides the role of external users.
type Provisioning struct {
	// Mappings are checked in order; the first group the user belongs to wins.
	Mappings []RoleMapping `json:"mappings"`
	// DefaultRoleID is the role of users without a matching group (0 = no access).
	DefaultRoleID int64 `json:"defaultRoleId"`
	// SyncRole sets the role from the groups at every sign-in, not only at the first one.
	SyncRole bool `json:"syncRole"`
}

// TLSOptions trust a private CA or, as a last resort, skip certificate checks.
type TLSOptions struct {
	CA                 string `json:"ca,omitempty"` // PEM
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
}

func (o TLSOptions) config(serverName string) (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, InsecureSkipVerify: o.InsecureSkipVerify} //nolint:gosec // opt-in by the administrator
	if strings.TrimSpace(o.CA) != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(o.CA)) {
			return nil, errors.New("CA-Zertifikat ist kein gültiges PEM")
		}
		c.RootCAs = pool
	}
	return c, nil
}

// OIDCConfig configures sign-in with an OpenID Connect provider.
type OIDCConfig struct {
	Enabled bool `json:"enabled"`
	// Name labels the button on the login page ("Anmelden mit <Name>").
	Name     string `json:"name"`
	Issuer   string `json:"issuer"`
	ClientID string `json:"clientId"`
	// Scopes requested besides openid (default profile, email, groups).
	Scopes []string `json:"scopes"`
	// UsernameClaim (default preferred_username, falling back to email and sub) and
	// GroupsClaim (default groups; a dot path like realm_access.roles is allowed).
	UsernameClaim string `json:"usernameClaim"`
	GroupsClaim   string `json:"groupsClaim"`
	// RedirectURL overrides the callback address derived from the request.
	RedirectURL string `json:"redirectUrl,omitempty"`
	Provisioning
	TLSOptions
	// HasSecret reports a stored client secret (output only).
	HasSecret bool `json:"hasSecret"`
}

// LDAPConfig configures sign-in against a directory (Active Directory, OpenLDAP, …).
type LDAPConfig struct {
	Enabled bool `json:"enabled"`
	// URL: ldap://host[:389] or ldaps://host[:636]; StartTLS upgrades ldap://.
	URL      string `json:"url"`
	StartTLS bool   `json:"startTls"`
	// BindDN of the service account that searches users ("" = anonymous search).
	BindDN string `json:"bindDn"`
	BaseDN string `json:"baseDn"`
	// UserFilter finds the account; {username} is replaced by the escaped login name.
	UserFilter      string `json:"userFilter"`
	UsernameAttr    string `json:"usernameAttr"`
	DisplayNameAttr string `json:"displayNameAttr"`
	EmailAttr       string `json:"emailAttr"`
	// GroupAttr lists the groups at the user (memberOf); without it groups are searched
	// below GroupBaseDN with GroupFilter ({dn} and {username} are replaced).
	GroupAttr   string `json:"groupAttr"`
	GroupBaseDN string `json:"groupBaseDn"`
	GroupFilter string `json:"groupFilter"`
	Provisioning
	TLSOptions
	// HasPassword reports a stored bind password (output only).
	HasPassword bool `json:"hasPassword"`
}

// ExternalAuth is the configuration of both methods.
type ExternalAuth struct {
	OIDC OIDCConfig `json:"oidc"`
	LDAP LDAPConfig `json:"ldap"`
}

// Providers is what the login page needs to know.
type Providers struct {
	OIDC     bool   `json:"oidc"`
	OIDCName string `json:"oidcName,omitempty"`
	LDAP     bool   `json:"ldap"`
}

func defaultOIDC() OIDCConfig {
	return OIDCConfig{Name: "Single Sign-on", Scopes: []string{"profile", "email", "groups"}, UsernameClaim: "preferred_username",
		GroupsClaim: "groups", Provisioning: Provisioning{SyncRole: true}}
}

func defaultLDAP() LDAPConfig {
	return LDAPConfig{UserFilter: "(&(objectClass=person)(|(uid={username})(sAMAccountName={username})))", UsernameAttr: "uid",
		DisplayNameAttr: "cn", EmailAttr: "mail", GroupAttr: "memberOf", GroupFilter: "(|(member={dn})(uniqueMember={dn})(memberUid={username}))",
		Provisioning: Provisioning{SyncRole: true}}
}

// oidcConfig loads the OIDC configuration and its client secret.
func (s *Service) oidcConfig(ctx context.Context) (OIDCConfig, string, error) {
	c := defaultOIDC()
	if _, err := settings.GetJSON(ctx, s.db.R, keyOIDC, &c); err != nil {
		return c, "", err
	}
	secret, err := s.openSecret(ctx, keyOIDCSecret)
	c.HasSecret = secret != ""
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	if c.Mappings == nil {
		c.Mappings = []RoleMapping{}
	}
	return c, secret, err
}

// ldapConfig loads the LDAP configuration and its bind password.
func (s *Service) ldapConfig(ctx context.Context) (LDAPConfig, string, error) {
	c := defaultLDAP()
	if _, err := settings.GetJSON(ctx, s.db.R, keyLDAP, &c); err != nil {
		return c, "", err
	}
	pw, err := s.openSecret(ctx, keyLDAPSecret)
	c.HasPassword = pw != ""
	if c.Mappings == nil {
		c.Mappings = []RoleMapping{}
	}
	return c, pw, err
}

func (s *Service) openSecret(ctx context.Context, key string) (string, error) {
	var sealed string
	if _, err := settings.GetJSON(ctx, s.db.R, key, &sealed); err != nil || sealed == "" {
		return "", err
	}
	if s.vault == nil {
		return "", errors.New("kein Vault verfügbar")
	}
	return s.vault.OpenString(sealed)
}

// setSecret stores (non-nil) or keeps (nil) a secret; "" deletes it.
func (s *Service) setSecret(ctx context.Context, q db.Querier, key string, v *string) error {
	if v == nil {
		return nil
	}
	if *v == "" {
		_, err := q.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", key)
		return err
	}
	if s.vault == nil {
		return errors.New("kein Vault verfügbar")
	}
	sealed, err := s.vault.SealString(*v)
	if err != nil {
		return err
	}
	return settings.SetJSON(ctx, q, key, sealed)
}

// ExternalConfig returns both configurations (secrets only as "set").
func (s *Service) ExternalConfig(ctx context.Context) (*ExternalAuth, error) {
	o, _, err := s.oidcConfig(ctx)
	if err != nil {
		return nil, err
	}
	l, _, err := s.ldapConfig(ctx)
	if err != nil {
		return nil, err
	}
	return &ExternalAuth{OIDC: o, LDAP: l}, nil
}

// Providers returns the enabled sign-in methods for the login page.
func (s *Service) Providers(ctx context.Context) Providers {
	var p Providers
	if o, _, err := s.oidcConfig(ctx); err == nil && o.Enabled {
		p.OIDC, p.OIDCName = true, o.Name
	}
	if l, _, err := s.ldapConfig(ctx); err == nil && l.Enabled {
		p.LDAP = true
	}
	return p
}

func (p *Provisioning) normalize(ctx context.Context, q db.Querier) error {
	var clean []RoleMapping
	for i, m := range p.Mappings {
		m.Group = strings.TrimSpace(m.Group)
		if m.Group == "" && m.RoleID == 0 {
			continue
		}
		if m.Group == "" {
			return plugin.FieldErr(fmt.Sprintf("mappings.%d.group", i), "Gruppe fehlt")
		}
		if err := roleExists(ctx, q, m.RoleID); err != nil {
			return plugin.FieldErr(fmt.Sprintf("mappings.%d.roleId", i), err.Error())
		}
		clean = append(clean, m)
	}
	p.Mappings = clean
	if p.DefaultRoleID != 0 {
		if err := roleExists(ctx, q, p.DefaultRoleID); err != nil {
			return plugin.FieldErr("defaultRoleId", err.Error())
		}
	}
	return nil
}

func roleExists(ctx context.Context, q db.Querier, id int64) error {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles WHERE id = ?", id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return errors.New("Rolle existiert nicht")
	}
	return nil
}

// SaveOIDC stores the OIDC configuration. secret: nil keeps the stored client secret,
// "" removes it.
func (s *Service) SaveOIDC(ctx context.Context, c OIDCConfig, secret *string) (*OIDCConfig, error) {
	c.Name = strings.TrimSpace(c.Name)
	c.Issuer = strings.TrimRight(strings.TrimSpace(c.Issuer), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.UsernameClaim = strings.TrimSpace(c.UsernameClaim)
	c.GroupsClaim = strings.TrimSpace(c.GroupsClaim)
	c.RedirectURL = strings.TrimSpace(c.RedirectURL)
	var scopes []string
	for _, sc := range c.Scopes {
		for _, f := range strings.Fields(sc) {
			if f != "openid" && !contains(scopes, f) {
				scopes = append(scopes, f)
			}
		}
	}
	c.Scopes = scopes
	if c.Name == "" {
		c.Name = "Single Sign-on"
	}
	if c.UsernameClaim == "" {
		c.UsernameClaim = "preferred_username"
	}
	if c.Enabled {
		if u, err := url.Parse(c.Issuer); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, plugin.FieldErr("issuer", "Issuer-URL erwartet, z. B. https://auth.example.org/application/o/netscope")
		}
		if c.ClientID == "" {
			return nil, plugin.FieldErr("clientId", "Client-ID fehlt")
		}
	}
	if c.RedirectURL != "" {
		if u, err := url.Parse(c.RedirectURL); err != nil || u.Host == "" || !strings.HasSuffix(u.Path, oidcCallbackPath) {
			return nil, plugin.FieldErr("redirectUrl", "vollständige URL mit dem Pfad "+oidcCallbackPath+" erwartet")
		}
	}
	if _, err := c.TLSOptions.config(""); err != nil {
		return nil, plugin.FieldErr("ca", err.Error())
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := c.Provisioning.normalize(ctx, tx); err != nil {
			return err
		}
		c.HasSecret = false
		if err := settings.SetJSON(ctx, tx, keyOIDC, c); err != nil {
			return err
		}
		return s.setSecret(ctx, tx, keyOIDCSecret, secret)
	})
	if err != nil {
		return nil, err
	}
	s.oidc.reset()
	out, _, err := s.oidcConfig(ctx)
	return &out, err
}

// SaveLDAP stores the LDAP configuration. password: nil keeps the stored bind password,
// "" removes it.
func (s *Service) SaveLDAP(ctx context.Context, c LDAPConfig, password *string) (*LDAPConfig, error) {
	c.URL = strings.TrimSpace(c.URL)
	c.BindDN = strings.TrimSpace(c.BindDN)
	c.BaseDN = strings.TrimSpace(c.BaseDN)
	c.UserFilter = strings.TrimSpace(c.UserFilter)
	c.UsernameAttr = strings.TrimSpace(c.UsernameAttr)
	c.DisplayNameAttr = strings.TrimSpace(c.DisplayNameAttr)
	c.EmailAttr = strings.TrimSpace(c.EmailAttr)
	c.GroupAttr = strings.TrimSpace(c.GroupAttr)
	c.GroupBaseDN = strings.TrimSpace(c.GroupBaseDN)
	c.GroupFilter = strings.TrimSpace(c.GroupFilter)
	d := defaultLDAP()
	if c.UserFilter == "" {
		c.UserFilter = d.UserFilter
	}
	if c.UsernameAttr == "" {
		c.UsernameAttr = d.UsernameAttr
	}
	if c.Enabled {
		u, err := url.Parse(c.URL)
		if err != nil || (u.Scheme != "ldap" && u.Scheme != "ldaps") || u.Host == "" {
			return nil, plugin.FieldErr("url", "ldap://host oder ldaps://host erwartet")
		}
		if c.StartTLS && u.Scheme == "ldaps" {
			return nil, plugin.FieldErr("startTls", "StartTLS nur mit ldap://")
		}
		if c.BaseDN == "" {
			return nil, plugin.FieldErr("baseDn", "Basis-DN fehlt")
		}
		if !strings.Contains(c.UserFilter, "{username}") {
			return nil, plugin.FieldErr("userFilter", "Filter muss {username} enthalten")
		}
		if c.GroupAttr == "" && c.GroupBaseDN != "" && !strings.Contains(c.GroupFilter, "{dn}") && !strings.Contains(c.GroupFilter, "{username}") {
			return nil, plugin.FieldErr("groupFilter", "Filter muss {dn} oder {username} enthalten")
		}
	}
	if _, err := c.TLSOptions.config(""); err != nil {
		return nil, plugin.FieldErr("ca", err.Error())
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := c.Provisioning.normalize(ctx, tx); err != nil {
			return err
		}
		c.HasPassword = false
		if err := settings.SetJSON(ctx, tx, keyLDAP, c); err != nil {
			return err
		}
		return s.setSecret(ctx, tx, keyLDAPSecret, password)
	})
	if err != nil {
		return nil, err
	}
	out, _, err := s.ldapConfig(ctx)
	return &out, err
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- provisioning

// Identity is a person authenticated by LDAP or OIDC.
type Identity struct {
	Source      string // ldap | oidc
	ExternalID  string // LDAP: DN, OIDC: issuer|subject
	Username    string
	DisplayName string
	Email       string
	Groups      []string
}

// roleFor maps the groups to a role (0 = no access). groupMatch compares a mapping entry
// with a group of the identity.
func (p Provisioning) roleFor(groups []string, groupMatch func(want, have string) bool) int64 {
	for _, m := range p.Mappings {
		for _, g := range groups {
			if groupMatch(m.Group, g) {
				return m.RoleID
			}
		}
	}
	return p.DefaultRoleID
}

var usernameClean = regexp.MustCompile(`[^A-Za-z0-9._@-]+`)

// externalUsername turns a directory or provider name into a valid NetScope user name.
func externalUsername(name string) string {
	name = strings.TrimSpace(name)
	name = usernameClean.ReplaceAllString(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return '.'
		}
		return r
	}, name), "")
	name = strings.TrimLeft(name, "._@-")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// provision finds or creates the user of an identity and returns its id. The role is set
// at creation and, with SyncRole, at every sign-in.
func (s *Service) provision(ctx context.Context, id Identity, p Provisioning, groupMatch func(want, have string) bool) (int64, error) {
	role := p.roleFor(id.Groups, groupMatch)
	username := externalUsername(id.Username)
	if !usernameRe.MatchString(username) {
		return 0, fmt.Errorf("Der Benutzername %q ist in NetScope nicht zulässig", id.Username)
	}
	if len([]rune(id.DisplayName)) > 100 {
		id.DisplayName = string([]rune(id.DisplayName)[:100])
	}
	if len(id.Email) > 200 || !strings.Contains(id.Email, "@") {
		id.Email = ""
	}
	var userID int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			disabled bool
			roleID   int64
		)
		err := tx.QueryRowContext(ctx, "SELECT id, disabled, role_id FROM users WHERE auth_source = ? AND external_id = ?",
			id.Source, id.ExternalID).Scan(&userID, &disabled, &roleID)
		if errors.Is(err, sql.ErrNoRows) && id.Source == SourceLDAP {
			// the account moved in the directory (new DN): the login name is unique there
			err = tx.QueryRowContext(ctx, "SELECT id, disabled, role_id FROM users WHERE auth_source = ? AND username = ? COLLATE NOCASE",
				id.Source, username).Scan(&userID, &disabled, &roleID)
			if err == nil {
				if _, err := tx.ExecContext(ctx, "UPDATE users SET external_id = ? WHERE id = ?", id.ExternalID, userID); err != nil {
					return err
				}
			}
		}
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if role == 0 {
				return ErrNoRole
			}
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE", username).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return fmt.Errorf("Der Benutzername %s ist in NetScope bereits vergeben – ein Administrator muss das bestehende Konto umbenennen oder entfernen", username)
			}
			now := db.Now()
			res, err := tx.ExecContext(ctx, `INSERT INTO users(username, password_hash, display_name, email, role_id, auth_source, external_id,
				created_at, updated_at) VALUES (?, '', ?, ?, ?, ?, ?, ?, ?)`, username, id.DisplayName, id.Email, role, id.Source, id.ExternalID, now, now)
			if err != nil {
				return err
			}
			userID, _ = res.LastInsertId()
			return nil
		case err != nil:
			return err
		}
		if disabled {
			return ErrAccountDisabled
		}
		newRole := roleID
		if p.SyncRole {
			if role == 0 {
				return ErrNoRole
			}
			newRole = role
		}
		// name and address follow the source; the user name stays (it may have been renamed)
		_, err = tx.ExecContext(ctx, `UPDATE users SET display_name = ?, email = ?, role_id = ?, updated_at = ? WHERE id = ?`,
			id.DisplayName, id.Email, newRole, db.Now(), userID)
		return err
	})
	return userID, err
}

// userSource returns the source of an account.
func (s *Service) userSource(ctx context.Context, userID int64) (string, error) {
	var src string
	err := s.db.R.QueryRowContext(ctx, "SELECT auth_source FROM users WHERE id = ?", userID).Scan(&src)
	return src, db.NotFound(err)
}

// errExternal refuses password operations for accounts managed elsewhere.
func errExternal(src string) error {
	switch src {
	case SourceLDAP:
		return errors.New("Das Konto wird über LDAP verwaltet; das Passwort wird dort geändert")
	case SourceOIDC:
		return errors.New("Das Konto meldet sich über den Identity Provider an und hat kein NetScope-Passwort")
	}
	return nil
}
