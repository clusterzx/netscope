package snmp

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// credStore remembers which credential worked for which device (only ids, no secrets),
// so the next run tries that credential first.
type credStore struct {
	path  string
	mu    sync.Mutex
	m     map[int64]int64
	dirty bool
}

type credFile struct {
	Devices map[string]int64 `json:"devices"`
}

// loadCredStore reads the store; a missing or damaged file yields an empty store.
func loadCredStore(path string) (*credStore, error) {
	s := &credStore{path: path, m: map[int64]int64{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f credFile
	if err := json.Unmarshal(b, &f); err != nil {
		return s, err
	}
	for k, v := range f.Devices {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil && id > 0 && v > 0 {
			s.m[id] = v
		}
	}
	return s, nil
}

// get returns the remembered credential id of a device (0 = none).
func (s *credStore) get(device int64) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[device]
}

// set remembers the working credential of a device (0 forgets it).
func (s *credStore) set(device, cred int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m[device] == cred {
		return
	}
	if cred == 0 {
		delete(s.m, device)
	} else {
		s.m[device] = cred
	}
	s.dirty = true
}

// save writes the store atomically if it changed.
func (s *credStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	f := credFile{Devices: make(map[string]int64, len(s.m))}
	for k, v := range s.m {
		f.Devices[strconv.FormatInt(k, 10)] = v
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}

// ordered returns ids with the remembered credential of the device first.
func orderedCredentials[T any](preferred int64, list []T, id func(T) int64) []T {
	out := make([]T, 0, len(list))
	for _, c := range list {
		if id(c) == preferred {
			out = append(out, c)
		}
	}
	for _, c := range list {
		if id(c) != preferred {
			out = append(out, c)
		}
	}
	return out
}
