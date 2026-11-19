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
	"context"
	"sync"
	"time"

	zlog "github.com/rs/zerolog/log"
	authv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
)

// Represents Service Account Token
type ServiceAccountToken struct {
	clientset          kubernetes.Interface
	namespace          string
	name               string
	latestTokenRequest *authv1.TokenRequest

	shutdownCh chan struct{}

	mu sync.Mutex
}

// Create new ServiceAccountToken instance
func NewServiceAccountToken(ctx context.Context, clientset kubernetes.Interface, namespace string, name string, shutdownCh chan struct{}) (*ServiceAccountToken, error) {
	sat := &ServiceAccountToken{
		clientset:  clientset,
		namespace:  namespace,
		name:       name,
		shutdownCh: shutdownCh,
	}

	// Initialize
	err := sat.refreshToken_UNSAFE(ctx)
	if err != nil {
		return nil, err
	}

	// Refresh in background
	go sat.startBackgroundRefresh()

	return sat, nil
}

// Token
func (sat *ServiceAccountToken) Token(ctx context.Context) (string, error) {
	sat.mu.Lock()
	defer sat.mu.Unlock()

	if sat.latestTokenRequest.Status.ExpirationTimestamp.Time.Before(time.Now()) {
		if err := sat.refreshToken_UNSAFE(ctx); err != nil {
			return "", err
		}
	}
	return sat.latestTokenRequest.Status.Token, nil
}

// Refresh the token
func (sat *ServiceAccountToken) refreshToken_UNSAFE(ctx context.Context) error {
	// Prepare the TokenRequest object
	tokenRequest := &authv1.TokenRequest{
		Spec: authv1.TokenRequestSpec{
			ExpirationSeconds: ptr.To[int64](3600), // Token validity (e.g., 1 hour)
		},
	}

	// Request a token for the ServiceAccount
	val, err := sat.clientset.CoreV1().ServiceAccounts(sat.namespace).CreateToken(ctx, sat.name, tokenRequest, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	sat.latestTokenRequest = val

	return nil
}

// Start background refresh process
func (sat *ServiceAccountToken) startBackgroundRefresh() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sat.mu.Lock()
	tr := sat.latestTokenRequest
	sat.mu.Unlock()

	initialDelay := time.Until(tr.Status.ExpirationTimestamp.Time) / 2

	// Refresh loop
	go func() {
		select {
		case <-time.After(initialDelay):
			// Continue
		case <-ctx.Done():
			// Exit
			return
		}

	Loop:
		for {
			// Refresh token
			ctxChild, cancel := context.WithTimeout(ctx, 10*time.Second)

			sat.mu.Lock()
			err := sat.refreshToken_UNSAFE(ctxChild)
			sat.mu.Unlock()

			if err != nil {
				zlog.Error().Caller().Err(err).Send()
			}
			cancel()

			// Exit if parent context was canceled
			if ctx.Err() != nil {
				break Loop
			}

			// Calculate sleep time
			sleepTime := time.Duration(30 * time.Second)

			sat.mu.Lock()
			tr := sat.latestTokenRequest
			sat.mu.Unlock()

			if tr != nil {
				t := time.Until(tr.Status.ExpirationTimestamp.Time) / 2
				if t > 30*time.Second {
					sleepTime = t
				}
			}

			// Wait with context awareness
			select {
			case <-time.After(sleepTime):
				// Continue after sleep
			case <-ctx.Done():
				// Exit loop if context is canceled
				break Loop
			}
		}
	}()

	// Wait for shutdown signal
	<-sat.shutdownCh
}
