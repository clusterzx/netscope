package auth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// ldapTimeout bounds connecting and every directory operation.
const ldapTimeout = 10 * time.Second

// errLDAPAmbiguous: the user filter found several accounts; signing in would be a guess.
var errLDAPAmbiguous = errors.New("Der LDAP-Filter findet mehrere Konten für diesen Benutzernamen – Filter in den Einstellungen eindeutiger machen")

func (c LDAPConfig) dial(ctx context.Context) (*ldap.Conn, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("ungültige LDAP-URL %q", c.URL)
	}
	tlsCfg, err := c.TLSOptions.config(u.Hostname())
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: ldapTimeout}
	conn, err := ldap.DialURL(c.URL, ldap.DialWithDialer(d), ldap.DialWithTLSConfig(tlsCfg))
	if err != nil {
		return nil, fmt.Errorf("Verbindung zu %s fehlgeschlagen: %w", u.Host, err)
	}
	conn.SetTimeout(ldapTimeout)
	if c.StartTLS {
		if err := conn.StartTLS(tlsCfg); err != nil {
			conn.Close()
			return nil, fmt.Errorf("StartTLS: %w", err)
		}
	}
	// give up when the caller does
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	return conn, nil
}

// serviceBind signs in as the search account (or anonymously without one).
func (c LDAPConfig) serviceBind(conn *ldap.Conn, password string) error {
	if c.BindDN == "" {
		return nil
	}
	if err := conn.Bind(c.BindDN, password); err != nil {
		return fmt.Errorf("Anmeldung des Dienstkontos %s: %w", c.BindDN, err)
	}
	return nil
}

func (c LDAPConfig) attrs() []string {
	var out []string
	for _, a := range []string{c.UsernameAttr, c.DisplayNameAttr, c.EmailAttr, c.GroupAttr} {
		if a != "" && !contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		out = []string{"1.1"} // no attributes, only the DN
	}
	return out
}

// findUser searches the account of a login name (nil: not found).
func (c LDAPConfig) findUser(conn *ldap.Conn, username string) (*ldap.Entry, error) {
	filter := strings.ReplaceAll(c.UserFilter, "{username}", ldap.EscapeFilter(username))
	res, err := conn.Search(ldap.NewSearchRequest(c.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(ldapTimeout/time.Second),
		false, filter, c.attrs(), nil))
	if err != nil {
		switch {
		case ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded):
			return nil, errLDAPAmbiguous
		case ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject):
			return nil, nil // some servers answer an empty result this way
		}
		return nil, fmt.Errorf("Benutzersuche: %w", err)
	}
	switch len(res.Entries) {
	case 0:
		return nil, nil
	case 1:
		return res.Entries[0], nil
	}
	return nil, errLDAPAmbiguous
}

// groups returns the groups of an account: the GroupAttr values, or the DNs of the groups
// found below GroupBaseDN.
func (c LDAPConfig) groups(conn *ldap.Conn, e *ldap.Entry, username string) ([]string, error) {
	if c.GroupAttr != "" {
		return e.GetAttributeValues(c.GroupAttr), nil
	}
	if c.GroupBaseDN == "" || c.GroupFilter == "" {
		return nil, nil
	}
	filter := strings.ReplaceAll(c.GroupFilter, "{dn}", ldap.EscapeFilter(e.DN))
	filter = strings.ReplaceAll(filter, "{username}", ldap.EscapeFilter(username))
	res, err := conn.Search(ldap.NewSearchRequest(c.GroupBaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1000,
		int(ldapTimeout/time.Second), false, filter, []string{"1.1"}, nil))
	if err != nil {
		return nil, fmt.Errorf("Gruppensuche: %w", err)
	}
	out := make([]string, 0, len(res.Entries))
	for _, g := range res.Entries {
		out = append(out, g.DN)
	}
	return out, nil
}

// identity builds the NetScope view of a directory account.
func (c LDAPConfig) identity(e *ldap.Entry, login string, groups []string) Identity {
	id := Identity{Source: SourceLDAP, ExternalID: strings.ToLower(e.DN), Username: login, Groups: groups}
	if c.UsernameAttr != "" {
		if v := e.GetAttributeValue(c.UsernameAttr); v != "" {
			id.Username = v
		}
	}
	if c.DisplayNameAttr != "" {
		id.DisplayName = e.GetAttributeValue(c.DisplayNameAttr)
	}
	if c.EmailAttr != "" {
		id.Email = e.GetAttributeValue(c.EmailAttr)
	}
	return id
}

// ldapGroupMatch compares a mapping entry with a group: the full DN or its first value
// (the CN of cn=admins,ou=groups,dc=example,dc=org), both case-insensitive.
func ldapGroupMatch(want, have string) bool {
	if strings.EqualFold(strings.TrimSpace(want), strings.TrimSpace(have)) {
		return true
	}
	if dn, err := ldap.ParseDN(have); err == nil && len(dn.RDNs) > 0 && len(dn.RDNs[0].Attributes) > 0 {
		return strings.EqualFold(strings.TrimSpace(want), dn.RDNs[0].Attributes[0].Value)
	}
	return false
}

// ldapAuthenticate checks a login name and password against the directory and returns
// the identity. Wrong or empty credentials give ErrInvalidCredentials.
func (s *Service) ldapAuthenticate(ctx context.Context, c LDAPConfig, bindPW, username, password string) (*Identity, error) {
	// an empty password is an "unauthenticated bind" that many servers accept
	if strings.TrimSpace(username) == "" || password == "" {
		return nil, ErrInvalidCredentials
	}
	ctx, cancel := context.WithTimeout(ctx, 3*ldapTimeout)
	defer cancel()
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := c.serviceBind(conn, bindPW); err != nil {
		return nil, err
	}
	e, err := c.findUser(conn, username)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, ErrInvalidCredentials
	}
	if err := conn.Bind(e.DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("LDAP-Anmeldung: %w", err)
	}
	// groups are read with the rights of the service account
	if c.GroupAttr == "" && c.BindDN != "" {
		if err := c.serviceBind(conn, bindPW); err != nil {
			return nil, err
		}
	}
	groups, err := c.groups(conn, e, username)
	if err != nil {
		return nil, err
	}
	id := c.identity(e, username, groups)
	return &id, nil
}

// loginLDAP authenticates against the directory and provisions the account. It returns
// ErrInvalidCredentials when LDAP is off or the credentials are wrong.
func (s *Service) loginLDAP(ctx context.Context, username, password string) (int64, error) {
	c, pw, err := s.ldapConfig(ctx)
	if err != nil {
		return 0, err
	}
	if !c.Enabled {
		return 0, ErrInvalidCredentials
	}
	id, err := s.ldapAuthenticate(ctx, c, pw, username, password)
	if err != nil {
		return 0, err
	}
	return s.provision(ctx, *id, c.Provisioning, ldapGroupMatch)
}

// LDAPTest is the outcome of a configuration test.
type LDAPTest struct {
	OK bool `json:"ok"`
	// Steps describe what was checked, in order.
	Steps []TestStep `json:"steps"`
	// the account found for the test user
	DN          string   `json:"dn,omitempty"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"displayName,omitempty"`
	Email       string   `json:"email,omitempty"`
	Groups      []string `json:"groups,omitempty"`
	// RoleID the account would get (0 = no access).
	RoleID int64 `json:"roleId"`
}

// TestStep is one check of a configuration test.
type TestStep struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func (t *LDAPTest) step(name string, err error) bool {
	st := TestStep{Name: name, OK: err == nil}
	if err != nil {
		st.Error = err.Error()
	}
	t.Steps = append(t.Steps, st)
	return err == nil
}

// TestLDAP checks a configuration without saving it: connection, service account, and
// optionally the search (username) and sign-in (password) of a test user. bindPW nil uses
// the stored bind password.
func (s *Service) TestLDAP(ctx context.Context, c LDAPConfig, bindPW *string, username, password string) (*LDAPTest, error) {
	pw := ""
	if bindPW != nil {
		pw = *bindPW
	} else if _, stored, err := s.ldapConfig(ctx); err == nil {
		pw = stored
	}
	d := defaultLDAP()
	if c.UserFilter == "" {
		c.UserFilter = d.UserFilter
	}
	ctx, cancel := context.WithTimeout(ctx, 3*ldapTimeout)
	defer cancel()
	t := &LDAPTest{Steps: []TestStep{}}
	conn, err := c.dial(ctx)
	if !t.step("Verbindung", err) {
		return t, nil
	}
	defer conn.Close()
	name := "Anonyme Anmeldung"
	if c.BindDN != "" {
		name = "Dienstkonto"
	}
	if !t.step(name, c.serviceBind(conn, pw)) {
		return t, nil
	}
	if strings.TrimSpace(username) == "" {
		t.OK = true
		return t, nil
	}
	e, err := c.findUser(conn, username)
	if err == nil && e == nil {
		err = fmt.Errorf("kein Konto für %q gefunden", username)
	}
	if !t.step("Benutzer suchen", err) {
		return t, nil
	}
	t.DN = e.DN
	if password != "" {
		err := conn.Bind(e.DN, password)
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			err = errors.New("Passwort falsch")
		}
		if !t.step("Anmeldung des Benutzers", err) {
			return t, nil
		}
		if c.GroupAttr == "" && c.BindDN != "" {
			if !t.step("Dienstkonto (Gruppen)", c.serviceBind(conn, pw)) {
				return t, nil
			}
		}
	}
	groups, err := c.groups(conn, e, username)
	if !t.step("Gruppen lesen", err) {
		return t, nil
	}
	id := c.identity(e, username, groups)
	t.Username, t.DisplayName, t.Email, t.Groups = externalUsername(id.Username), id.DisplayName, id.Email, groups
	if t.Groups == nil {
		t.Groups = []string{}
	}
	t.RoleID = c.Provisioning.roleFor(groups, ldapGroupMatch)
	t.OK = true
	return t, nil
}
