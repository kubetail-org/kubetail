// Copyright 2024-2025 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package preferences

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	zlog "github.com/rs/zerolog/log"
)

// Store provides thread-safe, file-backed preferences storage.
type Store struct {
	mu       sync.RWMutex
	filePath string
	cache    *Preferences
}

// NewStore creates a Store that reads from and writes to filePath.
func NewStore(filePath string) *Store {
	return &Store{filePath: filePath}
}

// Load reads preferences from disk. If the file does not exist,
// defaults are returned. If the file contains malformed JSON it is
// renamed to .bak and defaults are returned.
func (s *Store) Load() (*Preferences, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultPreferences(), nil
		}
		return nil, err
	}

	var p Preferences
	if err := json.Unmarshal(data, &p); err != nil {
		bakPath := s.filePath + ".bak"
		if renameErr := os.Rename(s.filePath, bakPath); renameErr != nil {
			zlog.Warn().Err(renameErr).Msg("failed to rename malformed preferences file")
		} else {
			zlog.Warn().Str("backup", bakPath).Msg("renamed malformed preferences file")
		}
		return DefaultPreferences(), nil
	}

	if p.Version == 0 {
		p.Version = CurrentVersion
	}

	return &p, nil
}

// Get returns cached preferences, loading from disk on first call.
func (s *Store) Get() (*Preferences, error) {
	s.mu.RLock()
	if s.cache != nil {
		defer s.mu.RUnlock()
		return s.cache, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	// double-check after acquiring write lock
	if s.cache != nil {
		return s.cache, nil
	}

	p, err := s.Load()
	if err != nil {
		return nil, err
	}
	s.cache = p
	return s.cache, nil
}

// Update merges patch into the current preferences, writes the result
// atomically to disk, and updates the cache.
func (s *Store) Update(patch *Preferences) (*Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	current := s.cache
	if current == nil {
		p, err := s.Load()
		if err != nil {
			return nil, err
		}
		current = p
	}

	merged := Merge(current, patch)

	if err := s.writeAtomic(merged); err != nil {
		return nil, err
	}

	s.cache = merged
	return merged, nil
}

// writeAtomic writes preferences to a temp file and renames it into
// place. The caller must hold s.mu.
func (s *Store) writeAtomic(p *Preferences) error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".preferences-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	return os.Rename(tmpName, s.filePath)
}
