package plugin

import "fmt"

// Credential types.
const (
	CredSSH      = "ssh"
	CredPassword = "password"
	CredSNMPv2c  = "snmp_v2c"
	CredSNMPv3   = "snmp_v3"
	CredAPIToken = "api_token"
)

// Credential is a decrypted vault entry. Only plugins get decrypted credentials;
// the API never returns secret values.
type Credential struct {
	ID     int64
	Name   string
	Type   string
	Public map[string]string
	Secret map[string]string
}

// Get returns a field value (secret fields take precedence).
func (c *Credential) Get(key string) string {
	if c == nil {
		return ""
	}
	if v, ok := c.Secret[key]; ok {
		return v
	}
	return c.Public[key]
}

// RequireType returns an error unless the credential has one of the given types.
func (c *Credential) RequireType(types ...string) error {
	for _, t := range types {
		if c.Type == t {
			return nil
		}
	}
	return fmt.Errorf("Credential %q hat Typ %s, erwartet %v", c.Name, c.Type, types)
}

// CredentialType describes a vault entry type; its fields are rendered like plugin settings.
type CredentialType struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Schema      Schema `json:"schema"`
}

// CredentialTypes returns all credential types.
func CredentialTypes() []CredentialType {
	return []CredentialType{
		{CredSSH, "SSH", "Benutzer mit privatem Schlüssel und/oder Passwort für SSH-Zugriffe.", Schema{Fields: []Field{
			{Key: "username", Type: FieldString, Label: "Benutzername", Required: true},
			{Key: "private_key", Type: FieldSecret, Label: "Privater Schlüssel", Multiline: true,
				Description: "OpenSSH- oder PEM-Format. Leer lassen für Passwort-Anmeldung."},
			{Key: "passphrase", Type: FieldSecret, Label: "Passphrase des Schlüssels"},
			{Key: "password", Type: FieldSecret, Label: "Passwort", Description: "Alternative oder Fallback zur Schlüssel-Anmeldung."},
		}}},
		{CredPassword, "Benutzer/Passwort", "Allgemeine Zugangsdaten (SMTP, LuCI, Web-APIs).", Schema{Fields: []Field{
			{Key: "username", Type: FieldString, Label: "Benutzername"},
			{Key: "password", Type: FieldSecret, Label: "Passwort", Required: true},
		}}},
		{CredSNMPv2c, "SNMP v2c", "Community-String für SNMP v1/v2c.", Schema{Fields: []Field{
			{Key: "community", Type: FieldSecret, Label: "Community", Required: true},
		}}},
		{CredSNMPv3, "SNMP v3", "Benutzerbasierte SNMPv3-Zugangsdaten (USM).", Schema{Fields: []Field{
			{Key: "username", Type: FieldString, Label: "Benutzername", Required: true},
			{Key: "security_level", Type: FieldEnum, Label: "Sicherheitsstufe", Default: "authPriv", Options: []Option{
				{"noAuthNoPriv", "noAuthNoPriv"}, {"authNoPriv", "authNoPriv"}, {"authPriv", "authPriv"}}},
			{Key: "auth_protocol", Type: FieldEnum, Label: "Auth-Protokoll", Default: "SHA", Options: []Option{
				{"MD5", "MD5"}, {"SHA", "SHA-1"}, {"SHA224", "SHA-224"}, {"SHA256", "SHA-256"}, {"SHA384", "SHA-384"}, {"SHA512", "SHA-512"}},
				VisibleIf: &Condition{Field: "security_level", Equals: []any{"authNoPriv", "authPriv"}}},
			{Key: "auth_password", Type: FieldSecret, Label: "Auth-Passwort",
				VisibleIf: &Condition{Field: "security_level", Equals: []any{"authNoPriv", "authPriv"}}},
			{Key: "priv_protocol", Type: FieldEnum, Label: "Privacy-Protokoll", Default: "AES", Options: []Option{
				{"DES", "DES"}, {"AES", "AES-128"}, {"AES192", "AES-192"}, {"AES256", "AES-256"}, {"AES192C", "AES-192 (Cisco)"}, {"AES256C", "AES-256 (Cisco)"}},
				VisibleIf: &Condition{Field: "security_level", Equals: []any{"authPriv"}}},
			{Key: "priv_password", Type: FieldSecret, Label: "Privacy-Passwort",
				VisibleIf: &Condition{Field: "security_level", Equals: []any{"authPriv"}}},
			{Key: "context_name", Type: FieldString, Label: "Context-Name", Advanced: true},
		}}},
		{CredAPIToken, "API-Token", "Token für HTTP-APIs (z. B. Proxmox: Token-ID user@realm!name + Secret).", Schema{Fields: []Field{
			{Key: "token_id", Type: FieldString, Label: "Token-ID", Description: "Bei Proxmox: user@realm!tokenname. Sonst optional."},
			{Key: "token", Type: FieldSecret, Label: "Token/Secret", Required: true},
		}}},
	}
}

// LookupCredentialType returns the definition of a credential type.
func LookupCredentialType(t string) (CredentialType, bool) {
	for _, ct := range CredentialTypes() {
		if ct.Type == t {
			return ct, true
		}
	}
	return CredentialType{}, false
}
