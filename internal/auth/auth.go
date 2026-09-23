// Package auth implements the local admin login (bcrypt + session cookie) and scoped
// API tokens (read/write) for scripts.
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
)

const (
	// SessionTTL is the idle lifetime of a session (sliding).
	SessionTTL = 7 * 24 * time.Hour
	// MinPasswordLength for the admin password.
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
)

// Principal is the authenticated caller.
type Principal struct {
	UserID    int64  `json:"userId"`
	Username  string `json:"username"`
	Kind      string `json:"kind"` // session | token | system
	Scope     string `json:"scope"`
	TokenID   int64  `json:"tokenId,omitempty"`
	TokenName string `json:"tokenName,omitempty"`
	SessionID string `json:"-"`
}

// CanWrite reports whether the principal may modify data.
func (p *Principal) CanWrite() bool { return p != nil && p.Scope == ScopeWrite }

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

// User is a local user.
type User struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// Token is an API token without its secret.
type Token struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scope      string     `json:"scope"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	LastUsedIP string     `json:"lastUsedIp,omitempty"`
}

// Service is the auth service.
type Service struct {
	db      *db.DB
	limiter *limiter
}

// New creates the service.
func New(d *db.DB) *Service { return &Service{db: d, limiter: newLimiter()} }

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

// GeneratePassword returns a random password suitable for the initial admin.
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
	_, err = s.db.W.ExecContext(ctx, "INSERT INTO users(username, password_hash, created_at, updated_at) VALUES (?,?,?,?)",
		adminUser, string(hash), now, now)
	return err == nil, generated, err
}

// Login verifies the password and creates a session. It returns the cookie token.
func (s *Service) Login(ctx context.Context, username, password, ip, userAgent string) (string, time.Time, error) {
	if !s.limiter.allow(ip) {
		return "", time.Time{}, ErrRateLimited
	}
	var (
		id   int64
		hash string
	)
	err := s.db.R.QueryRowContext(ctx, "SELECT id, password_hash FROM users WHERE username = ?", strings.TrimSpace(username)).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// constant-ish time: still run bcrypt
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$C6UzMDM.H6dfI/f/IKxGhuE0wYt1yZ0dD6MdH4J8YGAC5xzOO7GvS"), []byte(password))
		s.limiter.fail(ip)
		return "", time.Time{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", time.Time{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		s.limiter.fail(ip)
		return "", time.Time{}, ErrInvalidCredentials
	}
	s.limiter.success(ip)
	token, err := randomString(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	exp := now.Add(SessionTTL)
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err = s.db.W.ExecContext(ctx, `INSERT INTO sessions(id, user_id, created_at, expires_at, last_seen_at, ip, user_agent)
		VALUES (?,?,?,?,?,?,?)`, hashToken(token), id, now.UnixMilli(), exp.UnixMilli(), now.UnixMilli(), ip, userAgent)
	if err != nil {
		return "", time.Time{}, err
	}
	_, _ = s.db.W.ExecContext(ctx, "UPDATE users SET last_login_at = ? WHERE id = ?", now.UnixMilli(), id)
	return token, exp, nil
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
		username string
		expires  int64
		lastSeen int64
	)
	err = s.db.R.QueryRowContext(ctx, `SELECT s.user_id, u.username, s.expires_at, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, id).Scan(&userID, &username, &expires, &lastSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, false, ErrUnauthenticated
	}
	if err != nil {
		return nil, time.Time{}, false, err
	}
	now := time.Now()
	if now.UnixMilli() > expires {
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
	return &Principal{UserID: userID, Username: username, Kind: "session", Scope: ScopeWrite, SessionID: id}, exp, renewed, nil
}

// Logout deletes the session.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", hashToken(token))
	return err
}

// ChangePassword verifies the old password, sets the new one and ends all other sessions.
func (s *Service) ChangePassword(ctx context.Context, userID int64, keepSession, oldPw, newPw string) error {
	var hash string
	if err := s.db.R.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id = ?", userID).Scan(&hash); err != nil {
		return db.NotFound(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPw)) != nil {
		return ErrInvalidCredentials
	}
	return s.setPassword(ctx, userID, keepSession, newPw)
}

// ResetPassword sets the password of a user by name (CLI) and ends all sessions.
func (s *Service) ResetPassword(ctx context.Context, username, newPw string) error {
	var id int64
	if err := s.db.R.QueryRowContext(ctx, "SELECT id FROM users WHERE username = ?", username).Scan(&id); err != nil {
		return db.NotFound(err)
	}
	return s.setPassword(ctx, id, "", newPw)
}

func (s *Service) setPassword(ctx context.Context, userID int64, keepSession, pw string) error {
	if len(pw) < MinPasswordLength {
		return fmt.Errorf("das Passwort muss mindestens %d Zeichen haben", MinPasswordLength)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?", string(hash), db.Now(), userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ? AND id <> ?", userID, keepSession)
		return err
	})
}

// User returns a user.
func (s *Service) User(ctx context.Context, id int64) (*User, error) {
	var (
		u         User
		c, up     int64
		lastLogin sql.NullInt64
	)
	err := s.db.R.QueryRowContext(ctx, "SELECT id, username, created_at, updated_at, last_login_at FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &c, &up, &lastLogin)
	if err != nil {
		return nil, db.NotFound(err)
	}
	u.CreatedAt, u.UpdatedAt, u.LastLoginAt = db.Time(c), db.Time(up), db.NullTime(lastLogin)
	return &u, nil
}

// AdminID returns the id of the admin user (for CLI-created tokens).
func (s *Service) AdminID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.R.QueryRowContext(ctx, "SELECT id FROM users ORDER BY id LIMIT 1").Scan(&id)
	return id, db.NotFound(err)
}

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
	return plain, &Token{ID: id, Name: name, Prefix: plain[:10], Scope: scope, CreatedAt: now, ExpiresAt: expires}, nil
}

// ListTokens returns all tokens.
func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, prefix, scope, created_at, expires_at, last_used_at, last_used_ip
		FROM api_tokens ORDER BY id`)
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
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &t.Scope, &c, &exp, &lu, &t.LastUsedIP); err != nil {
			return nil, err
		}
		t.CreatedAt, t.ExpiresAt, t.LastUsedAt = db.Time(c), db.NullTime(exp), db.NullTime(lu)
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteToken revokes a token.
func (s *Service) DeleteToken(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, "DELETE FROM api_tokens WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// TokenPrincipal resolves a bearer token.
func (s *Service) TokenPrincipal(ctx context.Context, plain, ip string) (*Principal, error) {
	if !strings.HasPrefix(plain, tokenPrefix) {
		return nil, ErrUnauthenticated
	}
	h := hashToken(plain)
	var (
		p       Principal
		stored  string
		expires sql.NullInt64
		lastUse sql.NullInt64
	)
	err := s.db.R.QueryRowContext(ctx, `SELECT t.id, t.name, t.scope, t.token_hash, t.expires_at, t.last_used_at, u.id, u.username
		FROM api_tokens t JOIN users u ON u.id = t.user_id WHERE t.token_hash = ?`, h).
		Scan(&p.TokenID, &p.TokenName, &p.Scope, &stored, &expires, &lastUse, &p.UserID, &p.Username)
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
	if !lastUse.Valid || now-lastUse.Int64 > int64(time.Minute/time.Millisecond) {
		_, _ = s.db.W.ExecContext(ctx, "UPDATE api_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?", now, ip, p.TokenID)
	}
	p.Kind = "token"
	return &p, nil
}

// CleanupSessions removes expired sessions.
func (s *Service) CleanupSessions(ctx context.Context) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at < ?", db.Now())
	return err
}

// ---------------------------------------------------------------- login rate limiting

type attempt struct {
	failures int
	first    time.Time
	blocked  time.Time
}

type limiter struct {
	mu sync.Mutex
	m  map[string]*attempt
}

func newLimiter() *limiter { return &limiter{m: map[string]*attempt{}} }

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[ip]
	return a == nil || time.Now().After(a.blocked)
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.m[ip]
	if a == nil || now.Sub(a.first) > 15*time.Minute {
		a = &attempt{first: now}
		l.m[ip] = a
	}
	a.failures++
	if a.failures >= 5 {
		// 30s, 60s, 120s ... capped at 15 minutes
		shift := a.failures - 5
		if shift > 5 {
			shift = 5
		}
		a.blocked = now.Add(time.Duration(30<<uint(shift)) * time.Second)
	}
	if len(l.m) > 10000 {
		for k, v := range l.m {
			if now.Sub(v.first) > 15*time.Minute && now.After(v.blocked) {
				delete(l.m, k)
			}
		}
	}
}

func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.m, ip)
}
