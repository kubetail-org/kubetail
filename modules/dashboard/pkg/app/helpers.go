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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	authv1 "k8s.io/api/authentication/v1"
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
		return clusterapi.NewDesktopProxy(cm, pathPrefix)
	case sharedcfg.EnvironmentCluster:
		return clusterapi.NewInClusterProxy(cfg.ClusterAPIEndpoint, pathPrefix)
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

// resolveSessionKeyPairs returns the flat [][]byte pairs expected by
// cookie.NewStore: [signingKey0, encryptionKey0, signingKey1, encryptionKey1, ...].
// Priority: explicit config key-pairs > persisted key file (desktop only) > random per startup.
func resolveSessionKeyPairs(cfg *config.Config) ([][]byte, error) {
	pairs := cfg.Session.KeyPairs
	if len(pairs) == 0 {
		if cfg.Environment == sharedcfg.EnvironmentDesktop {
			var err error
			pairs, err = loadOrCreateKeyPairs(cfg.SessionKeysPath())
			if err != nil {
				return nil, err
			}
		} else {
			kp, err := randomKeyPair()
			if err != nil {
				return nil, err
			}
			pairs = []config.KeyPair{kp}
		}
	}

	result := make([][]byte, 0, len(pairs)*2)
	for _, kp := range pairs {
		sk, err := hex.DecodeString(kp.SigningKey)
		if err != nil {
			return nil, fmt.Errorf("session key-pair signing-key must be hex-encoded: %w", err)
		}
		ek, err := hex.DecodeString(kp.EncryptionKey)
		if err != nil {
			return nil, fmt.Errorf("session key-pair encryption-key must be hex-encoded: %w", err)
		}
		if !validAESKeySize(len(ek)) {
			return nil, fmt.Errorf("session key-pair encryption-key must decode to 16, 24, or 32 bytes (got %d)", len(ek))
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

// loadOrCreateKeyPairs reads the JSON key-pair file at path, or generates and
// persists a new single key pair if the file is missing or unreadable.
func loadOrCreateKeyPairs(path string) ([]config.KeyPair, error) {
	if data, err := os.ReadFile(path); err == nil {
		var persisted []persistedKeyPair
		if json.Unmarshal(data, &persisted) == nil && len(persisted) > 0 && validPersistedKeyPairs(persisted) {
			pairs := make([]config.KeyPair, len(persisted))
			for i, p := range persisted {
				pairs[i] = p.KeyPair
			}
			return pairs, nil
		}
	}

	kp, err := randomKeyPair()
	if err != nil {
		return nil, err
	}
	persisted := []persistedKeyPair{{KeyPair: kp, CreatedAt: time.Now().UTC()}}

	data, err := json.Marshal(persisted)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return nil, err
	}
	return []config.KeyPair{kp}, nil
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
