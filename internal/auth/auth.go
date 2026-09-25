// Package auth implements the users of NetScope: roles with freely chosen permissions,
// password login with an optional second factor (TOTP, passkeys, recovery codes), session
// cookies and scoped API tokens (read/write, never more than the rights of their user).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/vault"
)

const (
	// SessionTTL is the idle lifetime of a session (sliding).
	SessionTTL = 7 * 24 * time.Hour
	// MinPasswordLength for every password.
	MinPasswordLength = 10
	bcryptCost        = 12
	tokenPrefix       = "ns_"
	adminUser         = "admin"
)

// Scopes.
const (
	ScopeRead  = "read"
	ScopeWrite = "write"
)

var (
	ErrInvalidCredentials = errors.New("Benutzername oder Passwort falsch")
	ErrUnauthenticated    = errors.New("nicht angemeldet")
	ErrRateLimited        = errors.New("zu viele Fehlversuche, bitte später erneut versuchen")
	ErrAccountDisabled    = errors.New("Das Konto ist deaktiviert – wende dich an einen Administrator")
)

// Principal is the authenticated caller.
type Principal struct {
	UserID      int64  `json:"userId"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Kind        string `json:"kind"` // session | token | system
	Scope       string `json:"scope"`
	TokenID     int64  `json:"tokenId,omitempty"`
	TokenName   string `json:"tokenName,omitempty"`
	RoleID      int64  `json:"roleId"`
	RoleName    string `json:"roleName"`
	// Admin is set for the built-in administrator role, which holds every permission.
	Admin bool `json:"admin"`
	// Permissions are the effective permissions (all of them for an administrator).
	Permissions []string `json:"permissions"`
	// PasswordChange: the account was created or reset by an administrator; the session
	// may only change the password.
	PasswordChange bool `json:"passwordChange,omitempty"`
	// MFASetup: the role requires a second factor the user has not set up; the session
	// may only set one up.
	MFASetup  bool   `json:"mfaSetup,omitempty"`
	SessionID string `json:"-"`
	perms     map[string]bool
}

// CanWrite reports whether the principal may modify data at all (read tokens may not).
func (p *Principal) CanWrite() bool { return p != nil && p.Scope == ScopeWrite }

// Has reports whether the principal holds a permission ("" = every signed-in user).
func (p *Principal) Has(perm string) bool {
	switch {
	case p == nil:
		return false
	case perm == "" || p.Kind == "system" || p.Admin:
		return true
	}
	return p.perms[perm]
}

// Restricted returns what a session has to do before it may use NetScope: "password"
// (change the start password) or "mfa" (set up a second factor); "" = nothing.
func (p *Principal) Restricted() string {
	switch {
	case p == nil || p.Kind != "session":
		return ""
	case p.PasswordChange:
		return "password"
	case p.MFASetup:
		return "mfa"
	}
	return ""
}

// Actor returns a name for the audit log.
func (p *Principal) Actor() (name, typ string) {
	if p == nil {
		return "system", "system"
	}
	if p.Kind == "token" {
		return p.Username + " (Token " + p.TokenName + ")", "token"
	}
	if p.Kind == "system" {
		return p.Username, "system"
	}
	return p.Username, "user"
}

// Token is an API token without its secret.
type Token struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"`
	UserID     int64      `json:"userId"`
	Owner      string     `json:"owner"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedIP string     `json:"lastUsedIp,omitempty"`
}

// Service is the auth service.
type Service struct {
	db    *db.DB
	vault *vault.Vault
	// limiter counts failed logins per client address, mfaLimiter failed second factors
	// per user (a correct password does not reset it).
	limiter    *limiter
	mfaLimiter *limiter

	mu            sync.Mutex
	challenges    map[string]*challenge
	registrations map[int64]*registration
}

// New creates the service. The vault encrypts TOTP secrets; without it (command line
// tools) a second factor cannot be set up or checked.
func New(d *db.DB, v *vault.Vault) *Service {
	return &Service{db: d, vault: v, limiter: newLimiter(15*time.Minute, 15*time.Minute),
		mfaLimiter: newLimiter(24*time.Hour, time.Hour), challenges: map[string]*challenge{}, registrations: map[int64]*registration{}}
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GeneratePassword returns a random password, e.g. for the initial admin.
func GeneratePassword() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// EnsureAdmin creates the admin user if no user exists. If password is empty a random
// one is generated and returned so it can be shown once.
func (s *Service) EnsureAdmin(ctx context.Context, password string) (created bool, generated string, err error) {
	var n int
	if err := s.db.W.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
		return false, "", err
	}
	if n > 0 {
		return false, "", nil
	}
	if password == "" {
		if password, err = GeneratePassword(); err != nil {
			return false, "", err
		}
		generated = password
	} else if len(password) < MinPasswordLength {
		return false, "", fmt.Errorf("NETSCOPE_ADMIN_PASSWORD muss mindestens %d Zeichen haben", MinPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return false, "", err
	}
	now := db.Now()
	_, err = s.db.W.ExecContext(ctx, `INSERT INTO users(username, password_hash, role_id, created_at, updated_at)
		VALUES (?, ?, (SELECT id FROM roles WHERE builtin = 'admin'), ?, ?)`, adminUser, string(hash), now, now)
	return err == nil, generated, err
}

// ---------------------------------------------------------------- principals

// account is a user with the rights of its role.
type account struct {
	id                    int64
	username, displayName string
	disabled, mustChange  bool
	roleID                int64
	roleName              string
	perms                 []string
	admin, require2FA     bool
	mfa                   bool // a second factor is set up
}

const accountSelect = `SELECT u.id, u.username, u.display_name, u.disabled, u.must_change_password,
	r.id, r.name, r.permissions, r.builtin = 'admin', r.require_2fa,
	u.totp_enabled_at IS NOT NULL OR EXISTS (SELECT 1 FROM user_passkeys k WHERE k.user_id = u.id)
	FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = ?`

func (s *Service) account(ctx context.Context, userID int64) (*account, error) {
	var (
		a     account
		perms string
	)
	err := s.db.R.QueryRowContext(ctx, accountSelect, userID).Scan(&a.id, &a.username, &a.displayName, &a.disabled, &a.mustChange,
		&a.roleID, &a.roleName, &perms, &a.admin, &a.require2FA, &a.mfa)
	if err != nil {
		return nil, db.NotFound(err)
	}
	a.perms = []string{}
	_ = db.Unmarshal(perms, &a.perms)
	return &a, nil
}

func (a *account) principal(kind, scope string) *Principal {
	perms := a.perms
	if a.admin {
		perms = AllPermissions()
	}
	p := &Principal{UserID: a.id, Username: a.username, DisplayName: a.displayName, Kind: kind, Scope: scope,
		RoleID: a.roleID, RoleName: a.roleName, Admin: a.admin, Permissions: perms, perms: make(map[string]bool, len(perms))}
	for _, k := range perms {
		p.perms[k] = true
	}
	if kind == "session" {
		p.PasswordChange = a.mustChange
		p.MFASetup = a.require2FA && !a.mfa
	}
	return p
}

// ---------------------------------------------------------------- login

// LoginResult is the outcome of a login step: a session (Token) or the request for a
// second factor (Challenge, answered with one of Methods).
type LoginResult struct {
	Token     string
	Expires   time.Time
	Challenge string
	Methods   []string // totp | passkey | recovery
}

// Login verifies the password. Without a second factor it creates the session; with one
// it returns a challenge for the second step. rpID is the host name passkeys are bound to
// on this request ("" = passkeys cannot be used here).
func (s *Service) Login(ctx context.Context, username, password, rpID, ip, userAgent string) (*LoginResult, error) {
	if !s.limiter.allow(ip) {
		return nil, ErrRateLimited
	}
	var (
		id       int64
		hash     string
		disabled bool
		totp     bool
	)
	err := s.db.R.QueryRowContext(ctx, "SELECT id, password_hash, disabled, totp_enabled_at IS NOT NULL FROM users WHERE username = ? COLLATE NOCASE",
		strings.TrimSpace(username)).Scan(&id, &hash, &disabled, &totp)
	if errors.Is(err, sql.ErrNoRows) {
		// constant-ish time: still run bcrypt
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$C6UzMDM.H6dfI/f/IKxGhuE0wYt1yZ0dD6MdH4J8YGAC5xzOO7GvS"), []byte(password))
		s.limiter.fail(ip)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.limiter.fail(ip)
		return nil, ErrInvalidCredentials
	}
	s.limiter.success(ip)
	if disabled {
		return nil, ErrAccountDisabled
	}
	var passkeys, here, codes int
	if err := s.db.R.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM user_passkeys WHERE user_id = ?),
		(SELECT COUNT(*) FROM user_passkeys WHERE user_id = ? AND rp_id = ?),
		(SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = ? AND used_at IS NULL)`,
		id, id, rpID, id).Scan(&passkeys, &here, &codes); err != nil {
		return nil, err
	}
	if !totp && passkeys == 0 {
		return s.startSession(ctx, id, ip, userAgent)
	}
	var methods []string
	if totp {
		methods = append(methods, "totp")
	}
	if here > 0 && rpID != "" {
		methods = append(methods, "passkey")
	}
	if codes > 0 {
		methods = append(methods, "recovery")
	}
	if len(methods) == 0 {
		return nil, errors.New("Für diese Adresse ist kein zweiter Faktor verfügbar: Passkeys gelten nur für den Hostnamen, unter dem sie eingerichtet wurden. Melde dich über diese Adresse an oder lass die 2FA von einem Administrator zurücksetzen")
	}
	ch, err := s.newChallenge(id, rpID)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Challenge: ch, Methods: methods}, nil
}

// startSession creates a session and returns its cookie token.
func (s *Service) startSession(ctx context.Context, userID int64, ip, userAgent string) (*LoginResult, error) {
	token, err := randomString(32)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	exp := now.Add(SessionTTL)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err = s.db.W.ExecContext(ctx, `INSERT INTO sessions(id, user_id, created_at, expires_at, last_seen_at, ip, user_agent)
		VALUES (?,?,?,?,?,?,?)`, hashToken(token), userID, now.UnixMilli(), exp.UnixMilli(), now.UnixMilli(), ip, userAgent)
	if err != nil {
		return nil, err
	}
	_, _ = s.db.W.ExecContext(ctx, "UPDATE users SET last_login_at = ? WHERE id = ?", now.UnixMilli(), userID)
	return &LoginResult{Token: token, Expires: exp}, nil
}

// Session resolves a session cookie. Sessions slide: activity extends them.
// renewed reports whether the cookie expiry should be refreshed.
func (s *Service) Session(ctx context.Context, token string) (p *Principal, exp time.Time, renewed bool, err error) {
	if token == "" {
		return nil, time.Time{}, false, ErrUnauthenticated
	}
	id := hashToken(token)
	var (
		userID   int64
		expires  int64
		lastSeen int64
	)
	err = s.db.R.QueryRowContext(ctx, "SELECT user_id, expires_at, last_seen_at FROM sessions WHERE id = ?", id).Scan(&userID, &expires, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, false, ErrUnauthenticated
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	now := time.Now()
	a, err := s.account(ctx, userID)
	if err != nil || a.disabled || now.UnixMilli() > expires {
		_, _ = s.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
		return nil, time.Time{}, false, ErrUnauthenticated
	}
	exp = db.Time(expires)
	// Renew at most once per minute to keep writes low.
	if now.UnixMilli()-lastSeen > int64(time.Minute/time.Millisecond) {
		exp = now.Add(SessionTTL)
		_, _ = s.db.W.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?",
			now.UnixMilli(), exp.UnixMilli(), id)
		renewed = true
	}
	p = a.principal("session", ScopeWrite)
	p.SessionID = id
	return p, exp, renewed, nil
}

// Logout deletes the session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", hashToken(token))
	return err
}

// ---------------------------------------------------------------- passwords

// VerifyPassword checks the password of a user (re-authentication for sensitive changes).
func (s *Service) VerifyPassword(ctx context.Context, userID int64, pw string) error {
	var hash string
	if err := s.db.R.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash); err != nil {
		return db.NotFound(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

// ChangePassword verifies the old password, sets the new one and ends all other sessions.
func (s *Service) ChangePassword(ctx context.Context, userID int64, keepSession, oldPw, newPw string) error {
	if err := s.VerifyPassword(ctx, userID, oldPw); err != nil {
		return err
	}
	if oldPw == newPw {
		return plugin.FieldErr("new", "Das neue Passwort muss sich vom bisherigen unterscheiden")
	}
	return s.setPassword(ctx, userID, keepSession, newPw, false)
}

// ResetPassword sets the password of a user by name (command line) and ends all sessions.
func (s *Service) ResetPassword(ctx context.Context, username, newPw string) error {
	var id int64
	if err := s.db.R.QueryRowContext(ctx, "SELECT id FROM users WHERE username = ? COLLATE NOCASE", username).Scan(&id); err != nil {
		return db.NotFound(err)
	}
	return s.setPassword(ctx, id, "", newPw, false)
}

func (s *Service) setPassword(ctx context.Context, userID int64, keepSession, pw string, mustChange bool) error {
	if len(pw) < MinPasswordLength {
		return plugin.FieldErr("new", fmt.Sprintf("das Passwort muss mindestens %d Zeichen haben", MinPasswordLength))
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ?, must_change_password = ?, updated_at = ? WHERE id = ?",
			string(hash), db.Bool(mustChange), db.Now(), userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND id <> ?", userID, keepSession)
		return err
	})
}

// AdminID returns the id of the first active administrator (for tokens created on the
// command line).
func (s *Service) AdminID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.R.QueryRowContext(ctx, `SELECT u.id FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.builtin = 'admin' AND u.disabled = 0 ORDER BY u.id LIMIT 1`).Scan(&id)
	return id, db.NotFound(err)
}

// ---------------------------------------------------------------- API tokens

// CreateToken creates an API token and returns the plain token (shown once).
func (s *Service) CreateToken(ctx context.Context, userID int64, name, scope string, expires *time.Time) (string, *Token, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, plugin.FieldErr("name", "Name erforderlich")
	}
	if scope != ScopeRead && scope != ScopeWrite {
		return "", nil, plugin.FieldErr("scope", fmt.Sprintf("Scope muss %q oder %q sein", ScopeRead, ScopeWrite))
	}
	if expires != nil && expires.Before(time.Now()) {
		return "", nil, plugin.FieldErr("expiresAt", "Ablaufdatum liegt in der Vergangenheit")
	}
	var owner string
	if err := s.db.R.QueryRowContext(ctx, "SELECT username FROM users WHERE id = ?", userID).Scan(&owner); err != nil {
		return "", nil, db.NotFound(err)
	}
	r, err := randomString(32)
	if err != nil {
		return "", nil, err
	}
	plain := tokenPrefix + r
	now := time.Now()
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO api_tokens(user_id, name, prefix, token_hash, scope, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?)`, userID, name, plain[:10], hashToken(plain), scope, now.UnixMilli(), db.NullMs(expires))
	if err != nil {
		return "", nil, err
	}
	id, _ := res.LastInsertId()
	return plain, &Token{ID: id, Name: name, Prefix: plain[:10], Scope: scope, UserID: userID, Owner: owner, CreatedAt: now, ExpiresAt: expires}, nil
}

// ListTokens returns the tokens of a user (0 = of every user).
func (s *Service) ListTokens(ctx context.Context, userID int64) ([]Token, error) {
	q := `SELECT t.id, t.name, t.prefix, t.scope, t.user_id, u.username, t.created_at, t.expires_at, t.last_used_at, t.last_used_ip
		FROM api_tokens t JOIN users u ON u.id = t.user_id`
	var args []any
	if userID > 0 {
		q += " WHERE t.user_id = ?"
		args = append(args, userID)
	}
	rows, err := s.db.R.QueryContext(ctx, q+" ORDER BY t.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Token{}
	for rows.Next() {
		var (
			t       Token
			c       int64
			exp, lu sql.NullInt64
		)
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scope, &t.UserID, &t.Owner, &c, &exp, &lu, &t.LastUsedIP); err != nil {
			return nil, err
		}
		t.CreatedAt, t.ExpiresAt, t.LastUsedAt = db.Time(c), db.NullTime(exp), db.NullTime(lu)
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteToken revokes a token of a user (userID 0 = of any user).
func (s *Service) DeleteToken(ctx context.Context, id, userID int64) error {
	q, args := "DELETE FROM api_tokens WHERE id = ?", []any{id}
	if userID > 0 {
		q += " AND user_id = ?"
		args = append(args, userID)
	}
	res, err := s.db.W.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// TokenPrincipal resolves a bearer token. A token has the rights of its user's role
// (a read token only the reading part of them); tokens of disabled users stop working.
func (s *Service) TokenPrincipal(ctx context.Context, plain, ip string) (*Principal, error) {
	if !strings.HasPrefix(plain, tokenPrefix) {
		return nil, ErrUnauthenticated
	}
	h := hashToken(plain)
	var (
		tokenID       int64
		name, scope   string
		stored        string
		userID        int64
		expires, used sql.NullInt64
	)
	err := s.db.R.QueryRowContext(ctx, `SELECT id, name, scope, token_hash, expires_at, last_used_at, user_id
		FROM api_tokens WHERE token_hash = ?`, h).Scan(&tokenID, &name, &scope, &stored, &expires, &used, &userID)
	if err != nil {
		return nil, ErrUnauthenticated
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(h)) != 1 {
		return nil, ErrUnauthenticated
	}
	now := time.Now().UnixMilli()
	if expires.Valid && expires.Int64 < now {
		return nil, ErrUnauthenticated
	}
	a, err := s.account(ctx, userID)
	if err != nil || a.disabled {
		return nil, ErrUnauthenticated
	}
	if !used.Valid || now-used.Int64 > int64(time.Minute/time.Millisecond) {
		_, _ = s.db.W.ExecContext(ctx, "UPDATE api_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?", now, ip, tokenID)
	}
	p := a.principal("token", scope)
	p.TokenID, p.TokenName = tokenID, name
	return p, nil
}

// CleanupSessions removes expired sessions.
func (s *Service) CleanupSessions(ctx context.Context) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", db.Now())
	return err
}

// ---------------------------------------------------------------- rate limiting

type attempt struct {
	failures int
	first    time.Time
	blocked  time.Time
}

// limiter blocks a key after 5 failures within window, for 30 s doubling up to maxBlock.
type limiter struct {
	mu       sync.Mutex
	m        map[string]*attempt
	window   time.Duration
	maxBlock time.Duration
}

func newLimiter(window, maxBlock time.Duration) *limiter {
	return &limiter{m: map[string]*attempt{}, window: window, maxBlock: maxBlock}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	return a == nil || time.Now().After(a.blocked)
}

func (l *limiter) fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.m[key]
	if a == nil || (now.Sub(a.first) > l.window && now.After(a.blocked)) {
		a = &attempt{first: now}
		l.m[key] = a
	}
	a.failures++
	if a.failures >= 5 {
		// 30s, 60s, 120s ... capped at maxBlock
		shift := min(a.failures-5, 10)
		a.blocked = now.Add(min(time.Duration(30<<uint(shift))*time.Second, l.maxBlock))
	}
	if len(l.m) > 10000 {
		for k, v := range l.m {
			if now.Sub(v.first) > l.window && now.After(v.blocked) {
				delete(l.m, k)
			}
		}
	}
}

func (l *limiter) success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, key)
}
