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
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_Load_NoFile(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(filepath.Join(dir, "prefs.json"))

	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, p.Version)
	require.NotNil(t, p.Theme)
	assert.Equal(t, "system", *p.Theme)
	require.NotNil(t, p.Timezone)
	assert.Equal(t, "UTC", *p.Timezone)
}

func TestStore_Load_ValidFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")
	err := os.WriteFile(fp, []byte(`{"version":1,"theme":"dark","timezone":"America/New_York"}`), 0644)
	require.NoError(t, err)

	s := NewStore(fp)
	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, p.Version)
	require.NotNil(t, p.Theme)
	assert.Equal(t, "dark", *p.Theme)
	require.NotNil(t, p.Timezone)
	assert.Equal(t, "America/New_York", *p.Timezone)
}

func TestStore_Load_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")
	err := os.WriteFile(fp, []byte(`{not valid json`), 0644)
	require.NoError(t, err)

	s := NewStore(fp)
	p, err := s.Load()
	require.NoError(t, err)

	// should return defaults
	assert.Equal(t, CurrentVersion, p.Version)
	require.NotNil(t, p.Theme)
	assert.Equal(t, "system", *p.Theme)

	// original file should be renamed to .bak
	_, err = os.Stat(fp + ".bak")
	assert.NoError(t, err)
	_, err = os.Stat(fp)
	assert.True(t, os.IsNotExist(err))
}

func TestStore_Load_MissingVersion(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")
	err := os.WriteFile(fp, []byte(`{"theme":"dark"}`), 0644)
	require.NoError(t, err)

	s := NewStore(fp)
	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, p.Version)
	require.NotNil(t, p.Theme)
	assert.Equal(t, "dark", *p.Theme)
}

func TestStore_Get_CachesResult(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")
	err := os.WriteFile(fp, []byte(`{"version":1,"theme":"dark"}`), 0644)
	require.NoError(t, err)

	s := NewStore(fp)
	p1, err := s.Get()
	require.NoError(t, err)

	// overwrite file on disk
	err = os.WriteFile(fp, []byte(`{"version":1,"theme":"light"}`), 0644)
	require.NoError(t, err)

	// second Get should return cached value
	p2, err := s.Get()
	require.NoError(t, err)
	assert.Equal(t, *p1.Theme, *p2.Theme)
	assert.Equal(t, "dark", *p2.Theme)
}

func TestStore_Update_MergesAndWritesAtomically(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")
	err := os.WriteFile(fp, []byte(`{"version":1,"theme":"dark","timezone":"UTC"}`), 0644)
	require.NoError(t, err)

	s := NewStore(fp)
	light := "light"
	tz := "Europe/London"
	result, err := s.Update(&Preferences{Theme: &light, Timezone: &tz})
	require.NoError(t, err)
	require.NotNil(t, result.Theme)
	assert.Equal(t, "light", *result.Theme)
	require.NotNil(t, result.Timezone)
	assert.Equal(t, "Europe/London", *result.Timezone)
	assert.Equal(t, CurrentVersion, result.Version)

	// verify file on disk
	data, err := os.ReadFile(fp)
	require.NoError(t, err)
	var ondisk Preferences
	err = json.Unmarshal(data, &ondisk)
	require.NoError(t, err)
	require.NotNil(t, ondisk.Theme)
	assert.Equal(t, "light", *ondisk.Theme)
	require.NotNil(t, ondisk.Timezone)
	assert.Equal(t, "Europe/London", *ondisk.Timezone)
	assert.Equal(t, CurrentVersion, ondisk.Version)
}

func TestStore_Update_CreatesDirIfNeeded(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "subdir", "prefs.json")

	s := NewStore(fp)
	dark := "dark"
	result, err := s.Update(&Preferences{Theme: &dark})
	require.NoError(t, err)
	require.NotNil(t, result.Theme)
	assert.Equal(t, "dark", *result.Theme)

	// verify file exists
	_, err = os.Stat(fp)
	assert.NoError(t, err)
}

func TestStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "prefs.json")

	s := NewStore(fp)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = s.Get()
		}()
		go func(theme string) {
			defer wg.Done()
			_, _ = s.Update(&Preferences{Theme: &theme})
		}("dark")
	}
	wg.Wait()

	// should not panic or corrupt; final state should be valid
	p, err := s.Load()
	require.NoError(t, err)
	assert.Equal(t, CurrentVersion, p.Version)
	require.NotNil(t, p.Theme)
}
