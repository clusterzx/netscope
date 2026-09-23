package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

func openDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestEncryptRoundtrip(t *testing.T) {
	d := openDB(t)
	k, _ := NewKey()
	v, err := Open(context.Background(), d, k, KeySource{})
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range [][]byte{{}, []byte("x"), bytes.Repeat([]byte("secret"), 1000)} {
		c, err := v.Encrypt(msg)
		if err != nil {
			t.Fatal(err)
		}
		// a 1-byte plaintext occurs in ~30 random ciphertext bytes by chance (~11 %);
		// from 8 bytes on a random match is negligible
		if len(msg) >= 8 && bytes.Contains(c, msg) {
			t.Fatal("ciphertext contains plaintext")
		}
		p, err := v.Decrypt(c)
		if err != nil || !bytes.Equal(p, msg) {
			t.Fatalf("roundtrip failed: %v", err)
		}
		c[len(c)-1] ^= 0xff
		if _, err := v.Decrypt(c); err == nil {
			t.Fatal("tampered ciphertext accepted")
		}
	}
	// same plaintext encrypts differently (random nonce)
	a, _ := v.Encrypt([]byte("same"))
	b, _ := v.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("nonce reuse")
	}
}

func TestWrongKeyRejected(t *testing.T) {
	d := openDB(t)
	k1, _ := NewKey()
	if _, err := Open(context.Background(), d, k1, KeySource{}); err != nil {
		t.Fatal(err)
	}
	k2, _ := NewKey()
	if _, err := Open(context.Background(), d, k2, KeySource{}); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("expected ErrWrongKey, got %v", err)
	}
}

func TestCredentialsAndRotation(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	keyFile := filepath.Join(t.TempDir(), "master.key")
	k, src, err := LoadKey("", keyFile)
	if err != nil || !src.Generated {
		t.Fatalf("LoadKey: %v %+v", err, src)
	}
	v, err := Open(ctx, d, k, src)
	if err != nil {
		t.Fatal(err)
	}
	id, err := v.CreateCredential(ctx, CredentialInput{Name: "lxc", Type: plugin.CredSSH,
		Values: map[string]any{"username": "root", "password": "hunter2"}})
	if err != nil {
		t.Fatal(err)
	}
	// secrets are never part of the meta view
	m, err := v.Meta(ctx, id)
	if err != nil || m.Public["username"] != "root" || len(m.SecretsSet) != 1 || m.SecretsSet[0] != "password" {
		t.Fatalf("meta %+v %v", m, err)
	}
	var raw []byte
	_ = d.R.QueryRow("SELECT secret FROM credentials WHERE id=?", id).Scan(&raw)
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("secret stored in plaintext")
	}
	// masked update keeps the password
	if err := v.UpdateCredential(ctx, id, CredentialInput{Name: "lxc", Values: map[string]any{"username": "admin", "password": plugin.SecretMask}}); err != nil {
		t.Fatal(err)
	}
	c, err := v.Get(ctx, id)
	if err != nil || c.Get("password") != "hunter2" || c.Get("username") != "admin" {
		t.Fatalf("get %+v %v", c, err)
	}
	// plugin config secrets are rotated too
	blob, _ := v.Encrypt([]byte(`{"token":"abc"}`))
	if _, err := d.W.Exec("INSERT INTO plugin_configs(plugin_id, enabled, timeout_s, secrets, updated_at) VALUES ('x', 1, 60, ?, 0)", blob); err != nil {
		t.Fatal(err)
	}

	nk, _ := NewKey()
	if err := v.Rotate(ctx, nk); err != nil {
		t.Fatal(err)
	}
	c, err = v.Get(ctx, id)
	if err != nil || c.Get("password") != "hunter2" {
		t.Fatalf("after rotation: %v", err)
	}
	var pb []byte
	_ = d.R.QueryRow("SELECT secrets FROM plugin_configs WHERE plugin_id='x'").Scan(&pb)
	if p, err := v.Decrypt(pb); err != nil || string(p) != `{"token":"abc"}` {
		t.Fatalf("plugin secret after rotation: %v", err)
	}
	// key file was replaced; the old key no longer opens the vault
	if _, err := Open(ctx, d, k, KeySource{}); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("old key still valid: %v", err)
	}
	k3, src3, err := LoadKey("", keyFile)
	if err != nil || !bytes.Equal(k3, nk) {
		t.Fatalf("key file not updated: %v", err)
	}
	if _, err := Open(ctx, d, k3, src3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keyFile + ".new"); !os.IsNotExist(err) {
		t.Fatal("pending key file left behind")
	}
}

func TestInterruptedRotationRecovery(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	keyFile := filepath.Join(t.TempDir(), "master.key")
	k, src, _ := LoadKey("", keyFile)
	v, err := Open(ctx, d, k, src)
	if err != nil {
		t.Fatal(err)
	}
	nk, _ := NewKey()
	if err := v.Rotate(ctx, nk); err != nil {
		t.Fatal(err)
	}
	// simulate a crash after the DB commit but before the rename: old key in place,
	// new key still pending
	if err := os.WriteFile(keyFile+".new", []byte(nk.Encode()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, []byte(k.Encode()), 0o600); err != nil {
		t.Fatal(err)
	}
	k2, src2, _ := LoadKey("", keyFile)
	if _, err := Open(ctx, d, k2, src2); err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	k3, _, _ := LoadKey("", keyFile)
	if !bytes.Equal(k3, nk) {
		t.Fatal("pending key not promoted")
	}
}

func TestParseKey(t *testing.T) {
	k, _ := NewKey()
	for _, s := range []string{k.Encode(), " " + k.Encode() + "\n"} {
		got, err := ParseKey(s)
		if err != nil || !bytes.Equal(got, k) {
			t.Fatalf("ParseKey(%q): %v", s, err)
		}
	}
	if _, err := ParseKey("dG9vc2hvcnQ="); err == nil {
		t.Fatal("short key accepted")
	}
}
