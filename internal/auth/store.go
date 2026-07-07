package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) Load() (*StoreFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (*StoreFile, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &StoreFile{Version: StoreVersion}, nil
		}
		return nil, err
	}
	var file StoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse auth store: %w", err)
	}
	if file.Version == 0 {
		file.Version = StoreVersion
	}
	return &file, nil
}

func (s *Store) Save(file *StoreFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(file)
}

func (s *Store) saveLocked(file *StoreFile) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	file.Version = StoreVersion
	file.UpdatedAt = nowUTC()
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".tmp.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func (s *Store) WithLock(fn func(*StoreFile) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	file, err := s.loadLocked()
	if err != nil {
		return err
	}
	if err := fn(file); err != nil {
		return err
	}
	return s.saveLocked(file)
}

func (s *Store) Credentials() (*Credentials, error) {
	file, err := s.Load()
	if err != nil {
		return nil, err
	}
	if file.Codex == nil || strings.TrimSpace(file.Codex.AccessToken) == "" {
		return nil, &AuthError{
			Message:         "No Codex credentials stored. Run `openai-proxy auth login`.",
			Code:            "codex_auth_missing",
			ReloginRequired: true,
			StatusCode:      401,
		}
	}
	return file.Codex, nil
}

func (s *Store) Clear() error {
	return s.WithLock(func(file *StoreFile) error {
		file.Codex = nil
		return nil
	})
}

func (s *Store) SaveCredentials(creds *Credentials) error {
	return s.WithLock(func(file *StoreFile) error {
		file.Codex = creds
		return nil
	})
}
