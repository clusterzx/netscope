package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// User is a local user.
type User struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	Email              string `json:"email"`
	RoleID             int64  `json:"roleId"`
	RoleName           string `json:"roleName"`
	Disabled           bool   `json:"disabled"`
	MustChangePassword bool   `json:"mustChangePassword"`
	TOTP               bool   `json:"totp"`
	Passkeys           int    `json:"passkeys"`
	RecoveryCodes      int    `json:"recoveryCodes"` // unused
	// MFARequired: the role requires a second factor.
	MFARequired bool       `json:"mfaRequired"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

const userSelect = `SELECT u.id, u.username, u.display_name, u.email, u.role_id, r.name, r.require_2fa, u.disabled,
	u.must_change_password, u.totp_enabled_at IS NOT NULL,
	(SELECT COUNT(*) FROM user_passkeys k WHERE k.user_id = u.id),
	(SELECT COUNT(*) FROM user_recovery_codes c WHERE c.user_id = u.id AND c.used_at IS NULL),
	u.created_at, u.updated_at, u.last_login_at
	FROM users u JOIN roles r ON r.id = u.role_id`

func scanUser(sc interface{ Scan(...any) error }) (User, error) {
	var (
		u         User
		c, up     int64
		lastLogin sql.NullInt64
	)
	err := sc.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.RoleID, &u.RoleName, &u.MFARequired, &u.Disabled,
		&u.MustChangePassword, &u.TOTP, &u.Passkeys, &u.RecoveryCodes, &c, &up, &lastLogin)
	u.CreatedAt, u.UpdatedAt, u.LastLoginAt = db.Time(c), db.Time(up), db.NullTime(lastLogin)
	return u, err
}

// User returns a user.
func (s *Service) User(ctx context.Context, id int64) (*User, error) {
	u, err := scanUser(s.db.R.QueryRowContext(ctx, userSelect+" WHERE u.id = ?", id))
	if err != nil {
		return nil, db.NotFound(err)
	}
	return &u, nil
}

// Users lists all users.
func (s *Service) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.R.QueryContext(ctx, userSelect+" ORDER BY u.username COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserInput creates or changes a user.
type UserInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	RoleID      int64  `json:"roleId"`
	Disabled    bool   `json:"disabled"`
	// Password of a new account ("" = generate one). The user has to change it at the first
	// login, because the administrator knows it.
	Password string `json:"password,omitempty"`
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

func (in *UserInput) normalize() error {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Email = strings.TrimSpace(in.Email)
	switch {
	case !usernameRe.MatchString(in.Username):
		return plugin.FieldErr("username", "1–64 Zeichen: Buchstaben, Ziffern, . _ @ -")
	case len([]rune(in.DisplayName)) > 100:
		return plugin.FieldErr("displayName", "höchstens 100 Zeichen")
	case in.Email != "" && (!strings.Contains(in.Email, "@") || len(in.Email) > 200):
		return plugin.FieldErr("email", "ungültige E-Mail-Adresse")
	case in.RoleID <= 0:
		return plugin.FieldErr("roleId", "Rolle erforderlich")
	}
	return nil
}

func checkUserConflicts(ctx context.Context, tx *sql.Tx, in UserInput, self int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = ? COLLATE NOCASE AND id <> ?", in.Username, self).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return plugin.FieldErr("username", "Benutzername ist bereits vergeben")
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles WHERE id = ?", in.RoleID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return plugin.FieldErr("roleId", "Rolle existiert nicht")
	}
	return nil
}

// adminsLeft counts the enabled administrators except one user.
func adminsLeft(ctx context.Context, tx *sql.Tx, except int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.builtin = 'admin' AND u.disabled = 0 AND u.id <> ?`, except).Scan(&n)
	return n, err
}

var errLastAdmin = errors.New("Es muss mindestens ein aktiver Benutzer mit der Rolle Administrator bleiben")

// CreateUser creates a user. It returns the generated password when none was given.
func (s *Service) CreateUser(ctx context.Context, in UserInput) (*User, string, error) {
	if err := in.normalize(); err != nil {
		return nil, "", err
	}
	generated := ""
	if in.Password == "" {
		pw, err := GeneratePassword()
		if err != nil {
			return nil, "", err
		}
		in.Password, generated = pw, pw
	} else if len(in.Password) < MinPasswordLength {
		return nil, "", plugin.FieldErr("password", fmt.Sprintf("mindestens %d Zeichen", MinPasswordLength))
	}
	hash, err := hashPassword(in.Password)
	if err != nil {
		return nil, "", err
	}
	var id int64
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := checkUserConflicts(ctx, tx, in, 0); err != nil {
			return err
		}
		now := db.Now()
		res, err := tx.ExecContext(ctx, `INSERT INTO users(username, password_hash, display_name, email, role_id, disabled,
			must_change_password, created_at, updated_at) VALUES (?,?,?,?,?,?,1,?,?)`,
			in.Username, hash, in.DisplayName, in.Email, in.RoleID, db.Bool(in.Disabled), now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	u, err := s.User(ctx, id)
	return u, generated, err
}

// UpdateUser changes a user (not the password). actor is the user making the change: it
// cannot disable itself, and an active administrator always remains.
func (s *Service) UpdateUser(ctx context.Context, actor, id int64, in UserInput) (*User, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var wasAdmin bool
		if err := tx.QueryRowContext(ctx, `SELECT r.builtin = 'admin' AND u.disabled = 0 FROM users u JOIN roles r ON r.id = u.role_id
			WHERE u.id = ?`, id).Scan(&wasAdmin); err != nil {
			return db.NotFound(err)
		}
		if err := checkUserConflicts(ctx, tx, in, id); err != nil {
			return err
		}
		if id == actor && in.Disabled {
			return plugin.FieldErr("disabled", "Das eigene Konto kann nicht deaktiviert werden")
		}
		var isAdminRole bool
		if err := tx.QueryRowContext(ctx, "SELECT builtin = 'admin' FROM roles WHERE id = ?", in.RoleID).Scan(&isAdminRole); err != nil {
			return err
		}
		if wasAdmin && (!isAdminRole || in.Disabled) {
			n, err := adminsLeft(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return errLastAdmin
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET username = ?, display_name = ?, email = ?, role_id = ?, disabled = ?,
			updated_at = ? WHERE id = ?`, in.Username, in.DisplayName, in.Email, in.RoleID, db.Bool(in.Disabled), db.Now(), id); err != nil {
			return err
		}
		if in.Disabled {
			_, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", id)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.User(ctx, id)
}

// DeleteUser removes a user with its sessions, tokens and second factors.
func (s *Service) DeleteUser(ctx context.Context, actor, id int64) error {
	if id == actor {
		return errors.New("Das eigene Konto kann nicht gelöscht werden")
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var admin bool
		if err := tx.QueryRowContext(ctx, `SELECT r.builtin = 'admin' AND u.disabled = 0 FROM users u JOIN roles r ON r.id = u.role_id
			WHERE u.id = ?`, id).Scan(&admin); err != nil {
			return db.NotFound(err)
		}
		if admin {
			n, err := adminsLeft(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return errLastAdmin
			}
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
		return err
	})
}

// SetTemporaryPassword gives a user a new generated password that has to be changed at the
// next login, and ends the user's sessions.
func (s *Service) SetTemporaryPassword(ctx context.Context, id int64) (string, error) {
	pw, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	if _, err := s.User(ctx, id); err != nil {
		return "", err
	}
	return pw, s.setPassword(ctx, id, "", pw, true)
}

// ResetMFA removes TOTP, passkeys and recovery codes of a user (lost device) and ends the
// user's sessions. A role that requires a second factor makes the user set up a new one.
func (s *Service) ResetMFA(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret = NULL, totp_pending = NULL, totp_enabled_at = NULL,
			updated_at = ? WHERE id = ?`, db.Now(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		for _, q := range []string{"DELETE FROM user_passkeys WHERE user_id = ?", "DELETE FROM user_recovery_codes WHERE user_id = ?",
			"DELETE FROM sessions WHERE user_id = ?"} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetMFAByName resets the second factors of a user by name (command line).
func (s *Service) ResetMFAByName(ctx context.Context, username string) error {
	var id int64
	if err := s.db.R.QueryRowContext(ctx, "SELECT id FROM users WHERE username = ? COLLATE NOCASE", username).Scan(&id); err != nil {
		return db.NotFound(err)
	}
	return s.ResetMFA(ctx, id)
}

// ---------------------------------------------------------------- roles

// Role is a named set of permissions.
type Role struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Require2FA  bool     `json:"require2fa"`
	// Admin marks the built-in administrator role: every permission, name and permissions
	// cannot be changed.
	Admin     bool      `json:"admin"`
	Users     int       `json:"users"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RoleInput creates or changes a role.
type RoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Require2FA  bool     `json:"require2fa"`
}

const roleSelect = `SELECT r.id, r.name, r.description, r.permissions, r.require_2fa, r.builtin = 'admin',
	(SELECT COUNT(*) FROM users u WHERE u.role_id = r.id), r.created_at, r.updated_at FROM roles r`

func scanRole(sc interface{ Scan(...any) error }) (Role, error) {
	var (
		r     Role
		perms string
		c, u  int64
	)
	err := sc.Scan(&r.ID, &r.Name, &r.Description, &perms, &r.Require2FA, &r.Admin, &r.Users, &c, &u)
	r.Permissions = []string{}
	_ = db.Unmarshal(perms, &r.Permissions)
	if r.Admin {
		r.Permissions = AllPermissions()
	}
	r.CreatedAt, r.UpdatedAt = db.Time(c), db.Time(u)
	return r, err
}

// Roles lists all roles (administrator first).
func (s *Service) Roles(ctx context.Context) ([]Role, error) {
	rows, err := s.db.R.QueryContext(ctx, roleSelect+" ORDER BY r.builtin = 'admin' DESC, r.name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Role{}
	for rows.Next() {
		r, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Role returns a role.
func (s *Service) Role(ctx context.Context, id int64) (*Role, error) {
	r, err := scanRole(s.db.R.QueryRowContext(ctx, roleSelect+" WHERE r.id = ?", id))
	if err != nil {
		return nil, db.NotFound(err)
	}
	return &r, nil
}

func (in *RoleInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 64:
		return plugin.FieldErr("name", "1–64 Zeichen")
	case len([]rune(in.Description)) > 300:
		return plugin.FieldErr("description", "höchstens 300 Zeichen")
	}
	seen := map[string]bool{}
	perms := []string{}
	for _, p := range in.Permissions {
		if !ValidPermission(p) {
			return plugin.FieldErr("permissions", fmt.Sprintf("unbekannte Berechtigung %q", p))
		}
		if !seen[p] {
			seen[p] = true
			perms = append(perms, p)
		}
	}
	in.Permissions = perms
	return nil
}

func roleNameTaken(ctx context.Context, tx *sql.Tx, name string, self int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM roles WHERE name = ? AND id <> ?", name, self).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return plugin.FieldErr("name", "Eine Rolle mit diesem Namen gibt es bereits")
	}
	return nil
}

// CreateRole creates a role.
func (s *Service) CreateRole(ctx context.Context, in RoleInput) (*Role, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	var id int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := roleNameTaken(ctx, tx, in.Name, 0); err != nil {
			return err
		}
		now := db.Now()
		res, err := tx.ExecContext(ctx, `INSERT INTO roles(name, description, permissions, require_2fa, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`, in.Name, in.Description, db.JSON(in.Permissions), db.Bool(in.Require2FA), now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.Role(ctx, id)
}

// UpdateRole changes a role. Of the administrator role only the description and the 2FA
// requirement can change.
func (s *Service) UpdateRole(ctx context.Context, id int64, in RoleInput) (*Role, error) {
	if err := in.normalize(); err != nil {
		return nil, err
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			admin bool
			name  string
		)
		if err := tx.QueryRowContext(ctx, "SELECT builtin = 'admin', name FROM roles WHERE id = ?", id).Scan(&admin, &name); err != nil {
			return db.NotFound(err)
		}
		if admin {
			_, err := tx.ExecContext(ctx, "UPDATE roles SET description = ?, require_2fa = ?, updated_at = ? WHERE id = ?",
				in.Description, db.Bool(in.Require2FA), db.Now(), id)
			return err
		}
		if err := roleNameTaken(ctx, tx, in.Name, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE roles SET name = ?, description = ?, permissions = ?, require_2fa = ?, updated_at = ? WHERE id = ?",
			in.Name, in.Description, db.JSON(in.Permissions), db.Bool(in.Require2FA), db.Now(), id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Role(ctx, id)
}

// DeleteRole removes a role no user has.
func (s *Service) DeleteRole(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var (
			admin bool
			users int
		)
		if err := tx.QueryRowContext(ctx, "SELECT builtin = 'admin', (SELECT COUNT(*) FROM users WHERE role_id = ?) FROM roles WHERE id = ?",
			id, id).Scan(&admin, &users); err != nil {
			return db.NotFound(err)
		}
		if admin {
			return errors.New("Die Rolle Administrator kann nicht gelöscht werden")
		}
		if users > 0 {
			return fmt.Errorf("Die Rolle ist noch %d Benutzer(n) zugewiesen", users)
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM roles WHERE id = ?", id)
		return err
	})
}

func hashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(h), err
}
