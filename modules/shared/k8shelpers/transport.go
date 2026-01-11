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

package k8shelpers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type BearerTokenRoundTripper struct {
	Transport http.RoundTripper
}

func (b *BearerTokenRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get token from context
	ctx := req.Context()
	token, ok := ctx.Value(K8STokenCtxKey).(string)
	if ok {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	// Call original transport
	return b.Transport.RoundTrip(req)
}

func NewBearerTokenRoundTripper(transport http.RoundTripper) *BearerTokenRoundTripper {
	return &BearerTokenRoundTripper{transport}
}

// Represents round tripper for service account tokens mounted locally
// at "/var/run/secrets/kubernetes.io/serviceaccount/token"
type InClusterSATRoundTripper struct {
	Transport   http.RoundTripper
	path        string
	refreshSkew time.Duration
	cachedToken string
	expiresAt   time.Time
	mu          sync.Mutex
}

// Implements interface's RoundTrip method
func (rt *InClusterSATRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Get token
	token, err := rt.getToken()
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	// Call original transport
	return rt.Transport.RoundTrip(req)
}

// Return cached token or get a new one if necessary
func (rt *InClusterSATRoundTripper) getToken() (string, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	token := rt.cachedToken
	exp := rt.expiresAt
	if token != "" && (exp.IsZero() || time.Until(exp) > rt.refreshSkew) {
		return token, nil
	}

	newToken, expiresAt, err := rt.readTokenFromFile()
	if err != nil {
		return "", err
	}

	rt.cachedToken = newToken
	rt.expiresAt = expiresAt

	return newToken, nil
}

// Read token from file
func (rt *InClusterSATRoundTripper) readTokenFromFile() (string, time.Time, error) {
	tokenBytes, err := os.ReadFile(rt.path)
	if err != nil {
		return "", time.Time{}, err
	}

	tokenString := strings.TrimSpace(string(tokenBytes))
	if tokenString == "" {
		return "", time.Time{}, fmt.Errorf("service account token file %s is empty", rt.path)
	}

	// Parse without validation (since we just want the claims)
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return "", time.Time{}, err
	}

	// Extract exp claim; legacy tokens may omit it
	exp, err := token.Claims.GetExpirationTime()
	if err != nil {
		if errors.Is(err, jwt.ErrTokenRequiredClaimMissing) {
			return tokenString, time.Time{}, nil
		}

		return "", time.Time{}, err
	}

	if exp == nil {
		return tokenString, time.Time{}, nil
	}

	return tokenString, exp.Time, nil
}

// Returns new instance of InClusterSATRoundTripper
func NewInClusterSATRoundTripper(transport http.RoundTripper) (*InClusterSATRoundTripper, error) {
	rt := &InClusterSATRoundTripper{
		Transport:   transport,
		path:        "/var/run/secrets/kubernetes.io/serviceaccount/token",
		refreshSkew: 1 * time.Minute,
	}

	// Populate cache
	_, err := rt.getToken()
	if err != nil {
		return nil, err
	}

	return rt, nil
}
