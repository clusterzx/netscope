package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/db"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d)
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
	if _, _, err := s.Login(ctx, "admin", "wrong", "1.1.1.1", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
	tok, _, err := s.Login(ctx, "admin", gen, "1.1.1.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	p, _, _, err := s.Session(ctx, tok)
	if err != nil || p.Username != "admin" || !p.CanWrite() {
		t.Fatalf("session: %+v %v", p, err)
	}
	tok2, _, _ := s.Login(ctx, "admin", gen, "1.1.1.1", "other")
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
	if err != nil || p.CanWrite() || p.TokenID != tok.ID {
		t.Fatalf("token principal %+v %v", p, err)
	}
	if _, err := s.TokenPrincipal(ctx, plain+"x", ""); err == nil {
		t.Fatal("modified token accepted")
	}
	past := time.Now().Add(time.Hour)
	plain2, _, err := s.CreateToken(ctx, uid, "rw", ScopeWrite, &past)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := s.TokenPrincipal(ctx, plain2, ""); err != nil || !p.CanWrite() {
		t.Fatal("write token")
	}
	if err := s.DeleteToken(ctx, tok.ID); err != nil {
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
		_, _, _ = s.Login(ctx, "admin", "bad", "9.9.9.9", "")
	}
	if _, _, err := s.Login(ctx, "admin", "correct-horse-battery", "9.9.9.9", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected rate limit, got %v", err)
	}
	if _, _, err := s.Login(ctx, "admin", "correct-horse-battery", "8.8.8.8", ""); err != nil {
		t.Fatalf("other ip blocked: %v", err)
	}
}
