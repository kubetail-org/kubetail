// Copyright 2024 The Kubetail Authors
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

package app

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
)

func TestRandomKeyPair(t *testing.T) {
	kp, err := randomKeyPair()
	require.NoError(t, err)

	sk, err := hex.DecodeString(kp.SigningKey)
	require.NoError(t, err)
	assert.Len(t, sk, 32)

	ek, err := hex.DecodeString(kp.EncryptionKey)
	require.NoError(t, err)
	assert.Len(t, ek, 32)
}

func TestValidPersistedKeyPairs(t *testing.T) {
	sk := hex.EncodeToString(make([]byte, 32))
	ek32 := hex.EncodeToString(make([]byte, 32))
	ek16 := hex.EncodeToString(make([]byte, 16))

	tests := []struct {
		name  string
		pairs []persistedKeyPair
		want  bool
	}{
		{
			name:  "valid 32-byte keys",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: sk, EncryptionKey: ek32}}},
			want:  true,
		},
		{
			name:  "valid 16-byte encryption key",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: sk, EncryptionKey: ek16}}},
			want:  true,
		},
		{
			name:  "empty encryption key",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: sk, EncryptionKey: ""}}},
			want:  true,
		},
		{
			name:  "non-hex signing key",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: "not-hex!", EncryptionKey: ek32}}},
			want:  false,
		},
		{
			name:  "non-hex encryption key",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: sk, EncryptionKey: "not-hex!"}}},
			want:  false,
		},
		{
			name:  "bad encryption key length",
			pairs: []persistedKeyPair{{KeyPair: config.KeyPair{SigningKey: sk, EncryptionKey: hex.EncodeToString(make([]byte, 10))}}},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validPersistedKeyPairs(tt.pairs))
		})
	}
}

func TestLoadOrCreateKeyPairs(t *testing.T) {
	t.Run("creates file when missing", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "keys.json")

		pairs, err := loadOrCreateKeyPairs(path)
		require.NoError(t, err)
		require.Len(t, pairs, 1)

		_, err = os.Stat(path)
		assert.NoError(t, err)
	})

	t.Run("loads existing file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "keys.json")

		kp, _ := randomKeyPair()
		data, _ := json.Marshal([]persistedKeyPair{{KeyPair: kp, CreatedAt: time.Now().UTC()}})
		require.NoError(t, os.WriteFile(path, data, 0600))

		pairs, err := loadOrCreateKeyPairs(path)
		require.NoError(t, err)
		require.Len(t, pairs, 1)
		assert.Equal(t, kp.SigningKey, pairs[0].SigningKey)
	})

	t.Run("rebuilds corrupt file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "keys.json")

		require.NoError(t, os.WriteFile(path, []byte("not valid json"), 0600))

		pairs, err := loadOrCreateKeyPairs(path)
		require.NoError(t, err)
		require.Len(t, pairs, 1)

		data, err := os.ReadFile(path)
		require.NoError(t, err)
		var persisted []persistedKeyPair
		require.NoError(t, json.Unmarshal(data, &persisted))
	})

	t.Run("rebuilds file with invalid key sizes", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "keys.json")

		bad := []persistedKeyPair{{KeyPair: config.KeyPair{
			SigningKey:    hex.EncodeToString(make([]byte, 32)),
			EncryptionKey: hex.EncodeToString(make([]byte, 10)),
		}, CreatedAt: time.Now().UTC()}}
		data, _ := json.Marshal(bad)
		require.NoError(t, os.WriteFile(path, data, 0600))

		pairs, err := loadOrCreateKeyPairs(path)
		require.NoError(t, err)
		require.Len(t, pairs, 1)

		ek, err := hex.DecodeString(pairs[0].EncryptionKey)
		require.NoError(t, err)
		assert.Len(t, ek, 32)
	})
}

func TestResolveSessionKeyPairs(t *testing.T) {
	validSK := hex.EncodeToString(make([]byte, 32))
	validEK := hex.EncodeToString(make([]byte, 32))

	t.Run("returns decoded bytes for explicit config keys", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: validEK}}

		result, err := resolveSessionKeyPairs(cfg)
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 32)
		assert.Len(t, result[1], 32)
	})

	t.Run("errors on non-hex signing key", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: "not-hex!", EncryptionKey: validEK}}

		_, err := resolveSessionKeyPairs(cfg)
		assert.ErrorContains(t, err, "signing-key")
	})

	t.Run("errors on non-hex encryption key", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: "not-hex!"}}

		_, err := resolveSessionKeyPairs(cfg)
		assert.ErrorContains(t, err, "encryption-key")
	})

	t.Run("errors on invalid encryption key length", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: hex.EncodeToString(make([]byte, 10))}}

		_, err := resolveSessionKeyPairs(cfg)
		assert.ErrorContains(t, err, "encryption-key")
	})

	t.Run("generates random pair for cluster env when no keys configured", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = nil

		result, err := resolveSessionKeyPairs(cfg)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})
}
