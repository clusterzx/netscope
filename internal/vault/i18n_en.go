package vault

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// credentials
		"Name bereits vergeben":                                "Name already taken",
		"Pflichtfeld":                                          "Required",
		"Schlüssel oder Passwort erforderlich":                 "Key or password required",
		"unbekannter Credential-Typ %q":                        "unknown credential type %q",
		"der Typ eines Credentials kann nicht geändert werden": "the type of a credential cannot be changed",
		"Credential %d existiert nicht":                        "Credential %d does not exist",
		"Credential %d hat Typ %s, erlaubt: %s":                "Credential %d has type %s, allowed: %s",

		// master key and encryption
		"vault: master key passt nicht zur Datenbank":                 "vault: master key does not match the database",
		"master key muss %d Byte lang sein (ist %d)":                  "master key must be %d bytes long (is %d)",
		"master key: weder Base64 noch Hex":                           "master key: neither Base64 nor hex",
		"vault: unbekanntes Blob-Format":                              "vault: unknown blob format",
		"vault: Blob zu kurz":                                         "vault: blob too short",
		"vault: Entschlüsselung fehlgeschlagen":                       "vault: decryption failed",
		"neuer Schlüssel muss %d Byte lang sein":                      "new key must be %d bytes long",
		"neuen Schlüssel schreiben: %w":                               "writing the new key: %w",
		"Schlüsseldatei ersetzen: %w (neuer Schlüssel liegt in %s%s)": "Replacing the key file: %w (the new key is in %s%s)",
	})
}
