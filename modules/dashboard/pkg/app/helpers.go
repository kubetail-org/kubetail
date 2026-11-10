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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"

	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
)

const k8sTokenSessionKey = "k8sToken"

const k8sTokenGinKey = "k8sToken"

const csrfTokenSessionKey = "csrfToken"

// newClusterAPIProxy
func newClusterAPIProxy(cfg *config.Config, cm k8shelpers.ConnectionManager, pathPrefix string) (clusterapi.Proxy, error) {
	// Initialize new ClusterAPI proxy depending on environment
	switch cfg.Environment {
	case sharedcfg.EnvironmentDesktop:
		return clusterapi.NewDesktopProxy(cm, pathPrefix, cfg.AllowedOrigins)
	case sharedcfg.EnvironmentCluster:
		if !cfg.ClusterAPIEnabled {
			return nil, nil
		}
		return clusterapi.NewInClusterProxy(cm, pathPrefix, cfg.AllowedOrigins)
	default:
		return nil, fmt.Errorf("env not supported: %s", cfg.Environment)
	}
}

// queryHelpers interface
type queryHelpers interface {
	HasAccess(ctx context.Context, token string) (*authv1.TokenReview, error)
}

// Represents implementation of queryHelpers
type realQueryHelpers struct {
	cm k8shelpers.ConnectionManager
}

// Create new k8sQueryHelpers instance
func newRealQueryHelpers(cm k8shelpers.ConnectionManager) *realQueryHelpers {
	return &realQueryHelpers{cm}
}

// HasAccess
func (qh *realQueryHelpers) HasAccess(ctx context.Context, token string) (*authv1.TokenReview, error) {
	// Get client
	clientset, err := qh.cm.GetOrCreateClientset("")
	if err != nil {
		return nil, err
	}

	// Use Token service
	tokenReview := &authv1.TokenReview{
		Spec: authv1.TokenReviewSpec{
			Token: token,
		},
	}

	// Execute
	return clientset.AuthenticationV1().TokenReviews().Create(ctx, tokenReview, metav1.CreateOptions{})
}

// sessionKeysSecretName is the Secret, in the Dashboard's own namespace, that
// holds the session key pairs in the cluster environment. The Dashboard's Role
// grants get on this name alone, so renaming it changes the RBAC every
// deployment needs.
const sessionKeysSecretName = "kubetail-dashboard-session-keys"

// sessionKeysSecretKey is the Secret's data key; its value has the format of
// the desktop key file.
const sessionKeysSecretKey = "key-pairs.json"

// serviceAccountNamespacePath holds the namespace of the pod's ServiceAccount.
const serviceAccountNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// resolveSessionKeyPairs returns the flat [][]byte pairs expected by
// cookie.NewStore: [signingKey0, encryptionKey0, signingKey1, encryptionKey1, ...].
// Explicit config key-pairs win. Otherwise the pairs persist where every
// process of the environment finds them: a file in desktop, the Secret
// sessionKeysSecretName in cluster, read with the Dashboard's own credentials.
func resolveSessionKeyPairs(ctx context.Context, cfg *config.Config, cm k8shelpers.ConnectionManager, namespacePath string) ([][]byte, error) {
	pairs := cfg.Session.KeyPairs
	if len(pairs) == 0 {
		var err error
		switch cfg.Environment {
		case sharedcfg.EnvironmentDesktop:
			pairs, err = loadOrCreateKeyPairs(cfg.SessionKeysPath())
		case sharedcfg.EnvironmentCluster:
			pairs, err = loadOrCreateSecretKeyPairs(ctx, cm, namespacePath)
		default:
			err = fmt.Errorf("env not supported: %s", cfg.Environment)
		}
		if err != nil {
			return nil, err
		}
	}

	result := make([][]byte, 0, len(pairs)*2)
	for i, kp := range pairs {
		sk := decodeOrDeriveKey(kp.SigningKey, "signing-key", i, true)

		var ek []byte
		if kp.EncryptionKey != "" {
			ek = decodeOrDeriveKey(kp.EncryptionKey, "encryption-key", i, false)
		}
		// gorilla/securecookie treats a non-nil block key as AES input, so an
		// empty (but non-nil) slice would be rejected as an invalid AES key.
		if len(ek) == 0 {
			ek = nil
		}
		result = append(result, sk, ek)
	}
	return result, nil
}

// decodeOrDeriveKey returns hex-decoded bytes when input is valid hex of an
// acceptable length; otherwise it derives a deterministic 32-byte key via
// SHA-256 and logs a warning recommending the user switch to hex.
// allowAnyLen controls whether non-AES lengths are accepted from hex input
// (true for signing keys, false for encryption keys which must be 16/24/32).
func decodeOrDeriveKey(input, label string, pairIndex int, allowAnyLen bool) []byte {
	if decoded, err := hex.DecodeString(input); err == nil {
		if allowAnyLen || validAESKeySize(len(decoded)) {
			return decoded
		}
	}
	sum := sha256.Sum256([]byte(input))
	zlog.Warn().
		Int("pairIndex", pairIndex).
		Str("field", label).
		Msgf("session key-pair %s is not valid hex; deriving 32-byte key via SHA-256. For best results, configure a hex-encoded key.", label)
	return sum[:]
}

// persistedKeyPair is the on-disk representation of a key pair. It extends
// config.KeyPair with a creation timestamp so future rotation logic can
// decide when to generate a new primary pair.
type persistedKeyPair struct {
	config.KeyPair
	CreatedAt time.Time `json:"created-at"`
}

// validAESKeySize reports whether n is an acceptable AES key length (0 = disabled, or 16/24/32).
func validAESKeySize(n int) bool {
	return n == 0 || n == 16 || n == 24 || n == 32
}

// validPersistedKeyPairs returns true if all key pairs are hex-decodable and
// their encryption keys (when present) are valid AES key sizes.
func validPersistedKeyPairs(pairs []persistedKeyPair) bool {
	for _, p := range pairs {
		if _, err := hex.DecodeString(p.SigningKey); err != nil {
			return false
		}
		ek, err := hex.DecodeString(p.EncryptionKey)
		if err != nil {
			return false
		}
		if !validAESKeySize(len(ek)) {
			return false
		}
	}
	return true
}

// decodePersistedKeyPairs parses the persisted JSON format, reporting false
// when data holds no valid key pair.
func decodePersistedKeyPairs(data []byte) ([]config.KeyPair, bool) {
	var persisted []persistedKeyPair
	if json.Unmarshal(data, &persisted) != nil || len(persisted) == 0 || !validPersistedKeyPairs(persisted) {
		return nil, false
	}
	pairs := make([]config.KeyPair, len(persisted))
	for i, p := range persisted {
		pairs[i] = p.KeyPair
	}
	return pairs, true
}

// newPersistedKeyPairs generates a single random key pair and its persisted
// JSON form.
func newPersistedKeyPairs() ([]config.KeyPair, []byte, error) {
	kp, err := randomKeyPair()
	if err != nil {
		return nil, nil, err // coverage:ignore crypto/rand.Read does not fail
	}
	data, err := json.Marshal([]persistedKeyPair{{KeyPair: kp, CreatedAt: time.Now().UTC()}})
	if err != nil {
		return nil, nil, err // coverage:ignore a struct of strings and a time always marshals
	}
	return []config.KeyPair{kp}, data, nil
}

// loadOrCreateKeyPairs reads the JSON key-pair file at path, or generates and
// persists a new single key pair if the file is missing or unreadable.
func loadOrCreateKeyPairs(path string) ([]config.KeyPair, error) {
	if data, err := os.ReadFile(path); err == nil {
		if pairs, ok := decodePersistedKeyPairs(data); ok {
			return pairs, nil
		}
	}

	pairs, data, err := newPersistedKeyPairs()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return pairs, nil
}

// loadOrCreateSecretKeyPairs reads the key pairs from the Secret
// sessionKeysSecretName in the namespace named in the file at namespacePath,
// creating the Secret with a new random pair if it does not exist. Replicas
// that start together race to create it; the losers read the winner's.
func loadOrCreateSecretKeyPairs(ctx context.Context, cm k8shelpers.ConnectionManager, namespacePath string) ([]config.KeyPair, error) {
	namespace, err := os.ReadFile(namespacePath)
	if err != nil {
		return nil, fmt.Errorf("reading the Dashboard's namespace: %w", err)
	}
	clientset, err := cm.GetOrCreateClientset("")
	if err != nil {
		return nil, err
	}
	secrets := clientset.CoreV1().Secrets(strings.TrimSpace(string(namespace)))

	secret, err := secrets.Get(ctx, sessionKeysSecretName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		var pairs []config.KeyPair
		var data []byte
		pairs, data, err = newPersistedKeyPairs()
		if err != nil {
			return nil, err
		}
		_, err = secrets.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: sessionKeysSecretName,
				Labels: map[string]string{
					"app.kubernetes.io/name":      "kubetail",
					"app.kubernetes.io/component": "dashboard",
				},
			},
			Data: map[string][]byte{sessionKeysSecretKey: data},
		}, metav1.CreateOptions{})
		if err == nil {
			return pairs, nil
		}
		if k8serrors.IsAlreadyExists(err) {
			secret, err = secrets.Get(ctx, sessionKeysSecretName, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, fmt.Errorf("loading session keys from Secret %s: %w", sessionKeysSecretName, err)
	}

	// Replacing an invalid Secret here would race the other replicas, so a
	// person deletes it and the next start makes a new one.
	pairs, ok := decodePersistedKeyPairs(secret.Data[sessionKeysSecretKey])
	if !ok {
		return nil, fmt.Errorf("secret %s has no valid %s: delete it to generate new session keys", sessionKeysSecretName, sessionKeysSecretKey)
	}
	return pairs, nil
}

// randomKeyPair generates a new key pair with 32-byte random signing and
// encryption keys, hex-encoded for safe JSON/YAML storage.
func randomKeyPair() (config.KeyPair, error) {
	sk := make([]byte, 32)
	if _, err := rand.Read(sk); err != nil {
		return config.KeyPair{}, err
	}
	ek := make([]byte, 32)
	if _, err := rand.Read(ek); err != nil {
		return config.KeyPair{}, err
	}
	return config.KeyPair{
		SigningKey:    hex.EncodeToString(sk),
		EncryptionKey: hex.EncodeToString(ek),
	}, nil
}
