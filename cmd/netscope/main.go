// Command netscope is the NetScope server and admin CLI.
//
//	netscope [serve]                          start the server
//	netscope token create --name N --scope S  create an API token (prints it once)
//	netscope passwd [--password P]            reset the admin password
//	netscope healthcheck                      exit 0 if the local server is healthy (Docker HEALTHCHECK)
//	netscope openapi                          print the OpenAPI document
//	netscope version                          print the version
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // time zones without system tzdata

	"netscope/internal/api"
	"netscope/internal/app"
	"netscope/internal/auth"
	"netscope/internal/config"
	"netscope/internal/db"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "token":
		err = token(args)
	case "passwd":
		err = passwd(args)
	case "2fa-reset":
		err = resetMFA(args)
	case "healthcheck":
		err = healthcheck()
	case "openapi":
		err = openapi()
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unbekannter Befehl %q", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `NetScope – Netzwerk-Inventar und Asset-Monitoring

Befehle:
  serve                                   Server starten (Standard)
  token create --name NAME --scope read|write [--ttl 24h]
                                          API-Token erzeugen (wird einmalig ausgegeben)
  passwd [--user NAME] [--password PASSWORT]
                                          Passwort zurücksetzen (Standard admin; sonst Eingabe über stdin)
  2fa-reset [--user NAME]                 zweiten Faktor (TOTP, Passkeys, Codes) eines Benutzers entfernen
  healthcheck                             prüft /api/v1/health des lokalen Servers
  openapi                                 OpenAPI-Spezifikation ausgeben
  version                                 Version ausgeben

Konfiguration: /data/config.yaml bzw. NETSCOPE_CONFIG, Überschreibungen per NETSCOPE_*.
`)
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	return app.Run(ctx, cfg, version)
}

func openDB() (*config.Config, *db.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := db.Open(ctx, cfg.DBPath())
	return cfg, d, err
}

func token(args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return fmt.Errorf("verwende: netscope token create --name NAME --scope read|write [--ttl 24h]")
	}
	fs := flag.NewFlagSet("token create", flag.ContinueOnError)
	name := fs.String("name", "", "Name des Tokens")
	scope := fs.String("scope", "read", "read oder write")
	ttl := fs.Duration("ttl", 0, "Gültigkeit (0 = unbegrenzt)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	_, d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()
	ctx := context.Background()
	svc := auth.New(d, nil)
	uid, err := svc.AdminID(ctx)
	if err != nil {
		return fmt.Errorf("kein Benutzer vorhanden – Server einmal starten: %w", err)
	}
	var exp *time.Time
	if *ttl > 0 {
		t := time.Now().Add(*ttl)
		exp = &t
	}
	plain, _, err := svc.CreateToken(ctx, uid, *name, *scope, exp)
	if err != nil {
		return err
	}
	fmt.Println(plain)
	return nil
}

func passwd(args []string) error {
	fs := flag.NewFlagSet("passwd", flag.ContinueOnError)
	pw := fs.String("password", "", "neues Passwort (sonst über stdin)")
	user := fs.String("user", "admin", "Benutzer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pw == "" {
		fmt.Fprint(os.Stderr, "Neues Passwort: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return err
		}
		*pw = strings.TrimRight(line, "\r\n")
	}
	cfg, d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()
	if err := auth.New(d, nil).ResetPassword(context.Background(), *user, *pw); err != nil {
		return err
	}
	_ = os.Remove(cfg.DataDir + "/" + app.InitialPasswordFile)
	fmt.Println("Passwort geändert, alle Sitzungen beendet.")
	return nil
}

func healthcheck() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return err
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	c := &http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://" + net.JoinHostPort(host, port) + "/api/v1/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Status %d", resp.StatusCode)
	}
	return nil
}

func openapi() error {
	s := api.New(api.Deps{Version: version})
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(s.Spec())
}

// resetMFA removes the second factors of a user who lost the device (and the recovery codes).
func resetMFA(args []string) error {
	fs := flag.NewFlagSet("2fa-reset", flag.ContinueOnError)
	user := fs.String("user", "admin", "Benutzer")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()
	if err := auth.New(d, nil).ResetMFAByName(context.Background(), *user); err != nil {
		return err
	}
	fmt.Println("Zweiter Faktor entfernt, alle Sitzungen beendet. Verlangt die Rolle 2FA, wird sie beim nächsten Login neu eingerichtet.")
	return nil
}
