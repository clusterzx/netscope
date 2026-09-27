package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/go-hclog"
	"github.com/jimlambrt/gldap/testdirectory"
)

const (
	groupAdmins = "cn=netscope-admins,ou=groups,dc=example,dc=org"
	groupStaff  = "cn=staff,ou=groups,dc=example,dc=org"
)

// startDirectory runs an in-process LDAP server (LDAPS with its own CA) with alice (admins),
// bob (staff), carol (no group) and the service account svc; every password is "password".
func startDirectory(t *testing.T, opts ...testdirectory.Option) *testdirectory.Directory {
	t.Helper()
	opts = append(opts, testdirectory.WithLogger(t, hclog.NewNullLogger()))
	td := testdirectory.Start(t, opts...)
	users := testdirectory.NewUsers(t, []string{"alice"}, testdirectory.WithMembersOf(t, groupAdmins))
	users = append(users, testdirectory.NewUsers(t, []string{"bob"}, testdirectory.WithMembersOf(t, groupStaff))...)
	users = append(users, testdirectory.NewUsers(t, []string{"carol", "svc"})...)
	td.SetUsers(users...)
	td.SetGroups(testdirectory.NewGroup(t, "netscope-admins", []string{"alice"}), testdirectory.NewGroup(t, "staff", []string{"bob"}))
	return td
}

// ldapSetup configures LDAP sign-in against td: admins → Administrator, staff → a
// read-only role, everyone else no access.
func ldapSetup(t *testing.T, s *Service, td *testdirectory.Directory, tls bool) (LDAPConfig, int64, int64) {
	t.Helper()
	ctx := context.Background()
	var admin, viewer int64
	_ = s.db.R.QueryRow("SELECT id FROM roles WHERE builtin = 'admin'").Scan(&admin)
	r, err := s.CreateRole(ctx, RoleInput{Name: "Lesen", Permissions: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	viewer = r.ID
	scheme := "ldap"
	if tls {
		scheme = "ldaps"
	}
	c := LDAPConfig{Enabled: true, URL: fmt.Sprintf("%s://%s:%d", scheme, td.Host(), td.Port()), BindDN: "cn=svc,ou=people,dc=example,dc=org",
		BaseDN: "ou=people,dc=example,dc=org", UserFilter: "(cn={username})", UsernameAttr: "", DisplayNameAttr: "name", EmailAttr: "email",
		GroupAttr: "memberOf", Provisioning: Provisioning{Mappings: []RoleMapping{{Group: "netscope-admins", RoleID: admin}, {Group: groupStaff, RoleID: viewer}},
			SyncRole: true}}
	if tls {
		c.CA = td.Cert()
	}
	pw := "password"
	out, err := s.SaveLDAP(ctx, c, &pw)
	if err != nil {
		t.Fatal(err)
	}
	if !out.HasPassword {
		t.Fatal("bind password not stored")
	}
	return *out, admin, viewer
}

func TestLDAPLogin(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	if _, _, err := s.EnsureAdmin(ctx, "local-admin-pw"); err != nil {
		t.Fatal(err)
	}
	td := startDirectory(t)
	_, admin, viewer := ldapSetup(t, s, td, true)

	// first sign-in creates the account, role from the group CN
	tok := login(t, s, "alice", "password")
	p, _, _, _ := s.Session(ctx, tok)
	if p.Username != "alice" || !p.Admin || p.RoleID != admin || p.Restricted() != "" {
		t.Fatalf("alice: %+v", p)
	}
	u, _ := s.User(ctx, p.UserID)
	if u.AuthSource != SourceLDAP || u.Email != "alice@example.com" || u.DisplayName != "alice" {
		t.Fatalf("user %+v", u)
	}
	var ext string
	_ = s.db.R.QueryRow("SELECT external_id FROM users WHERE id = ?", p.UserID).Scan(&ext)
	if ext != "cn=alice,ou=people,dc=example,dc=org" {
		t.Errorf("external id %q", ext)
	}

	// group by full DN, second sign-in keeps the same account
	p2 := sessionOf(t, s, login(t, s, "bob", "password"))
	if p2.RoleID != viewer || p2.Admin {
		t.Fatalf("bob: %+v", p2)
	}
	// (the test directory compares case-sensitively, real servers do not)
	if again := sessionOf(t, s, login(t, s, "bob", "password")); again.UserID != p2.UserID {
		t.Fatal("second sign-in created another account")
	}

	// wrong or empty password, unknown user, no group
	for _, c := range []struct{ user, pw string }{{"alice", "wrong"}, {"alice", ""}, {"nobody", "password"}, {"", "password"}} {
		if _, err := s.Login(ctx, c.user, c.pw, "", "2.2.2.2", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("%q/%q: %v", c.user, c.pw, err)
		}
	}
	if _, err := s.Login(ctx, "carol", "password", "", "3.3.3.3", ""); !errors.Is(err, ErrNoRole) {
		t.Errorf("carol without group: %v", err)
	}

	// external accounts have no NetScope password
	if err := s.ChangePassword(ctx, p.UserID, "", "password", "a-new-password"); err == nil || !strings.Contains(err.Error(), "LDAP") {
		t.Errorf("change password: %v", err)
	}
	if _, err := s.SetTemporaryPassword(ctx, p.UserID); err == nil {
		t.Error("temporary password set for an LDAP account")
	}
	if err := s.VerifyPassword(ctx, p.UserID, "password"); err != nil {
		t.Errorf("verify against LDAP: %v", err)
	}
	if err := s.VerifyPassword(ctx, p.UserID, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("verify wrong: %v", err)
	}

	// a local account of the same name always wins
	if _, _, err := s.CreateUser(ctx, UserInput{Username: "carol", RoleID: viewer, Password: "local-carol-pw"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "carol", "password", "", "4.4.4.4", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("directory password for a local account: %v", err)
	}
	login(t, s, "carol", "local-carol-pw")

	// disabled in NetScope stays disabled
	if _, err := s.UpdateUser(ctx, 1, p2.UserID, UserInput{Username: "bob", RoleID: viewer, Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "bob", "password", "", "5.5.5.5", ""); !errors.Is(err, ErrAccountDisabled) {
		t.Errorf("disabled bob: %v", err)
	}

	// LDAP off: directory accounts cannot sign in, the local admin still can
	c, _, _ := s.ldapConfig(ctx)
	c.Enabled = false
	if _, err := s.SaveLDAP(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "alice", "password", "", "6.6.6.6", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("LDAP off: %v", err)
	}
	login(t, s, "admin", "local-admin-pw")
}

func sessionOf(t *testing.T, s *Service, tok string) *Principal {
	t.Helper()
	p, _, _, err := s.Session(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLDAPRoleSyncAndLastLocalAdmin(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	if _, _, err := s.EnsureAdmin(ctx, "local-admin-pw"); err != nil {
		t.Fatal(err)
	}
	td := startDirectory(t, testdirectory.WithNoTLS(t))
	c, _, viewer := ldapSetup(t, s, td, false)
	alice := sessionOf(t, s, login(t, s, "alice", "password"))

	// the directory decides the role at every sign-in (SyncRole)
	if _, err := s.UpdateUser(ctx, 1, alice.UserID, UserInput{Username: "alice", RoleID: viewer}); err != nil {
		t.Fatal(err)
	}
	if p := sessionOf(t, s, login(t, s, "alice", "password")); !p.Admin {
		t.Error("role not synced from the directory")
	}
	// without SyncRole a manual change sticks
	c.SyncRole = false
	if _, err := s.SaveLDAP(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(ctx, 1, alice.UserID, UserInput{Username: "alice", RoleID: viewer}); err != nil {
		t.Fatal(err)
	}
	if p := sessionOf(t, s, login(t, s, "alice", "password")); p.Admin {
		t.Error("manual role overwritten without SyncRole")
	}

	// an external administrator does not replace the last local one
	if _, err := s.UpdateUser(ctx, alice.UserID, alice.UserID, UserInput{Username: "alice", RoleID: 1}); err != nil {
		t.Fatal(err)
	}
	var adminID int64
	_ = s.db.R.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID)
	if err := s.DeleteUser(ctx, alice.UserID, adminID); !errors.Is(err, errLastLocalAdmin) {
		t.Errorf("deleting the last local admin: %v", err)
	}
	if _, err := s.UpdateUser(ctx, alice.UserID, adminID, UserInput{Username: "admin", RoleID: viewer}); !errors.Is(err, errLastLocalAdmin) {
		t.Errorf("demoting the last local admin: %v", err)
	}
	// the external admin itself may go
	if err := s.DeleteUser(ctx, adminID, alice.UserID); err != nil {
		t.Errorf("deleting the external admin: %v", err)
	}
}

func TestLDAPConfigAndTest(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	td := startDirectory(t)
	c, admin, _ := ldapSetup(t, s, td, true)

	// validation
	for _, bad := range []LDAPConfig{
		{Enabled: true, URL: "http://x", BaseDN: "dc=x", UserFilter: "(uid={username})"},
		{Enabled: true, URL: "ldap://x", UserFilter: "(uid={username})"},
		{Enabled: true, URL: "ldap://x", BaseDN: "dc=x", UserFilter: "(uid=fixed)"},
		{Enabled: true, URL: "ldaps://x", StartTLS: true, BaseDN: "dc=x", UserFilter: "(uid={username})"},
		{URL: "ldap://x", Provisioning: Provisioning{Mappings: []RoleMapping{{Group: "g", RoleID: 999}}}},
		{URL: "ldap://x", TLSOptions: TLSOptions{CA: "not pem"}},
	} {
		if _, err := s.SaveLDAP(ctx, bad, nil); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}

	// the stored password is kept when none is sent; the test uses it
	res, err := s.TestLDAP(ctx, c, nil, "alice", "password")
	if err != nil || !res.OK || res.DN != "cn=alice,ou=people,dc=example,dc=org" || res.RoleID != admin || len(res.Groups) != 1 {
		t.Fatalf("test: %+v %v", res, err)
	}
	res, _ = s.TestLDAP(ctx, c, nil, "alice", "wrong")
	if res.OK || res.Steps[len(res.Steps)-1].Error != "Passwort falsch" {
		t.Errorf("wrong password: %+v", res)
	}
	wrong := "nope"
	res, _ = s.TestLDAP(ctx, c, &wrong, "", "")
	if res.OK || len(res.Steps) != 2 || res.Steps[1].OK {
		t.Errorf("wrong bind password: %+v", res)
	}
	// without the CA the certificate is not trusted
	noCA := c
	noCA.CA = ""
	res, _ = s.TestLDAP(ctx, noCA, nil, "", "")
	if res.OK || res.Steps[0].OK {
		t.Errorf("untrusted certificate accepted: %+v", res)
	}

	// the bind password is sealed by the vault and never returned
	var raw string
	_ = s.db.R.QueryRow("SELECT value FROM settings WHERE key = ?", keyLDAPSecret).Scan(&raw)
	if raw == "" || strings.Contains(raw, "password") {
		t.Errorf("stored secret %q", raw)
	}
	cfg, _ := s.ExternalConfig(ctx)
	if !cfg.LDAP.HasPassword || !cfg.LDAP.Enabled || cfg.OIDC.Enabled {
		t.Errorf("config %+v", cfg)
	}
	empty := ""
	if out, _ := s.SaveLDAP(ctx, c, &empty); out.HasPassword {
		t.Error("password not removed")
	}
}

func TestLDAPGroupSearchAndMatching(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	td := startDirectory(t, testdirectory.WithNoTLS(t))
	c, admin, _ := ldapSetup(t, s, td, false)
	c.GroupAttr = ""
	c.GroupBaseDN = "ou=groups,dc=example,dc=org"
	c.GroupFilter = "(|(member={dn}))"
	if _, err := s.SaveLDAP(ctx, c, nil); err != nil {
		t.Fatal(err)
	}
	p := sessionOf(t, s, login(t, s, "alice", "password"))
	if p.RoleID != admin {
		t.Fatalf("group search: %+v", p)
	}
	for _, m := range []struct {
		want, have string
		ok         bool
	}{
		{"netscope-admins", groupAdmins, true},
		{"NETSCOPE-ADMINS", groupAdmins, true},
		{groupAdmins, strings.ToUpper(groupAdmins), true},
		{"groups", groupAdmins, false},
		{"netscope", groupAdmins, false},
	} {
		if got := ldapGroupMatch(m.want, m.have); got != m.ok {
			t.Errorf("match %q %q = %v", m.want, m.have, got)
		}
	}
}
