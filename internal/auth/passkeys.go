package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// ErrPasskeysUnavailable is returned when a request cannot use passkeys.
var ErrPasskeysUnavailable = errors.New("Passkeys funktionieren nur über HTTPS mit einem Hostnamen (nicht über eine IP-Adresse) oder auf localhost")

// RP identifies the relying party of a request: the host name passkeys are bound to and
// the origin the browser reports. An empty ID means passkeys cannot be used.
type RP struct {
	ID     string
	Origin string
}

func (rp RP) webauthn() (*webauthn.WebAuthn, error) {
	if rp.ID == "" {
		return nil, ErrPasskeysUnavailable
	}
	return webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: "NetScope",
		RPOrigins:     []string{rp.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementPreferred,
			UserVerification: protocol.VerificationPreferred,
		},
	})
}

// Passkey is a registered passkey without its key material.
type Passkey struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	RPID       string     `json:"rpId"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// waUser adapts a user to the webauthn library.
type waUser struct {
	id            []byte
	name, display string
	creds         []webauthn.Credential
	rows          []int64 // user_passkeys.id per credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.display }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.creds }

// registration is a passkey registration in progress.
type registration struct {
	rpID    string
	expires time.Time
	session []byte // webauthn.SessionData (JSON)
}

// webauthnUser loads a user with its passkeys for rpID; the random user handle is created
// on first use.
func (s *Service) webauthnUser(ctx context.Context, userID int64, rpID string) (*waUser, error) {
	u := &waUser{}
	var display string
	if err := s.db.R.QueryRowContext(ctx, "SELECT username, display_name, webauthn_id FROM users WHERE id = ?", userID).
		Scan(&u.name, &display, &u.id); err != nil {
		return nil, db.NotFound(err)
	}
	u.display = display
	if u.display == "" {
		u.display = u.name
	}
	if len(u.id) == 0 {
		u.id = make([]byte, 32)
		if _, err := rand.Read(u.id); err != nil {
			return nil, err
		}
		if _, err := s.db.W.ExecContext(ctx, "UPDATE users SET webauthn_id = ? WHERE id = ? AND webauthn_id IS NULL", u.id, userID); err != nil {
			return nil, err
		}
		if err := s.db.W.QueryRowContext(ctx, "SELECT webauthn_id FROM users WHERE id = ?", userID).Scan(&u.id); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, data FROM user_passkeys WHERE user_id = ? AND rp_id = ? ORDER BY id", userID, rpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id   int64
			data string
			c    webauthn.Credential
		)
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(data), &c) == nil {
			u.creds = append(u.creds, c)
			u.rows = append(u.rows, id)
		}
	}
	return u, rows.Err()
}

// Passkeys lists the passkeys of a user.
func (s *Service) Passkeys(ctx context.Context, userID int64) ([]Passkey, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, name, rp_id, created_at, last_used_at FROM user_passkeys WHERE user_id = ? ORDER BY id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Passkey{}
	for rows.Next() {
		var (
			p       Passkey
			created int64
			used    sql.NullInt64
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.RPID, &created, &used); err != nil {
			return nil, err
		}
		p.CreatedAt, p.LastUsedAt = db.Time(created), db.NullTime(used)
		out = append(out, p)
	}
	return out, rows.Err()
}

// BeginPasskeyRegistration starts registering a passkey for the signed-in user.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, userID int64, rp RP) (*protocol.CredentialCreation, error) {
	wa, err := rp.webauthn()
	if err != nil {
		return nil, err
	}
	u, err := s.webauthnUser(ctx, userID, rp.ID)
	if err != nil {
		return nil, err
	}
	creation, session, err := wa.BeginRegistration(u, webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.registrations[userID] = &registration{rpID: rp.ID, expires: time.Now().Add(challengeTTL), session: data}
	s.mu.Unlock()
	return creation, nil
}

func passkeyName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Passkey"
	}
	if len([]rune(name)) > 64 {
		return "", plugin.FieldErr("name", "höchstens 64 Zeichen")
	}
	return name, nil
}

// FinishPasskeyRegistration stores the passkey the browser created. It returns new
// recovery codes if this is the user's first second factor.
func (s *Service) FinishPasskeyRegistration(ctx context.Context, userID int64, rp RP, name string, raw []byte) (*Passkey, []string, error) {
	name, err := passkeyName(name)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	reg := s.registrations[userID]
	delete(s.registrations, userID)
	s.mu.Unlock()
	if reg == nil || time.Now().After(reg.expires) || reg.rpID != rp.ID {
		return nil, nil, errors.New("Die Registrierung ist abgelaufen – bitte neu beginnen")
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(reg.session, &session); err != nil {
		return nil, nil, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("Antwort des Browsers ungültig: %w", err)
	}
	wa, err := rp.webauthn()
	if err != nil {
		return nil, nil, err
	}
	u, err := s.webauthnUser(ctx, userID, rp.ID)
	if err != nil {
		return nil, nil, err
	}
	cred, err := wa.CreateCredential(u, session, parsed)
	if err != nil {
		return nil, nil, fmt.Errorf("Passkey konnte nicht registriert werden: %w", err)
	}
	data, err := json.Marshal(cred)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	pk := &Passkey{Name: name, RPID: rp.ID, CreatedAt: now}
	var codes []string
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO user_passkeys(user_id, name, credential_id, rp_id, data, created_at) VALUES (?,?,?,?,?,?)`,
			userID, name, cred.ID, rp.ID, string(data), now.UnixMilli())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return errors.New("Dieser Passkey ist bereits registriert")
			}
			return err
		}
		pk.ID, _ = res.LastInsertId()
		codes, err = ensureRecoveryCodes(ctx, tx, userID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return pk, codes, nil
}

// RenamePasskey renames a passkey of the user.
func (s *Service) RenamePasskey(ctx context.Context, userID, id int64, name string) error {
	name, err := passkeyName(name)
	if err != nil {
		return err
	}
	res, err := s.db.W.ExecContext(ctx, "UPDATE user_passkeys SET name = ? WHERE id = ? AND user_id = ?", name, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// DeletePasskey removes a passkey of the user. A role that requires a second factor keeps
// the last one.
func (s *Service) DeletePasskey(ctx context.Context, userID, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			required, totp bool
			passkeys       int
		)
		if err := tx.QueryRowContext(ctx, `SELECT r.require_2fa, u.totp_enabled_at IS NOT NULL,
			(SELECT COUNT(*) FROM user_passkeys k WHERE k.user_id = u.id)
			FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = ?`, userID).Scan(&required, &totp, &passkeys); err != nil {
			return db.NotFound(err)
		}
		if required && !totp && passkeys <= 1 {
			return errors.New("Deine Rolle verlangt einen zweiten Faktor – richte zuerst TOTP oder einen weiteren Passkey ein")
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM user_passkeys WHERE id = ? AND user_id = ?", id, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		return dropUnusedRecovery(ctx, tx, userID)
	})
}

// BeginPasskeyLogin starts the passkey step of a login.
func (s *Service) BeginPasskeyLogin(ctx context.Context, challengeID string, rp RP) (*protocol.CredentialAssertion, error) {
	c, err := s.challenge(challengeID)
	if err != nil {
		return nil, err
	}
	if c.rpID == "" || c.rpID != rp.ID {
		return nil, ErrPasskeysUnavailable
	}
	wa, err := rp.webauthn()
	if err != nil {
		return nil, err
	}
	u, err := s.webauthnUser(ctx, c.userID, rp.ID)
	if err != nil {
		return nil, err
	}
	if len(u.creds) == 0 {
		return nil, errors.New("Für diese Adresse ist kein Passkey registriert")
	}
	assertion, session, err := wa.BeginLogin(u)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if ch, ok := s.challenges[challengeID]; ok {
		ch.passkey = data
	}
	s.mu.Unlock()
	return assertion, nil
}

// FinishPasskeyLogin completes a login with the assertion the browser returned.
func (s *Service) FinishPasskeyLogin(ctx context.Context, challengeID string, rp RP, raw []byte, ip, userAgent string) (*LoginResult, error) {
	c, err := s.challenge(challengeID)
	if err != nil {
		return nil, err
	}
	if len(c.passkey) == 0 || c.rpID != rp.ID {
		return nil, ErrChallengeExpired
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(c.passkey, &session); err != nil {
		return nil, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(raw)
	if err != nil {
		return nil, s.failChallenge(challengeID, c.userID, ip)
	}
	wa, err := rp.webauthn()
	if err != nil {
		return nil, err
	}
	u, err := s.webauthnUser(ctx, c.userID, rp.ID)
	if err != nil {
		return nil, err
	}
	cred, err := wa.ValidateLogin(u, session, parsed)
	if err != nil || cred.Authenticator.CloneWarning {
		return nil, s.failChallenge(challengeID, c.userID, ip)
	}
	// keep the signature counter and flags current
	if data, err := json.Marshal(cred); err == nil {
		for i, known := range u.creds {
			if bytes.Equal(known.ID, cred.ID) {
				_, _ = s.db.W.ExecContext(ctx, "UPDATE user_passkeys SET data = ?, last_used_at = ? WHERE id = ?", string(data), db.Now(), u.rows[i])
			}
		}
	}
	return s.completeChallenge(ctx, challengeID, c.userID, ip, userAgent)
}
