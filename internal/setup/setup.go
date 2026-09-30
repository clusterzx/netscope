// Package setup keeps the state of the setup wizard (FR-013). Until the setup is
// completed, the instance belongs to whoever proves access to the host: NetScope writes a
// setup code to the log and to <data>/setup-code.txt, and the setup endpoints only answer
// with it (or with the session of the administrator the wizard created). No plugin runs
// before the setup is completed.
package setup

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"netscope/internal/db"
	"netscope/internal/settings"
)

// CodeFile holds the setup code until the setup is completed.
const CodeFile = "setup-code.txt"

// settingsKey stores the State.
const settingsKey = "setup"

// Modes of a completed setup.
const (
	ModeWizard    = "wizard"    // finished in the web interface
	ModeAutomatic = "automatic" // NETSCOPE_ADMIN_PASSWORD or an instance without web interface
	ModeMigration = "migration" // installation from before the wizard
)

// State is the stored setup state.
type State struct {
	Completed   bool       `json:"completed"`
	Mode        string     `json:"mode,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// ErrCompleted is returned by setup steps after the setup was completed.
var ErrCompleted = errors.New("Die Einrichtung ist bereits abgeschlossen")

// Service holds the setup state and the setup code.
type Service struct {
	db      *db.DB
	dataDir string

	mu    sync.RWMutex
	state State
	code  string
}

// Load reads the setup state.
func Load(ctx context.Context, d *db.DB, dataDir string) (*Service, error) {
	s := &Service{db: d, dataDir: dataDir}
	if _, err := settings.GetJSON(ctx, d.R, settingsKey, &s.state); err != nil {
		return nil, err
	}
	return s, nil
}

// Pending reports whether the setup still has to be completed.
func (s *Service) Pending() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.state.Completed
}

// State returns the current state.
func (s *Service) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// CodePath is the file holding the setup code.
func (s *Service) CodePath() string { return filepath.Join(s.dataDir, CodeFile) }

// PrepareCode loads the setup code from its file or creates a new one and writes it there
// (readable by the owner only). It returns the code for the log.
func (s *Service) PrepareCode() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b, err := os.ReadFile(s.CodePath()); err == nil {
		if c := normalize(string(b)); len(c) == codeLen {
			s.code = c
			return format(c), nil
		}
	}
	c, err := newCode()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dataDir, 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.CodePath(), []byte(format(c)+"\n"), 0o600); err != nil {
		return "", err
	}
	s.code = c
	return format(c), nil
}

// CheckCode compares a code in constant time (case, spaces and dashes do not matter).
func (s *Service) CheckCode(code string) bool {
	s.mu.RLock()
	want := s.code
	s.mu.RUnlock()
	got := normalize(code)
	return want != "" && len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// ErrCodeFileLeft is returned by Complete when the setup is completed but the code file
// could not be removed. It is only a warning: the code is no longer accepted anyway.
var ErrCodeFileLeft = errors.New("Einrichtungscode-Datei konnte nicht gelöscht werden")

// Complete stores the setup as completed and removes the code file. A code file that
// cannot be removed does not undo the completion (see ErrCodeFileLeft).
func (s *Service) Complete(ctx context.Context, mode string) error {
	now := time.Now().UTC()
	st := State{Completed: true, Mode: mode, CompletedAt: &now}
	if err := settings.SetJSON(ctx, s.db.W, settingsKey, st); err != nil {
		return err
	}
	s.mu.Lock()
	s.state, s.code = st, ""
	s.mu.Unlock()
	if err := os.Remove(s.CodePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %v", ErrCodeFileLeft, err)
	}
	return nil
}

// ---------------------------------------------------------------- code format

// alphabet without look-alikes (0/O, 1/I/L).
const (
	alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	codeLen  = 12
)

func newCode() (string, error) {
	b := make([]byte, codeLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// format groups the code in blocks of four: ABCD-EFGH-JKMN.
func format(c string) string {
	var parts []string
	for i := 0; i < len(c); i += 4 {
		parts = append(parts, c[i:min(i+4, len(c))])
	}
	return strings.Join(parts, "-")
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		if strings.ContainsRune(alphabet, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Warning reports whether an error of Complete leaves the setup completed (only the code
// file is left over).
func Warning(err error) bool { return errors.Is(err, ErrCodeFileLeft) }
