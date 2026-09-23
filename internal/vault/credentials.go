package vault

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/wgconf"
)

// CredentialMeta is the public view of a credential (no secret values).
type CredentialMeta struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Public      map[string]string `json:"public"`
	SecretsSet  []string          `json:"secretsSet"` // names of secret fields that have a value
	Scope       plugin.Scope      `json:"scope"`      // where the credential applies
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	LastUsedAt  *time.Time        `json:"lastUsedAt,omitempty"`
}

// CredentialInput creates or updates a credential. Values holds all fields of the type
// schema; secret fields equal to plugin.SecretMask keep their stored value.
type CredentialInput struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Values      map[string]any `json:"values"`
	// Scope limits where the credential applies (nil: everywhere on create, unchanged on update).
	Scope *plugin.Scope `json:"scope,omitempty"`
}

// CredentialScope is the selection data of a credential (no values).
type CredentialScope struct {
	ID    int64
	Name  string
	Type  string
	Scope plugin.Scope
}

func scopeOrDefault(s *plugin.Scope) plugin.Scope {
	if s == nil {
		return plugin.DefaultScope()
	}
	return *s
}

// ErrDuplicateName is returned when a credential name is already used.
var ErrDuplicateName = errors.New("Name bereits vergeben")

func (v *Vault) splitValues(ct plugin.CredentialType, in map[string]any, prevSecret map[string]string, prevPublic map[string]string) (map[string]string, map[string]string, error) {
	prev := map[string]any{}
	for k, val := range prevPublic {
		prev[k] = val
	}
	for k, val := range prevSecret {
		prev[k] = val
	}
	vals, err := ct.Schema.Validate(in, prev, nil)
	if err != nil {
		return nil, nil, err
	}
	pub, sec := map[string]string{}, map[string]string{}
	for _, f := range ct.Schema.Fields {
		s := fmt.Sprint(vals[f.Key])
		if f.IsSecret() {
			if s != "" {
				sec[f.Key] = s
			}
		} else {
			pub[f.Key] = s
		}
	}
	if ct.Type == plugin.CredSSH && sec["private_key"] == "" && sec["password"] == "" {
		return nil, nil, &plugin.ValidationError{Errors: []plugin.FieldError{{Field: "private_key", Message: "Schlüssel oder Passwort erforderlich"}}}
	}
	if ct.Type == plugin.CredWireGuard {
		if _, err := wgconf.Parse(sec["config"]); err != nil {
			return nil, nil, plugin.FieldErr("config", err.Error())
		}
	}
	return pub, sec, nil
}

// CreateCredential stores a new credential.
func (v *Vault) CreateCredential(ctx context.Context, in CredentialInput) (int64, error) {
	ct, ok := plugin.LookupCredentialType(in.Type)
	if !ok {
		return 0, fmt.Errorf("unbekannter Credential-Typ %q", in.Type)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return 0, &plugin.ValidationError{Errors: []plugin.FieldError{{Field: "name", Message: "Pflichtfeld"}}}
	}
	pub, sec, err := v.splitValues(ct, in.Values, nil, nil)
	if err != nil {
		return 0, err
	}
	blob, err := v.sealSecrets(sec)
	if err != nil {
		return 0, err
	}
	now := db.Now()
	res, err := v.db.W.ExecContext(ctx, `INSERT INTO credentials(name, type, description, public, secret, key_id, scope, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, name, in.Type, in.Description, db.JSON(pub), blob, v.KeyID(), db.JSON(scopeOrDefault(in.Scope)), now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrDuplicateName
		}
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCredential changes name, description and values. The type cannot change.
func (v *Vault) UpdateCredential(ctx context.Context, id int64, in CredentialInput) error {
	meta, pubPrev, secPrev, err := v.load(ctx, id)
	if err != nil {
		return err
	}
	if in.Type != "" && in.Type != meta.Type {
		return fmt.Errorf("der Typ eines Credentials kann nicht geändert werden")
	}
	ct, _ := plugin.LookupCredentialType(meta.Type)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = meta.Name
	}
	pub, sec, err := v.splitValues(ct, in.Values, secPrev, pubPrev)
	if err != nil {
		return err
	}
	blob, err := v.sealSecrets(sec)
	if err != nil {
		return err
	}
	scope := meta.Scope
	if in.Scope != nil {
		scope = *in.Scope
	}
	_, err = v.db.W.ExecContext(ctx, `UPDATE credentials SET name=?, description=?, public=?, secret=?, key_id=?, scope=?, updated_at=? WHERE id=?`,
		name, in.Description, db.JSON(pub), blob, v.KeyID(), db.JSON(scope), db.Now(), id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrDuplicateName
	}
	return err
}

// DeleteCredential removes a credential.
func (v *Vault) DeleteCredential(ctx context.Context, id int64) error {
	res, err := v.db.W.ExecContext(ctx, "DELETE FROM credentials WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

func (v *Vault) sealSecrets(sec map[string]string) ([]byte, error) {
	if len(sec) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(sec)
	if err != nil {
		return nil, err
	}
	return v.Encrypt(b)
}

func (v *Vault) load(ctx context.Context, id int64) (*CredentialMeta, map[string]string, map[string]string, error) {
	var (
		m        CredentialMeta
		pubJSON  string
		scopeJS  string
		blob     []byte
		created  int64
		updated  int64
		lastUsed sql.NullInt64
	)
	err := v.db.R.QueryRowContext(ctx, `SELECT id, name, type, description, public, secret, scope, created_at, updated_at, last_used_at
		FROM credentials WHERE id = ?`, id).Scan(&m.ID, &m.Name, &m.Type, &m.Description, &pubJSON, &blob, &scopeJS, &created, &updated, &lastUsed)
	if err != nil {
		return nil, nil, nil, db.NotFound(err)
	}
	pub := map[string]string{}
	_ = db.Unmarshal(pubJSON, &pub)
	sec := map[string]string{}
	if len(blob) > 0 {
		p, err := v.Decrypt(blob)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("credential %d: %w", id, err)
		}
		if err := json.Unmarshal(p, &sec); err != nil {
			return nil, nil, nil, err
		}
	}
	m.Public = pub
	m.Scope = parseScope(scopeJS)
	m.CreatedAt = db.Time(created)
	m.UpdatedAt = db.Time(updated)
	m.LastUsedAt = db.NullTime(lastUsed)
	for k := range sec {
		m.SecretsSet = append(m.SecretsSet, k)
	}
	sort.Strings(m.SecretsSet)
	return &m, pub, sec, nil
}

// Meta returns the public view of one credential.
func (v *Vault) Meta(ctx context.Context, id int64) (*CredentialMeta, error) {
	m, _, _, err := v.load(ctx, id)
	return m, err
}

// ListCredentials returns all credentials without secret values.
func (v *Vault) ListCredentials(ctx context.Context) ([]CredentialMeta, error) {
	rows, err := v.db.R.QueryContext(ctx, "SELECT id FROM credentials ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	out := make([]CredentialMeta, 0, len(ids))
	for _, id := range ids {
		m, err := v.Meta(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, nil
}

// CredentialScopes returns the scopes of all credentials (optionally of the given types)
// without decrypting anything.
func (v *Vault) CredentialScopes(ctx context.Context, types []string) ([]CredentialScope, error) {
	q, args := "SELECT id, name, type, scope FROM credentials", []any{}
	if len(types) > 0 {
		q += " WHERE type IN (" + db.Placeholders(len(types)) + ")"
		args = db.StringArgs(types)
	}
	rows, err := v.db.R.QueryContext(ctx, q+" ORDER BY name COLLATE NOCASE", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CredentialScope
	for rows.Next() {
		var (
			c  CredentialScope
			js string
		)
		if err := rows.Scan(&c.ID, &c.Name, &c.Type, &js); err != nil {
			return nil, err
		}
		c.Scope = parseScope(js)
		out = append(out, c)
	}
	return out, rows.Err()
}

func parseScope(js string) plugin.Scope {
	var s plugin.Scope
	if err := db.Unmarshal(js, &s); err != nil {
		return plugin.DefaultScope()
	}
	if !s.AllSubnets && len(s.Subnets) == 0 && !s.DeviceRestricted() {
		s.AllSubnets = true
	}
	return s
}

// Get decrypts a credential for a plugin and records the usage time.
func (v *Vault) Get(ctx context.Context, id int64) (*plugin.Credential, error) {
	m, pub, sec, err := v.load(ctx, id)
	if err != nil {
		return nil, err
	}
	_, _ = v.db.W.ExecContext(ctx, "UPDATE credentials SET last_used_at = ? WHERE id = ?", db.Now(), id)
	return &plugin.Credential{ID: m.ID, Name: m.Name, Type: m.Type, Public: pub, Secret: sec}, nil
}

// Check verifies that a credential exists and has one of the allowed types.
func (v *Vault) Check(ctx context.Context, id int64, allowed []string) error {
	var typ string
	if err := v.db.R.QueryRowContext(ctx, "SELECT type FROM credentials WHERE id = ?", id).Scan(&typ); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("Credential %d existiert nicht", id)
		}
		return err
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, a := range allowed {
		if a == typ {
			return nil
		}
	}
	return fmt.Errorf("Credential %d hat Typ %s, erlaubt: %s", id, typ, strings.Join(allowed, ", "))
}
