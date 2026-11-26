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

package catalog

import (
	"maps"
	"net/http"
	"sync"
	"testing"

	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
)

// credentials is what newCredentials builds the service from.
type credentials struct {
	// keys is the environment's key per provider id.
	keys, baseURLs map[string]string
	vertexBaseURL  string
	// connected is each provider whose connector has a session.
	connected []string
	// connectors replaces the connector of each provider it names.
	connectors map[string]providercredentials.Connector
}

// newCredentials is every provider's credentials service over an empty
// keyring, closed at the test's end.
func newCredentials(t *testing.T, c credentials) *providercredentials.Service {
	t.Helper()
	connectors := map[string]providercredentials.Connector{}
	for _, id := range c.connected {
		connectors[id] = sessionConnector{planSigner{token: id}}
	}
	maps.Copy(connectors, c.connectors)
	s, err := providercredentials.New(providercredentials.Config{
		Keyring:    oskeyringtest.New(),
		Providers:  Providers(c.baseURLs, c.vertexBaseURL),
		EnvKeys:    c.keys,
		Connectors: connectors,
	})
	require.NoError(t, err)
	t.Cleanup(s.Close)
	testutil.Wait(t, s.Restored(), "the restore")
	return s
}

// newKeys is newCredentials holding keys as the environment's, at the base
// URLs a debug build moves.
func newKeys(t *testing.T, keys, baseURLs map[string]string) *providercredentials.Service {
	t.Helper()
	return newCredentials(t, credentials{keys: keys, baseURLs: baseURLs})
}

// sessionConnector is a connector with a session for its whole life.
type sessionConnector struct{ planSigner }

func (sessionConnector) WatchSessionGenerationState() (<-chan uint64, func()) {
	rx := watch.New[uint64](1).Receiver()
	return rx.Chan(), rx.Close
}

func (sessionConnector) Restored() <-chan struct{} { return restored }

func (sessionConnector) RestoredSession() uint64 { return 1 }

// restored is a connector's Restored once its restore has published.
var restored = func() chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}()

// switchConnector is a connector the test connects and disconnects. It signs
// with its token while it has a session.
type switchConnector struct {
	token    string
	sessions *watch.Hub[uint64]

	mu      sync.Mutex
	session uint64
}

func newSwitchConnector(token string) *switchConnector {
	return &switchConnector{token: token, sessions: watch.New[uint64](0)}
}

// connect starts session gen; 0 ends it.
func (c *switchConnector) connect(gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = gen
	c.sessions.Sender().Send(gen) //nolint:errcheck // never closed
}

func (c *switchConnector) Sign(r *http.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == 0 {
		return llm.ErrSignedOut
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}

func (*switchConnector) Refused(*http.Request) {}

// Bind is the connector itself: the catalog's reads sign unbound.
func (c *switchConnector) Bind() llm.Signer { return c }

func (*switchConnector) Restored() <-chan struct{} { return restored }

// RestoredSession is 0: the test connects after the restore.
func (*switchConnector) RestoredSession() uint64 { return 0 }

func (c *switchConnector) WatchSessionGenerationState() (<-chan uint64, func()) {
	rx := c.sessions.Receiver()
	return rx.Chan(), rx.Close
}
