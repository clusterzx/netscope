package auth

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/vault"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	key, _ := vault.NewKey()
	v, err := vault.Open(ctx, d, key, vault.KeySource{})
	if err != nil {
		t.Fatal(err)
	}
	return New(d, v)
}

// login runs a password login that must end in a session.
func login(t *testing.T, s *Service, user, pw string) string {
	t.Helper()
	res, err := s.Login(context.Background(), user, pw, "", "1.1.1.1", "test")
	if err != nil || res.Token == "" {
		t.Fatalf("login %s: %+v %v", user, res, err)
	}
	return res.Token
}

func TestLoginSessionAndPasswordChange(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	created, gen, err := s.EnsureAdmin(ctx, "")
	if err != nil || !created || len(gen) != 20 {
		t.Fatalf("EnsureAdmin: %v %v %q", created, err, gen)
	}
	if again, _, _ := s.EnsureAdmin(ctx, ""); again {
		t.Fatal("admin created twice")
	}
	if _, err := s.Login(ctx, "admin", "wrong", "", "1.1.1.1", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	tok := login(t, s, "ADMIN", gen) // user names are case-insensitive
	p, _, _, err := s.Session(ctx, tok)
	if err != nil || p.Username != "admin" || !p.CanWrite() || !p.Admin || !p.Has(PermUsersManage) || p.Restricted() != "" {
		t.Fatalf("session: %+v %v", p, err)
	}
	if len(p.Permissions) != len(Permissions) {
		t.Fatalf("admin must hold every permission: %v", p.Permissions)
	}
	tok2 := login(t, s, "admin", gen)
	if err := s.ChangePassword(ctx, p.UserID, p.SessionID, gen, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if err := s.ChangePassword(ctx, p.UserID, p.SessionID, gen, "a-new-long-password"); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Session(ctx, tok); err != nil {
		t.Fatal("current session must survive password change")
	}
	if _, _, _, err := s.Session(ctx, tok2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("other sessions must end on password change")
	}
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Session(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("session alive after logout")
	}
}

func TestTokens(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	if _, _, err := s.EnsureAdmin(ctx, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	uid, _ := s.AdminID(ctx)
	plain, tok, err := s.CreateToken(ctx, uid, "script", ScopeRead, nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.TokenPrincipal(ctx, plain, "1.2.3.4")
	if err != nil || p.CanWrite() || p.TokenID != tok.ID || tok.Owner != "admin" {
		t.Fatalf("token principal %+v %v", p, err)
	}
	if _, err := s.TokenPrincipal(ctx, plain+"x", ""); err == nil {
		t.Fatal("modified token accepted")
	}
	future := time.Now().Add(time.Hour)
	plain2, _, err := s.CreateToken(ctx, uid, "rw", ScopeWrite, &future)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.TokenPrincipal(ctx, plain2, ""); err != nil || !p.CanWrite() {
		t.Fatal("write token")
	}
	if err := s.DeleteToken(ctx, tok.ID, uid+1); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("token of another user deleted: %v", err)
	}
	if err := s.DeleteToken(ctx, tok.ID, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TokenPrincipal(ctx, plain, ""); err == nil {
		t.Fatal("revoked token accepted")
	}
}

func TestRateLimit(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	_, _, _ = s.EnsureAdmin(ctx, "correct-horse-battery")
	for i := 0; i < 5; i++ {
		_, _ = s.Login(ctx, "admin", "bad", "", "9.9.9.9", "")
	}
	if _, err := s.Login(ctx, "admin", "correct-horse-battery", "", "9.9.9.9", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, err := s.Login(ctx, "admin", "correct-horse-battery", "", "8.8.8.8", ""); err != nil {
		t.Fatalf("other ip blocked: %v", err)
	}
}

func TestTOTPVector(t *testing.T) {
	// RFC 6238 appendix B (SHA1), last six digits
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		t    int64
		want string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := totpAt(secret, c.t/totpPeriod); got != c.want {
			t.Errorf("T=%d: got %s want %s", c.t, got, c.want)
		}
	}
}

// totpNow returns the code of the current step (+offset) for a setup.
func totpNow(t *testing.T, setup *TOTPSetup, offset int64) string {
	t.Helper()
	secret, err := b32.DecodeString(setup.Secret)
	if err != nil {
		t.Fatal(err)
	}
	return totpAt(secret, time.Now().Unix()/totpPeriod+offset)
}

func TestTOTPLogin(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	_, _, _ = s.EnsureAdmin(ctx, "correct-horse-battery")
	uid, _ := s.AdminID(ctx)
	setup, err := s.BeginTOTP(ctx, uid, "netscope.lan")
	if err != nil || setup.QR == "" || setup.URI == "" {
		t.Fatalf("begin: %+v %v", setup, err)
	}
	if _, err := s.ConfirmTOTP(ctx, uid, "000000"); err == nil && totpNow(t, setup, 0) != "000000" {
		t.Fatal("wrong confirmation code accepted")
	}
	codes, err := s.ConfirmTOTP(ctx, uid, totpNow(t, setup, 0))
	if err != nil || len(codes) != recoveryCodeCount {
		t.Fatalf("confirm: %v %v", codes, err)
	}

	res, err := s.Login(ctx, "admin", "correct-horse-battery", "", "1.1.1.1", "")
	if err != nil || res.Token != "" || res.Challenge == "" || !slices.Equal(res.Methods, []string{"totp", "recovery"}) {
		t.Fatalf("login must ask for the second factor: %+v %v", res, err)
	}
	// the confirmation code cannot be used again
	if _, err := s.LoginTOTP(ctx, res.Challenge, totpNow(t, setup, 0), "1.1.1.1", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("replayed code: %v", err)
	}
	out, err := s.LoginTOTP(ctx, res.Challenge, totpNow(t, setup, 1), "1.1.1.1", "")
	if err != nil || out.Token == "" {
		t.Fatalf("totp login: %+v %v", out, err)
	}
	if _, err := s.LoginTOTP(ctx, res.Challenge, totpNow(t, setup, 1), "1.1.1.1", ""); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("challenge must end with the session: %v", err)
	}

	// recovery codes work once
	res, _ = s.Login(ctx, "admin", "correct-horse-battery", "", "1.1.1.1", "")
	if out, err := s.LoginRecovery(ctx, res.Challenge, " "+codes[0]+" ", "1.1.1.1", ""); err != nil || out.Token == "" {
		t.Fatalf("recovery login: %v", err)
	}
	s.mfaLimiter.success(strconv.FormatInt(uid, 10)) // count the failures below on their own
	res, _ = s.Login(ctx, "admin", "correct-horse-battery", "", "1.1.1.1", "")
	if _, err := s.LoginRecovery(ctx, res.Challenge, codes[0], "1.1.1.1", ""); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("used recovery code accepted: %v", err)
	}
	// five wrong answers end the challenge
	for i := 0; i < maxChallengeFailures-2; i++ {
		_, _ = s.LoginTOTP(ctx, res.Challenge, "abcdef", "2.2.2.2", "")
	}
	if _, err := s.LoginTOTP(ctx, res.Challenge, "abcdef", "2.2.2.2", ""); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("challenge must end after %d failures: %v", maxChallengeFailures, err)
	}

	// switching off needs the password and leaves no recovery codes behind
	if err := s.DisableTOTP(ctx, uid, "wrong"); err == nil {
		t.Fatal("disabled without password")
	}
	if err := s.DisableTOTP(ctx, uid, "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.MFA(ctx, uid); st.TOTP || st.RecoveryCodes != 0 {
		t.Fatalf("after disable: %+v", st)
	}
	login(t, s, "admin", "correct-horse-battery")
}

func TestRolesAndUsers(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	_, _, _ = s.EnsureAdmin(ctx, "correct-horse-battery")
	admin, _ := s.AdminID(ctx)
	role, err := s.CreateRole(ctx, RoleInput{Name: "Helpdesk", Permissions: []string{PermEventsAck, PermEventsAck, PermDevicesEdit}})
	if err != nil || len(role.Permissions) != 2 {
		t.Fatalf("create role: %+v %v", role, err)
	}
	if _, err := s.CreateRole(ctx, RoleInput{Name: "helpdesk"}); err == nil {
		t.Fatal("duplicate role name accepted")
	}
	if _, err := s.CreateRole(ctx, RoleInput{Name: "X", Permissions: []string{"root"}}); err == nil {
		t.Fatal("unknown permission accepted")
	}
	u, pw, err := s.CreateUser(ctx, UserInput{Username: "anna", RoleID: role.ID})
	if err != nil || pw == "" || !u.MustChangePassword || u.RoleName != "Helpdesk" {
		t.Fatalf("create user: %+v %v", u, err)
	}
	if _, _, err := s.CreateUser(ctx, UserInput{Username: "Anna", RoleID: role.ID}); err == nil {
		t.Fatal("duplicate user name accepted")
	}

	// the start password has to be changed first
	tok := login(t, s, "anna", pw)
	p, _, _, _ := s.Session(ctx, tok)
	if p.Restricted() != "password" || p.Admin || !p.Has(PermEventsAck) || p.Has(PermUsersManage) || !p.Has("") {
		t.Fatalf("restricted principal: %+v", p)
	}
	if err := s.ChangePassword(ctx, u.ID, p.SessionID, pw, "annas-own-password"); err != nil {
		t.Fatal(err)
	}
	p, _, _, _ = s.Session(ctx, tok)
	if p.Restricted() != "" {
		t.Fatalf("still restricted: %+v", p)
	}

	// a role change applies at once; a read token only reads
	if _, err := s.UpdateRole(ctx, role.ID, RoleInput{Name: "Helpdesk", Permissions: []string{PermAuditView}}); err != nil {
		t.Fatal(err)
	}
	p, _, _, _ = s.Session(ctx, tok)
	if p.Has(PermEventsAck) || !p.Has(PermAuditView) {
		t.Fatalf("role change not applied: %v", p.Permissions)
	}
	plain, _, _ := s.CreateToken(ctx, u.ID, "ro", ScopeRead, nil)
	tp, err := s.TokenPrincipal(ctx, plain, "")
	if err != nil || tp.CanWrite() || !tp.Has(PermAuditView) {
		t.Fatalf("token principal: %+v %v", tp, err)
	}

	// disabling ends sessions and tokens
	if _, err := s.UpdateUser(ctx, admin, u.ID, UserInput{Username: "anna", RoleID: role.ID, Disabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Session(ctx, tok); err == nil {
		t.Fatal("session of disabled user")
	}
	if _, err := s.TokenPrincipal(ctx, plain, ""); err == nil {
		t.Fatal("token of disabled user")
	}
	if _, err := s.Login(ctx, "anna", "annas-own-password", "", "1.1.1.1", ""); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("disabled login: %v", err)
	}

	// roles in use and the administrator role stay
	if err := s.DeleteRole(ctx, role.ID); err == nil {
		t.Fatal("role in use deleted")
	}
	if err := s.DeleteRole(ctx, 1); err == nil {
		t.Fatal("administrator role deleted")
	}
	adminRole, err := s.UpdateRole(ctx, 1, RoleInput{Name: "Chef", Description: "alles", Permissions: nil, Require2FA: true})
	if err != nil || adminRole.Name != "Administrator" || !adminRole.Require2FA || len(adminRole.Permissions) != len(Permissions) {
		t.Fatalf("admin role update: %+v %v", adminRole, err)
	}

	// an active administrator always remains
	if _, err := s.UpdateUser(ctx, admin, admin, UserInput{Username: "admin", RoleID: role.ID}); !errors.Is(err, errLastAdmin) {
		t.Fatalf("last admin demoted: %v", err)
	}
	if err := s.DeleteUser(ctx, admin, admin); err == nil {
		t.Fatal("own account deleted")
	}
	second, _, _ := s.CreateUser(ctx, UserInput{Username: "boss", RoleID: 1})
	if err := s.DeleteUser(ctx, second.ID, admin); err != nil {
		t.Fatalf("admin with a second admin left: %v", err)
	}
	if err := s.DeleteUser(ctx, 0, second.ID); !errors.Is(err, errLastAdmin) {
		t.Fatalf("last admin deleted: %v", err)
	}
}

func TestRequire2FA(t *testing.T) {
	ctx := context.Background()
	s := newSvc(t)
	_, _, _ = s.EnsureAdmin(ctx, "correct-horse-battery")
	role, _ := s.CreateRole(ctx, RoleInput{Name: "Ops", Require2FA: true})
	u, pw, _ := s.CreateUser(ctx, UserInput{Username: "ops", RoleID: role.ID})
	tok := login(t, s, "ops", pw)
	p, _, _, _ := s.Session(ctx, tok)
	_ = s.ChangePassword(ctx, u.ID, p.SessionID, pw, "ops-password-123")
	p, _, _, _ = s.Session(ctx, tok)
	if p.Restricted() != "mfa" {
		t.Fatalf("2FA setup must be required: %+v", p)
	}
	setup, _ := s.BeginTOTP(ctx, u.ID, "")
	if _, err := s.ConfirmTOTP(ctx, u.ID, totpNow(t, setup, 0)); err != nil {
		t.Fatal(err)
	}
	p, _, _, _ = s.Session(ctx, tok)
	if p.Restricted() != "" {
		t.Fatalf("restricted after setup: %+v", p)
	}
	if err := s.DisableTOTP(ctx, u.ID, "ops-password-123"); err == nil {
		t.Fatal("required second factor switched off")
	}
	// an administrator resets it (lost phone): sessions end, setup is required again
	if err := s.ResetMFA(ctx, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Session(ctx, tok); err == nil {
		t.Fatal("session survived the reset")
	}
	res, err := s.Login(ctx, "ops", "ops-password-123", "", "1.1.1.1", "")
	if err != nil || res.Token == "" {
		t.Fatalf("login after reset: %+v %v", res, err)
	}
	p, _, _, _ = s.Session(ctx, res.Token)
	if p.Restricted() != "mfa" {
		t.Fatalf("setup must be required again: %+v", p)
	}
}
