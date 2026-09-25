package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // TOTP (RFC 6238) uses HMAC-SHA1; every authenticator app expects it
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

const (
	totpPeriod        = 30
	totpSkew          = 1 // accepted steps before and after the current one (clock drift)
	recoveryCodeCount = 10
	// challengeTTL is how long a login waits for its second factor.
	challengeTTL         = 5 * time.Minute
	maxChallengeFailures = 5
)

var (
	ErrChallengeExpired = errors.New("Die Anmeldung ist abgelaufen – bitte erneut mit dem Passwort anmelden")
	ErrInvalidCode      = errors.New("Der Code ist falsch oder bereits verwendet")
	errNoVault          = errors.New("ohne Vault kann kein zweiter Faktor eingerichtet oder geprüft werden")
)

// MFAStatus is the second-factor state of the signed-in user.
type MFAStatus struct {
	TOTP          bool      `json:"totp"`
	RecoveryCodes int       `json:"recoveryCodes"` // unused
	Passkeys      []Passkey `json:"passkeys"`
	// Required: the role requires a second factor (it cannot be switched off completely).
	Required bool `json:"required"`
}

// MFA returns the second-factor state of a user.
func (s *Service) MFA(ctx context.Context, userID int64) (*MFAStatus, error) {
	st := &MFAStatus{}
	err := s.db.R.QueryRowContext(ctx, `SELECT u.totp_enabled_at IS NOT NULL, r.require_2fa,
		(SELECT COUNT(*) FROM user_recovery_codes c WHERE c.user_id = u.id AND c.used_at IS NULL)
		FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = ?`, userID).Scan(&st.TOTP, &st.Required, &st.RecoveryCodes)
	if err != nil {
		return nil, db.NotFound(err)
	}
	if st.Passkeys, err = s.Passkeys(ctx, userID); err != nil {
		return nil, err
	}
	return st, nil
}

// ---------------------------------------------------------------- TOTP

// totpAt computes the 6-digit code of a time step (RFC 6238, HMAC-SHA1).
func totpAt(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1000000)
}

func digitsOnly(code string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' {
			return -1
		}
		return 'x'
	}, code)
}

// TOTPSetup is a TOTP setup in progress.
type TOTPSetup struct {
	Secret string `json:"secret"` // base32, for manual entry
	URI    string `json:"uri"`    // otpauth:// URI
	QR     string `json:"qr"`     // data: URI of an SVG QR code
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// BeginTOTP creates a new secret for the user. It becomes active with ConfirmTOTP; an
// active TOTP stays valid until then. host names the instance in the authenticator app.
func (s *Service) BeginTOTP(ctx context.Context, userID int64, host string) (*TOTPSetup, error) {
	if s.vault == nil {
		return nil, errNoVault
	}
	var username string
	if err := s.db.R.QueryRowContext(ctx, "SELECT username FROM users WHERE id = ?", userID).Scan(&username); err != nil {
		return nil, db.NotFound(err)
	}
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	sealed, err := s.vault.Encrypt(secret)
	if err != nil {
		return nil, err
	}
	if _, err := s.db.W.ExecContext(ctx, "UPDATE users SET totp_pending = ?, updated_at = ? WHERE id = ?", sealed, db.Now(), userID); err != nil {
		return nil, err
	}
	account := username
	if host != "" {
		account += "@" + host
	}
	enc := b32.EncodeToString(secret)
	q := url.Values{"secret": {enc}, "issuer": {"NetScope"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {strconv.Itoa(totpPeriod)}}
	uri := "otpauth://totp/" + url.PathEscape("NetScope:"+account) + "?" + q.Encode()
	img, err := qrDataURI(uri)
	if err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: enc, URI: uri, QR: img}, nil
}

// ConfirmTOTP activates the pending secret when code matches it. It returns new recovery
// codes if the user had none (first second factor).
func (s *Service) ConfirmTOTP(ctx context.Context, userID int64, code string) ([]string, error) {
	if s.vault == nil {
		return nil, errNoVault
	}
	var pending []byte
	if err := s.db.R.QueryRowContext(ctx, "SELECT totp_pending FROM users WHERE id = ?", userID).Scan(&pending); err != nil {
		return nil, db.NotFound(err)
	}
	if len(pending) == 0 {
		return nil, plugin.FieldErr("code", "Keine Einrichtung offen – bitte neu beginnen")
	}
	secret, err := s.vault.Decrypt(pending)
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(secret, code, 0)
	if !ok {
		return nil, plugin.FieldErr("code", "Der Code passt nicht – Uhrzeit des Geräts prüfen und den aktuellen Code eingeben")
	}
	var codes []string
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		now := db.Now()
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = totp_pending, totp_pending = NULL, totp_enabled_at = ?,
			totp_last_step = ?, updated_at = ? WHERE id = ?`, now, step, now, userID); err != nil {
			return err
		}
		codes, err = ensureRecoveryCodes(ctx, tx, userID)
		return err
	})
	return codes, err
}

// matchTOTP returns the time step code belongs to (within the allowed drift, after last).
func matchTOTP(secret []byte, code string, last int64) (int64, bool) {
	code = digitsOnly(code)
	if len(code) != 6 {
		return 0, false
	}
	now := time.Now().Unix() / totpPeriod
	for d := int64(-totpSkew); d <= totpSkew; d++ {
		step := now + d
		if step > last && subtle.ConstantTimeCompare([]byte(totpAt(secret, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// checkTOTP verifies a code of the active secret; each code is accepted only once.
func (s *Service) checkTOTP(ctx context.Context, userID int64, code string) (bool, error) {
	if s.vault == nil {
		return false, errNoVault
	}
	var (
		sealed []byte
		last   int64
	)
	if err := s.db.R.QueryRowContext(ctx, "SELECT totp_secret, totp_last_step FROM users WHERE id = ?", userID).Scan(&sealed, &last); err != nil {
		return false, db.NotFound(err)
	}
	if len(sealed) == 0 {
		return false, nil
	}
	secret, err := s.vault.Decrypt(sealed)
	if err != nil {
		return false, err
	}
	step, ok := matchTOTP(secret, code, last)
	if !ok {
		return false, nil
	}
	res, err := s.db.W.ExecContext(ctx, "UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?", step, userID, step)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// DisableTOTP switches TOTP off after checking the password. A role that requires a second
// factor keeps it unless a passkey remains.
func (s *Service) DisableTOTP(ctx context.Context, userID int64, password string) error {
	if err := s.VerifyPassword(ctx, userID, password); err != nil {
		return plugin.FieldErr("password", "Passwort ist falsch")
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var required bool
		var passkeys int
		if err := tx.QueryRowContext(ctx, `SELECT r.require_2fa, (SELECT COUNT(*) FROM user_passkeys k WHERE k.user_id = u.id)
			FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = ?`, userID).Scan(&required, &passkeys); err != nil {
			return db.NotFound(err)
		}
		if required && passkeys == 0 {
			return errors.New("Deine Rolle verlangt einen zweiten Faktor – richte zuerst einen Passkey ein")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_pending = NULL, totp_enabled_at = NULL,
			updated_at = ? WHERE id = ?`, db.Now(), userID); err != nil {
			return err
		}
		return dropUnusedRecovery(ctx, tx, userID)
	})
}

// qrDataURI renders text as a QR code (SVG data: URI).
func qrDataURI(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const quiet = 4
	n := c.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	b.WriteString(`<rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="`)
	for y := 0; y < c.Size; y++ {
		for x := 0; x < c.Size; x++ {
			if c.Black(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(b.String())), nil
}

// ---------------------------------------------------------------- recovery codes

func normalizeRecovery(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(code)))
}

func newRecoveryCodes() ([]string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	out := make([]string, recoveryCodeCount)
	for i := range out {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		for j := range b {
			b[j] = alphabet[int(b[j])%len(alphabet)]
		}
		out[i] = string(b[:5]) + "-" + string(b[5:])
	}
	return out, nil
}

// replaceRecoveryCodes stores new codes and invalidates the old ones.
func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	codes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_recovery_codes WHERE user_id = ?", userID); err != nil {
		return nil, err
	}
	for _, c := range codes {
		if _, err := tx.ExecContext(ctx, "INSERT INTO user_recovery_codes(user_id, code_hash) VALUES (?, ?)", userID, hashToken(normalizeRecovery(c))); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// ensureRecoveryCodes creates codes when the user has no unused one (first second factor).
func ensureRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64) ([]string, error) {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL", userID).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, nil
	}
	return replaceRecoveryCodes(ctx, tx, userID)
}

// dropUnusedRecovery removes the recovery codes once no second factor is left.
func dropUnusedRecovery(ctx context.Context, tx *sql.Tx, userID int64) error {
	var left bool
	if err := tx.QueryRowContext(ctx, `SELECT totp_enabled_at IS NOT NULL OR EXISTS (SELECT 1 FROM user_passkeys WHERE user_id = ?)
		FROM users WHERE id = ?`, userID, userID).Scan(&left); err != nil {
		return err
	}
	if left {
		return nil
	}
	_, err := tx.ExecContext(ctx, "DELETE FROM user_recovery_codes WHERE user_id = ?", userID)
	return err
}

// RegenerateRecoveryCodes replaces the recovery codes after checking the password.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, userID int64, password string) ([]string, error) {
	if err := s.VerifyPassword(ctx, userID, password); err != nil {
		return nil, plugin.FieldErr("password", "Passwort ist falsch")
	}
	var codes []string
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var mfa bool
		if err := tx.QueryRowContext(ctx, `SELECT totp_enabled_at IS NOT NULL OR EXISTS (SELECT 1 FROM user_passkeys WHERE user_id = ?)
			FROM users WHERE id = ?`, userID, userID).Scan(&mfa); err != nil {
			return db.NotFound(err)
		}
		if !mfa {
			return errors.New("Wiederherstellungscodes gibt es erst mit einem zweiten Faktor")
		}
		var err error
		codes, err = replaceRecoveryCodes(ctx, tx, userID)
		return err
	})
	return codes, err
}

func (s *Service) useRecoveryCode(ctx context.Context, userID int64, code string) (bool, error) {
	res, err := s.db.W.ExecContext(ctx, "UPDATE user_recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL",
		db.Now(), userID, hashToken(normalizeRecovery(code)))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// ---------------------------------------------------------------- login challenges

// challenge is a login waiting for its second factor.
type challenge struct {
	userID   int64
	rpID     string
	expires  time.Time
	failures int
	passkey  []byte // webauthn session data (JSON) of a started passkey login
}

func (s *Service) newChallenge(userID int64, rpID string) (string, error) {
	id, err := randomString(24)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, c := range s.challenges {
		if now.After(c.expires) {
			delete(s.challenges, k)
		}
	}
	s.challenges[id] = &challenge{userID: userID, rpID: rpID, expires: now.Add(challengeTTL)}
	return id, nil
}

// challenge returns a copy of an open challenge.
func (s *Service) challenge(id string) (challenge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.challenges[id]
	if !ok || time.Now().After(c.expires) {
		delete(s.challenges, id)
		return challenge{}, ErrChallengeExpired
	}
	if !s.mfaLimiter.allow(strconv.FormatInt(c.userID, 10)) {
		return challenge{}, ErrRateLimited
	}
	return *c, nil
}

// failChallenge counts a wrong second factor; after maxChallengeFailures the password has
// to be entered again.
func (s *Service) failChallenge(id string, userID int64, ip string) error {
	s.limiter.fail(ip)
	s.mfaLimiter.fail(strconv.FormatInt(userID, 10))
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.challenges[id]; ok {
		c.failures++
		if c.failures >= maxChallengeFailures {
			delete(s.challenges, id)
			return ErrChallengeExpired
		}
	}
	return ErrInvalidCode
}

// completeChallenge ends a challenge with a session.
func (s *Service) completeChallenge(ctx context.Context, id string, userID int64, ip, userAgent string) (*LoginResult, error) {
	s.mu.Lock()
	delete(s.challenges, id)
	s.mu.Unlock()
	s.mfaLimiter.success(strconv.FormatInt(userID, 10))
	return s.startSession(ctx, userID, ip, userAgent)
}

// LoginTOTP completes a login with a TOTP code.
func (s *Service) LoginTOTP(ctx context.Context, challengeID, code, ip, userAgent string) (*LoginResult, error) {
	c, err := s.challenge(challengeID)
	if err != nil {
		return nil, err
	}
	ok, err := s.checkTOTP(ctx, c.userID, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.failChallenge(challengeID, c.userID, ip)
	}
	return s.completeChallenge(ctx, challengeID, c.userID, ip, userAgent)
}

// LoginRecovery completes a login with a recovery code (each code works once).
func (s *Service) LoginRecovery(ctx context.Context, challengeID, code, ip, userAgent string) (*LoginResult, error) {
	c, err := s.challenge(challengeID)
	if err != nil {
		return nil, err
	}
	ok, err := s.useRecoveryCode(ctx, c.userID, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.failChallenge(challengeID, c.userID, ip)
	}
	return s.completeChallenge(ctx, challengeID, c.userID, ip, userAgent)
}
