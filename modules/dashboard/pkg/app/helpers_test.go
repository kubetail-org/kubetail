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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	k8shelpersmock "github.com/kubetail-org/kubetail/modules/shared/k8shelpers/mock"

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

		result, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 32)
		assert.Len(t, result[1], 32)
	})

	t.Run("derives signing key when not hex", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: "my passphrase", EncryptionKey: validEK}}

		result, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 32)

		expected := sha256.Sum256([]byte("my passphrase"))
		assert.Equal(t, expected[:], result[0])
	})

	t.Run("derives encryption key when not hex", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: "another passphrase"}}

		result, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[1], 32)

		expected := sha256.Sum256([]byte("another passphrase"))
		assert.Equal(t, expected[:], result[1])
	})

	t.Run("derives encryption key when hex but wrong length", func(t *testing.T) {
		cfg := newTestConfig()
		shortHex := hex.EncodeToString(make([]byte, 10))
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: shortHex}}

		result, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[1], 32)

		expected := sha256.Sum256([]byte(shortHex))
		assert.Equal(t, expected[:], result[1])
	})

	t.Run("derivation is deterministic", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: "passphrase-a", EncryptionKey: "passphrase-b"}}

		r1, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		r2, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		assert.Equal(t, r1, r2)
	})

	t.Run("empty encryption key stays nil", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = []config.KeyPair{{SigningKey: validSK, EncryptionKey: ""}}

		result, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Nil(t, result[1])
	})

	t.Run("loads the Secret in cluster when no keys are configured", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = nil
		clientset := fake.NewClientset()
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", "").Return(clientset, nil)

		result, err := resolveSessionKeyPairs(t.Context(), cfg, cm, writeNamespaceFile(t, "kubetail"))
		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Len(t, result[0], 32)
		assert.Len(t, result[1], 32)

		_, err = clientset.CoreV1().Secrets("kubetail").Get(t.Context(), sessionKeysSecretName, metav1.GetOptions{})
		assert.NoError(t, err)
	})

	t.Run("returns the Secret's error in cluster", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = nil

		_, err := resolveSessionKeyPairs(t.Context(), cfg, nil, filepath.Join(t.TempDir(), "missing"))
		assert.ErrorContains(t, err, "reading the Dashboard's namespace")
	})

	t.Run("rejects an unknown environment", func(t *testing.T) {
		cfg := newTestConfig()
		cfg.Session.KeyPairs = nil
		cfg.Environment = ""

		_, err := resolveSessionKeyPairs(t.Context(), cfg, nil, "")
		assert.ErrorContains(t, err, "env not supported")
	})
}

// writeNamespaceFile writes namespace to a file laid out like the pod's
// ServiceAccount namespace file, and returns its path.
func writeNamespaceFile(t *testing.T, namespace string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "namespace")
	require.NoError(t, os.WriteFile(path, []byte(namespace), 0600))
	return path
}

func newSessionKeysSecret(namespace string, data []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: sessionKeysSecretName, Namespace: namespace},
		Data:       map[string][]byte{sessionKeysSecretKey: data},
	}
}

func TestLoadOrCreateSecretKeyPairs(t *testing.T) {
	newCM := func(clientset *fake.Clientset) *k8shelpersmock.MockConnectionManager {
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", "").Return(clientset, nil)
		return cm
	}

	t.Run("creates the Secret and reuses it on the next start", func(t *testing.T) {
		clientset := fake.NewClientset()
		cm := newCM(clientset)
		nsPath := writeNamespaceFile(t, "kubetail\n")

		first, err := loadOrCreateSecretKeyPairs(t.Context(), cm, nsPath)
		require.NoError(t, err)
		require.Len(t, first, 1)

		secret, err := clientset.CoreV1().Secrets("kubetail").Get(t.Context(), sessionKeysSecretName, metav1.GetOptions{})
		require.NoError(t, err)
		assert.Contains(t, secret.Data, sessionKeysSecretKey)

		second, err := loadOrCreateSecretKeyPairs(t.Context(), cm, nsPath)
		require.NoError(t, err)
		assert.Equal(t, first, second)
	})

	t.Run("loads an existing Secret", func(t *testing.T) {
		kp, err := randomKeyPair()
		require.NoError(t, err)
		data, err := json.Marshal([]persistedKeyPair{{KeyPair: kp, CreatedAt: time.Now().UTC()}})
		require.NoError(t, err)
		clientset := fake.NewClientset(newSessionKeysSecret("kubetail", data))

		pairs, err := loadOrCreateSecretKeyPairs(t.Context(), newCM(clientset), writeNamespaceFile(t, "kubetail"))
		require.NoError(t, err)
		assert.Equal(t, []config.KeyPair{kp}, pairs)
	})

	t.Run("reads the winner's Secret after losing the create race", func(t *testing.T) {
		kp, err := randomKeyPair()
		require.NoError(t, err)
		data, err := json.Marshal([]persistedKeyPair{{KeyPair: kp, CreatedAt: time.Now().UTC()}})
		require.NoError(t, err)

		// The first get misses; another replica's Secret lands before the create.
		clientset := fake.NewClientset()
		gets := 0
		clientset.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			gets++
			if gets == 1 {
				return true, nil, k8serrors.NewNotFound(corev1.Resource("secrets"), sessionKeysSecretName)
			}
			return false, nil, nil
		})
		require.NoError(t, clientset.Tracker().Add(newSessionKeysSecret("kubetail", data)))

		pairs, err := loadOrCreateSecretKeyPairs(t.Context(), newCM(clientset), writeNamespaceFile(t, "kubetail"))
		require.NoError(t, err)
		assert.Equal(t, []config.KeyPair{kp}, pairs)
		assert.Equal(t, 2, gets)
	})

	t.Run("refuses an invalid Secret", func(t *testing.T) {
		clientset := fake.NewClientset(newSessionKeysSecret("kubetail", []byte("not valid json")))

		_, err := loadOrCreateSecretKeyPairs(t.Context(), newCM(clientset), writeNamespaceFile(t, "kubetail"))
		assert.ErrorContains(t, err, "delete it to generate new session keys")
	})

	t.Run("returns a get error", func(t *testing.T) {
		clientset := fake.NewClientset()
		clientset.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, k8serrors.NewForbidden(corev1.Resource("secrets"), sessionKeysSecretName, nil)
		})

		_, err := loadOrCreateSecretKeyPairs(t.Context(), newCM(clientset), writeNamespaceFile(t, "kubetail"))
		assert.True(t, k8serrors.IsForbidden(err))
	})

	t.Run("returns a create error", func(t *testing.T) {
		clientset := fake.NewClientset()
		clientset.PrependReactor("create", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, k8serrors.NewForbidden(corev1.Resource("secrets"), sessionKeysSecretName, nil)
		})

		_, err := loadOrCreateSecretKeyPairs(t.Context(), newCM(clientset), writeNamespaceFile(t, "kubetail"))
		assert.True(t, k8serrors.IsForbidden(err))
	})

	t.Run("returns a clientset error", func(t *testing.T) {
		cm := &k8shelpersmock.MockConnectionManager{}
		cm.On("GetOrCreateClientset", "").Return(nil, errors.New("no clientset"))

		_, err := loadOrCreateSecretKeyPairs(t.Context(), cm, writeNamespaceFile(t, "kubetail"))
		assert.ErrorContains(t, err, "no clientset")
	})
}
