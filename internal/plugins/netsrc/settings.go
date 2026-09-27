package netsrc

import (
	"context"
	"fmt"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// Setting keys shared by the importers.
const (
	KeySources     = "hosts"
	KeyCredentials = "credentials"
	KeyVerifyTLS   = "verify_tls"
	KeyCreate      = "create_missing"
	KeyARP         = "include_arp"
)

// SourcesField lists the devices to read (host, IP or URL per line).
func SourcesField(label, def, desc string) plugin.Field {
	f := plugin.Field{Key: KeySources, Type: plugin.FieldStringList, Label: label, Required: true, Description: desc}
	if def != "" {
		f.Default = []string{def}
	}
	return f
}

// CredentialsField selects the credentials (empty: every matching one by scope).
func CredentialsField(types []string, desc string) plugin.Field {
	return plugin.Field{Key: KeyCredentials, Type: plugin.FieldCredentialRef, Label: "Zugangsdaten", Multi: true, CredentialTypes: types,
		Description: desc + " Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen."}
}

// VerifyTLSField: devices usually have self-signed certificates.
func VerifyTLSField() plugin.Field {
	return plugin.Field{Key: KeyVerifyTLS, Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: false, Advanced: true,
		Description: "Nur mit gültigem Zertifikat einschalten; die meisten Geräte nutzen ein selbstsigniertes."}
}

// CreateField: create devices that no scan has found yet.
func CreateField() plugin.Field {
	return plugin.Field{Key: KeyCreate, Type: plugin.FieldBool, Label: "Fehlende Geräte anlegen", Default: false,
		Description: "Geräte anlegen, die noch kein Scan gefunden hat – nützlich für Netze, die NetScope nicht selbst scannt. Aus: nur bekannte Geräte ergänzen."}
}

// ARPField: also read the ARP table (devices with fixed addresses, no lease).
func ARPField() plugin.Field {
	return plugin.Field{Key: KeyARP, Type: plugin.FieldBool, Label: "ARP-Tabelle einbeziehen", Default: true,
		Description: "Auch Geräte mit fester IP-Adresse (ohne Lease) aus der ARP-Tabelle übernehmen."}
}

// ValidateSources checks the source entries with parse.
func ValidateSources(s plugin.Settings, parse func(string) error) error {
	for _, h := range s.StringList(KeySources) {
		if err := parse(h); err != nil {
			return plugin.FieldErr(KeySources, err.Error())
		}
	}
	return nil
}

// Endpoints are the host names of the sources (for the firewall hints of the UI).
func Endpoints(s plugin.Settings) []string {
	var out []string
	for _, h := range s.StringList(KeySources) {
		if host := Host(h); host != "" {
			out = append(out, host)
		}
	}
	return out
}

// Credentials returns the credentials to try for a source, in order.
func Credentials(ctx context.Context, rc *plugin.RunContext, types []string, source string) ([]*plugin.Credential, error) {
	picker := &plugin.CredentialPicker{Creds: rc.Creds, Types: types, Allowed: rc.Settings.CredentialIDs(KeyCredentials), Log: rc.Log}
	host := Host(source)
	ip, _ := netutil.ResolveHost(ctx, host)
	creds, err := picker.For(ctx, plugin.CredentialTarget{IP: ip})
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("keine passenden Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w", host, plugin.ErrNoCredential)
	}
	return creds, nil
}

// TryCredentials calls fn with each credential until one is not rejected (ErrAuth).
func TryCredentials(rc *plugin.RunContext, source string, creds []*plugin.Credential, fn func(*plugin.Credential) error) error {
	var err error
	for i, c := range creds {
		err = fn(c)
		if err == nil || !isAuth(err) || i == len(creds)-1 {
			return err
		}
		rc.Log.Info("Anmeldung abgelehnt, nächste Zugangsdaten werden probiert", "quelle", source, "credential", c.Name)
	}
	return err
}
