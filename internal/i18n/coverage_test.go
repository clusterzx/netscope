package i18n_test

// Coverage tests: every German text in the server code has an English translation.
//
//   - TestCatalogCoverage scans the Go sources for German string literals (and patterns
//     built from concatenations like "Agent auf " + host + " meldet sich nicht") and
//     requires a catalog entry for each. Literals that are no display text (parser
//     keywords, CSV column names …) carry the comment "i18n:ignore" on their line.
//   - TestStructuredTexts requires a translation for every label, description, option and
//     hint of the catalogs the API delivers (plugins, actions, events, credentials,
//     permissions, query fields) – including language-neutral ones, so a translator has
//     decided about each.
//
// I18N_MISSING=<file> writes the missing texts per package as JSON, a starting point for
// new i18n_en.go entries.

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	_ "netscope/internal/app" // registers every catalog of the server
	"netscope/internal/auth"
	"netscope/internal/i18n"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
)

// germanWords are words that mark a text as German (in addition to umlauts and ß). They
// are chosen not to occur in English texts.
var germanWords = regexp.MustCompile(`(?i)\b(der|das|und|oder|nicht|kein|keine|keinen|keiner|keinem|ist|sind|wird|werden|wurde|wurden|` +
	`für|mit|auf|bei|nach|vom|zum|zur|aus|ein|eine|einen|einem|einer|eines|dem|des|den|als|auch|noch|nur|schon|bitte|` +
	`ungültig|ungültige|ungültiger|ungültiges|erwartet|fehlt|fehlen|gefunden|konnte|kann|darf|muss|soll|` +
	`gerät|geräte|geräten|fehler|datei|dateien|zeit|lauf|läufe|anmeldung|benutzer|passwort|zugangsdaten|eintrag|einträge|` +
	`wert|werte|leer|leere|leerer|unbekannt|unbekannte|unbekannter|unbekanntes|abgebrochen|fehlgeschlagen|erfolgreich|` +
	`verbindung|antwort|anfrage|dienst|dienste|netz|netze|subnetz|subnetze|standort|standorte|zentrale|regel|regeln|` +
	`bericht|berichte|schwachstelle|schwachstellen|hersteller|modell|zeile|spalte|feld|felder|pflichtfeld|gesetzt|` +
	`gespeichert|gelöscht|angelegt|entfernt|aktiviert|deaktiviert|abgelaufen|erreichbar|verfügbar|gestartet|beendet|` +
	`jetzt|heute|immer|wieder|alle|allen|jede|jeder|jedes|zwischen|über|unter|ohne|seit|bis|wenn|dann|sonst|weil|` +
	`dass|sich|haben|habe|sein|diese|dieser|dieses|dieser|welche|welcher|mehr|weniger|neu|neue|neuer|neues|` +
	`gewechselt|geändert|ausgefallen|beeinträchtigt|quittiert|eskaliert|zeitplan|schlüssel|zertifikat|zertifikate|` +
	`betriebssystem|freigabe|freigaben|hinweis|einstellung|einstellungen|sekunden|minuten|stunden|tage|tagen|woche|` +
	`wochen|monat|monate|jahr|jahre|zeichen|zahl|zahlen|liste|namen|beschreibung|anzahl|dauer|frist|` +
	`gruppe|gruppen|rolle|rollen|rechte|berechtigung|sitzung|sitzungen|konto|konten|abfrage|abfragen|ausgabe|` +
	`ergebnis|ergebnisse|zustand|zustände|ziel|ziele|quelle|quellen|pfad|verzeichnis|speicher|belegt|frei|voll|` +
	`hochgeladen|heruntergeladen|herunterladen|hochladen|erstellen|erstellt|löschen|ändern|anlegen|prüfen|geprüft|` +
	`übersprungen|ignoriert|gelesen|geschrieben|gesendet|versendet|empfangen|zugestellt|abgelehnt|verweigert|` +
	`zeitüberschreitung|antwortet|meldet|läuft|gilt|fehlerhaft|vorhanden|erforderlich|möglich|unmöglich|aktuell|` +
	`gerätetyp|aufstellort|besitzer|notizen|kennung|anbieter|hostname-quelle|zugriff)\b`)

var umlaut = regexp.MustCompile(`[äöüÄÖÜß„“]`)

// looksGerman reports whether a literal is a German display text.
func looksGerman(s string) bool {
	return umlaut.MatchString(s) || germanWords.MatchString(s)
}

type literal struct {
	text string
	pos  token.Position
	pkg  string
}

// scanSources collects the German literals of all non-test Go files below root.
func scanSources(t *testing.T, root string) []literal {
	t.Helper()
	var out []literal
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "testdata", "node_modules", "dist", "i18n", "plugintest", "sshtest", "oidctest":
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "i18n_en.go" {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}
		out = append(out, scanFile(fset, f, src, filepath.ToSlash(filepath.Dir(path)))...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// logMethods take a message followed by key/value pairs (slog); the keys are code.
var logMethods = map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true}

func scanFile(fset *token.FileSet, f *ast.File, src []byte, dir string) []literal {
	lines := strings.Split(string(src), "\n")
	ignored := map[int]bool{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, "i18n:ignore-file") {
				return nil
			}
			if strings.Contains(c.Text, "i18n:ignore") {
				pos := fset.Position(c.Slash)
				ignored[pos.Line] = true
				// a comment on a line of its own covers the next line, a trailing one only
				// its own
				if pos.Line-1 < len(lines) && strings.TrimSpace(lines[pos.Line-1][:pos.Column-1]) == "" {
					ignored[pos.Line+1] = true
				}
			}
		}
	}
	handled := map[ast.Node]bool{}
	var out []literal
	add := func(n ast.Node, text string) {
		pos := fset.Position(n.Pos())
		if ignored[pos.Line] || !looksGerman(text) {
			return
		}
		out = append(out, literal{text: text, pos: pos, pkg: pkgOf(dir)})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.ImportSpec:
			return false
		case *ast.Field:
			if x.Tag != nil {
				handled[x.Tag] = true
			}
		case *ast.IndexExpr:
			// m["key"]: a map key is data
			handled[x.Index] = true
		case *ast.CallExpr:
			// attribute keys of log calls: log.Info("Nachricht", "key", value, …)
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok && logMethods[sel.Sel.Name] {
				for i := 1; i < len(x.Args); i += 2 {
					if _, ok := strLit(x.Args[i]); ok {
						handled[x.Args[i]] = true
					}
				}
			}
		case *ast.BinaryExpr:
			if handled[x] || x.Op != token.ADD {
				return true
			}
			parts := flatten(x)
			hasLit := false
			for _, p := range parts {
				if _, ok := strLit(p); ok {
					hasLit = true
				}
			}
			if !hasLit {
				return true
			}
			var b strings.Builder
			for _, p := range parts {
				if s, ok := strLit(p); ok {
					b.WriteString(strings.ReplaceAll(s, "%", "%%"))
					handled[p] = true
				} else {
					b.WriteString("%s")
				}
			}
			markBinary(x, handled)
			pattern := b.String()
			// a concatenation of literals only is a plain text
			if !strings.Contains(strings.ReplaceAll(pattern, "%%", ""), "%s") {
				pattern = strings.ReplaceAll(pattern, "%%", "%")
			}
			add(x, pattern)
		case *ast.BasicLit:
			if handled[x] {
				return true
			}
			if s, ok := strLit(x); ok {
				add(x, s)
			}
		}
		return true
	})
	return out
}

func flatten(e ast.Expr) []ast.Expr {
	switch x := e.(type) {
	case *ast.BinaryExpr:
		if x.Op == token.ADD {
			return append(flatten(x.X), flatten(x.Y)...)
		}
	case *ast.ParenExpr:
		return flatten(x.X)
	}
	return []ast.Expr{e}
}

func markBinary(e ast.Expr, handled map[ast.Node]bool) {
	if x, ok := e.(*ast.BinaryExpr); ok && x.Op == token.ADD {
		handled[x] = true
		markBinary(x.X, handled)
		markBinary(x.Y, handled)
	}
}

func strLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

func pkgOf(dir string) string {
	if i := strings.Index(dir, "/internal/"); i >= 0 {
		return dir[i+1:]
	}
	if i := strings.Index(dir, "/cmd/"); i >= 0 {
		return dir[i+1:]
	}
	return dir
}

// TestCatalogProblems fails on conflicting or invalid registrations.
func TestCatalogProblems(t *testing.T) {
	for _, p := range i18n.Problems() {
		t.Error(p)
	}
}

func TestCatalogCoverage(t *testing.T) {
	var lits []literal
	// cmd/netscope (package main) has its own test for the CLI texts
	for _, root := range []string{"../../internal"} {
		lits = append(lits, scanSources(t, root)...)
	}
	missing := map[string][]string{}
	seen := map[string]bool{}
	for _, l := range lits {
		if i18n.Has(l.text) || seen[l.pkg+"\x00"+l.text] {
			continue
		}
		seen[l.pkg+"\x00"+l.text] = true
		missing[l.pkg] = append(missing[l.pkg], l.text)
		t.Errorf("%s:%d: no English translation for %q", l.pos.Filename, l.pos.Line, l.text)
	}
	dumpMissing(t, missing)
}

// TestStructuredTexts checks the catalogs the API delivers field by field.
func TestStructuredTexts(t *testing.T) {
	missing := map[string][]string{}
	need := func(where, s string) {
		if s == "" || i18n.Has(s) {
			return
		}
		missing[where] = append(missing[where], s)
		t.Errorf("%s: no English translation for %q", where, s)
	}
	schema := func(where string, s plugin.Schema) {
		for _, f := range s.Fields {
			need(where, f.Label)
			need(where, f.Description)
			need(where, f.Group)
			for _, o := range f.Options {
				need(where, o.Label)
			}
			if looksGerman(f.Placeholder) {
				need(where, f.Placeholder)
			}
		}
	}
	for _, p := range plugin.All() {
		info := p.Info()
		where := "plugin " + info.ID
		need(where, info.Name)
		need(where, info.Description)
		schema(where, p.Schema())
		if ap, ok := p.(plugin.ActionProvider); ok {
			for _, a := range ap.Actions() {
				need(where, a.Label)
				need(where, a.Description)
				need(where, a.Confirm)
				schema(where, plugin.Schema{Fields: a.Params})
			}
		}
	}
	for _, e := range plugin.Catalog() {
		need("event "+e.Type, e.Label)
		need("event "+e.Type, e.Description)
		for _, f := range e.Payload {
			need("event "+e.Type, f.Description)
		}
	}
	for _, s := range plugin.Severities {
		need("severity", s.Label())
	}
	for _, c := range plugin.CredentialTypes() {
		need("credential "+c.Type, c.Label)
		need("credential "+c.Type, c.Description)
		schema("credential "+c.Type, c.Schema)
	}
	for _, p := range auth.Permissions {
		need("permission "+p.Key, p.Group)
		need("permission "+p.Key, p.Label)
		need("permission "+p.Key, p.Hint)
	}
	for _, q := range inventory.QueryFields() {
		if looksGerman(q.Field) {
			need("query field", q.Field)
		}
		need("query field "+q.Field, q.Description)
	}
	dumpMissing(t, missing)
}

func dumpMissing(t *testing.T, missing map[string][]string) {
	path := os.Getenv("I18N_MISSING")
	if path == "" || len(missing) == 0 {
		return
	}
	for k := range missing {
		sort.Strings(missing[k])
	}
	var prev map[string][]string
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &prev)
	}
	for k, v := range prev {
		if _, ok := missing[k]; !ok {
			missing[k] = v
		}
	}
	b, _ := json.MarshalIndent(missing, "", "  ")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Log(err)
	}
}
