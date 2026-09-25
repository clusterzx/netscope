// Package vault encrypts secrets at rest with AES-256-GCM. The master key comes from
// NETSCOPE_MASTER_KEY or a key file. Secrets are only ever decrypted for plugins; the API
// references credentials by id and never returns secret values.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"netscope/internal/db"
)

const (
	keySize      = 32
	blobVersion  = 1
	checkPlain   = "netscope-vault-check-v1"
	aad          = "netscope-vault-v1"
	pendingSufix = ".new"
)

// ErrWrongKey is returned when the master key does not match the database.
var ErrWrongKey = errors.New("vault: master key passt nicht zur Datenbank")

// Key is a 256-bit master key.
type Key []byte

// ID returns a short fingerprint of the key.
func (k Key) ID() string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:6])
}

// Encode returns the base64 representation stored in key files.
func (k Key) Encode() string { return base64.StdEncoding.EncodeToString(k) }

// NewKey generates a random key.
func NewKey() (Key, error) {
	k := make([]byte, keySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return nil, err
	}
	return k, nil
}

// ParseKey accepts base64 (standard or URL alphabet, with or without padding) or 64 hex chars.
func ParseKey(s string) (Key, error) {
	s = strings.TrimSpace(s)
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			if len(b) != keySize {
				return nil, fmt.Errorf("master key muss %d Byte lang sein (ist %d)", keySize, len(b))
			}
			return b, nil
		}
	}
	return nil, errors.New("master key: weder Base64 noch Hex")
}

// KeySource describes where the key came from.
type KeySource struct {
	FromEnv   bool
	File      string
	Generated bool
}

// LoadKey returns the master key from env (if set) or file (generated if missing).
func LoadKey(envValue, file string) (Key, KeySource, error) {
	if envValue != "" {
		k, err := ParseKey(envValue)
		return k, KeySource{FromEnv: true}, err
	}
	b, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		k, err := NewKey()
		if err != nil {
			return nil, KeySource{}, err
		}
		if err := writeKeyFile(file, k); err != nil {
			return nil, KeySource{}, err
		}
		return k, KeySource{File: file, Generated: true}, nil
	}
	if err != nil {
		return nil, KeySource{}, fmt.Errorf("master key file: %w", err)
	}
	k, err := ParseKey(string(b))
	return k, KeySource{File: file}, err
}

func writeKeyFile(path string, k Key) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(k.Encode()+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Vault encrypts and decrypts with the active master key.
type Vault struct {
	db  *db.DB
	mu  sync.RWMutex
	key Key
	src KeySource
}

// Open binds the key to the database. On first use the key's check value is stored; later
// opens verify it. If a key rotation was interrupted, the pending key file is recovered.
func Open(ctx context.Context, d *db.DB, key Key, src KeySource) (*Vault, error) {
	v := &Vault{db: d, key: key, src: src}
	var check []byte
	var keyID string
	err := d.W.QueryRowContext(ctx, "SELECT key_id, check_value FROM vault_meta WHERE id = 1").Scan(&keyID, &check)
	if errors.Is(err, sql.ErrNoRows) {
		c, err := v.Encrypt([]byte(checkPlain))
		if err != nil {
			return nil, err
		}
		_, err = d.W.ExecContext(ctx, "INSERT INTO vault_meta(id, key_id, check_value, created_at) VALUES (1, ?, ?, ?)",
			key.ID(), c, db.Now())
		return v, err
	}
	if err != nil {
		return nil, err
	}
	if v.verify(check) {
		return v, nil
	}
	// Recover from an interrupted rotation: the new key was written to <file>.new and the
	// database already re-encrypted with it.
	if src.File != "" {
		if b, err := os.ReadFile(src.File + pendingSufix); err == nil {
			if nk, err := ParseKey(string(b)); err == nil {
				v.key = nk
				if v.verify(check) {
					if err := os.Rename(src.File+pendingSufix, src.File); err != nil {
						return nil, fmt.Errorf("vault: pending key recovery: %w", err)
					}
					return v, nil
				}
			}
		}
	}
	return nil, ErrWrongKey
}

func (v *Vault) verify(check []byte) bool {
	p, err := v.Decrypt(check)
	return err == nil && string(p) == checkPlain
}

// KeyID returns the fingerprint of the active key.
func (v *Vault) KeyID() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.key.ID()
}

// Source returns where the key came from.
func (v *Vault) Source() KeySource { return v.src }

func encryptWith(key Key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+gcm.Overhead())
	out = append(out, blobVersion)
	out = append(out, nonce...)
	return gcm.Seal(out, nonce, plaintext, []byte(aad)), nil
}

func decryptWith(key Key, blob []byte) ([]byte, error) {
	if len(blob) < 1 || blob[0] != blobVersion {
		return nil, errors.New("vault: unbekanntes Blob-Format")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < 1+gcm.NonceSize()+gcm.Overhead() {
		return nil, errors.New("vault: Blob zu kurz")
	}
	nonce := blob[1 : 1+gcm.NonceSize()]
	p, err := gcm.Open(nil, nonce, blob[1+gcm.NonceSize():], []byte(aad))
	if err != nil {
		return nil, errors.New("vault: Entschlüsselung fehlgeschlagen")
	}
	return p, nil
}

// Encrypt encrypts plaintext with the active key: version(1) | nonce(12) | ciphertext+tag.
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return encryptWith(v.key, plaintext)
}

// Decrypt decrypts a blob produced by Encrypt.
func (v *Vault) Decrypt(blob []byte) ([]byte, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return decryptWith(v.key, blob)
}

// SealString encrypts a secret for a setting "<name>.secret" (base64 text, re-encrypted
// by Rotate).
func (v *Vault) SealString(plain string) (string, error) {
	blob, err := v.Encrypt([]byte(plain))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(blob), nil
}

// OpenString decrypts a value produced by SealString.
func (v *Vault) OpenString(sealed string) (string, error) {
	blob, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	p, err := v.Decrypt(blob)
	return string(p), err
}

// Rotate re-encrypts every secret with newKey in one transaction. With a file-based key
// the new key is written to <file>.new before the transaction and moved into place
// afterwards, so a crash in between is recovered by Open. With an env-based key the
// caller must update NETSCOPE_MASTER_KEY before the next start.
func (v *Vault) Rotate(ctx context.Context, newKey Key) error {
	if len(newKey) != keySize {
		return fmt.Errorf("neuer Schlüssel muss %d Byte lang sein", keySize)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	old := v.key
	if v.src.File != "" && !v.src.FromEnv {
		if err := writeKeyFile(v.src.File+pendingSufix, newKey); err != nil {
			return fmt.Errorf("neuen Schlüssel schreiben: %w", err)
		}
	}
	reencrypt := func(blob []byte) ([]byte, error) {
		if len(blob) == 0 {
			return blob, nil
		}
		p, err := decryptWith(old, blob)
		if err != nil {
			return nil, err
		}
		return encryptWith(newKey, p)
	}
	err := v.db.Tx(ctx, func(tx *sql.Tx) error {
		type row struct {
			id   any
			blob []byte
		}
		load := func(q string) ([]row, error) {
			rows, err := tx.QueryContext(ctx, q)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var out []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.id, &r.blob); err != nil {
					return nil, err
				}
				out = append(out, r)
			}
			return out, rows.Err()
		}
		creds, err := load("SELECT id, secret FROM credentials WHERE secret IS NOT NULL")
		if err != nil {
			return err
		}
		for _, r := range creds {
			nb, err := reencrypt(r.blob)
			if err != nil {
				return fmt.Errorf("credential %v: %w", r.id, err)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE credentials SET secret = ?, key_id = ? WHERE id = ?", nb, newKey.ID(), r.id); err != nil {
				return err
			}
		}
		cfgs, err := load("SELECT plugin_id, secrets FROM plugin_configs WHERE secrets IS NOT NULL")
		if err != nil {
			return err
		}
		for _, r := range cfgs {
			nb, err := reencrypt(r.blob)
			if err != nil {
				return fmt.Errorf("plugin %v: %w", r.id, err)
			}
			if _, err := tx.ExecContext(ctx, "UPDATE plugin_configs SET secrets = ?, secrets_key_id = ? WHERE plugin_id = ?", nb, newKey.ID(), r.id); err != nil {
				return err
			}
		}
		// TOTP secrets of the users (active and in setup)
		for _, col := range []string{"totp_secret", "totp_pending"} {
			users, err := load("SELECT id, " + col + " FROM users WHERE " + col + " IS NOT NULL")
			if err != nil {
				return err
			}
			for _, r := range users {
				nb, err := reencrypt(r.blob)
				if err != nil {
					return fmt.Errorf("user %v: %w", r.id, err)
				}
				if _, err := tx.ExecContext(ctx, "UPDATE users SET "+col+" = ? WHERE id = ?", nb, r.id); err != nil {
					return err
				}
			}
		}
		// settings "<name>.secret" hold a JSON string with a base64 blob (e.g. the token a
		// site uses at its central instance)
		sets, err := load("SELECT key, value FROM settings WHERE key LIKE '%.secret'")
		if err != nil {
			return err
		}
		for _, r := range sets {
			var enc string
			if err := json.Unmarshal(r.blob, &enc); err != nil || enc == "" {
				continue
			}
			blob, err := base64.StdEncoding.DecodeString(enc)
			if err != nil {
				return fmt.Errorf("setting %v: %w", r.id, err)
			}
			nb, err := reencrypt(blob)
			if err != nil {
				return fmt.Errorf("setting %v: %w", r.id, err)
			}
			val, _ := json.Marshal(base64.StdEncoding.EncodeToString(nb))
			if _, err := tx.ExecContext(ctx, "UPDATE settings SET value = ?, updated_at = ? WHERE key = ?", string(val), db.Now(), r.id); err != nil {
				return err
			}
		}
		check, err := encryptWith(newKey, []byte(checkPlain))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE vault_meta SET key_id = ?, check_value = ?, rotated_at = ? WHERE id = 1",
			newKey.ID(), check, db.Now())
		return err
	})
	if err != nil {
		if v.src.File != "" && !v.src.FromEnv {
			_ = os.Remove(v.src.File + pendingSufix)
		}
		return err
	}
	v.key = newKey
	if v.src.File != "" && !v.src.FromEnv {
		if err := os.Rename(v.src.File+pendingSufix, v.src.File); err != nil {
			return fmt.Errorf("Schlüsseldatei ersetzen: %w (neuer Schlüssel liegt in %s%s)", err, v.src.File, pendingSufix)
		}
	}
	return nil
}
