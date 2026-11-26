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

package providercredentials

import (
	"net/http"
	"sync"
	"testing"

	"github.com/amorey/gochan/watch"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

// Two keyed providers, one with a connector's place, one with two routes, and
// one its connector alone signs.
const (
	routerID  = "router"
	twinID    = "twin"
	planID    = "plan"
	routerAPI = "https://router.test/api/v1"
)

var (
	routerProvider = Provider{ID: routerID, Label: "Router", KeyVar: "ROUTER_API_KEY", Routes: []Route{
		{BaseURL: routerAPI, Header: "Authorization", Prefix: "Bearer "},
	}}
	twinProvider = Provider{ID: twinID, Label: "Twin", KeyVar: "TWIN_API_KEY", Routes: []Route{
		{BaseURL: "https://twin.test/openai/v1", Header: "Authorization", Prefix: "Bearer "},
		{BaseURL: "https://twin.test/anthropic", Header: "X-Api-Key"},
	}}
	planProvider = Provider{ID: planID, Label: "Plan"}
)

// Keys long enough for the redactor, each unique to its test.
const (
	envKey   = "env-key-0123456789abcdef"
	savedKey = "saved-key-0123456789abcdef"
	otherKey = "other-key-0123456789abcdef"
)

// savedEntry is savedKey's entry.
const savedEntry = `{"v":1,"key":"` + savedKey + `"}`

// held is a keyring with entry under the twin's account, its calls of kinds
// held until the test ends or calls the release it answers.
func held(t *testing.T, entry string, kinds ...oskeyringtest.Kind) (*oskeyringtest.Keyring, func()) {
	t.Helper()
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), entry))
	release := k.Hold(kinds...)
	t.Cleanup(release)
	return k, release
}

// newService builds a service over cfg's keyring, or a fresh one, closed at
// the test's end, and waits for every provider's restore.
func newService(t *testing.T, cfg Config) *Service {
	t.Helper()
	s := startService(t, cfg)
	testutil.Wait(t, s.Restored(), "the restore")
	return s
}

// startService is newService without the wait for the restore.
func startService(t *testing.T, cfg Config) *Service {
	t.Helper()
	if cfg.Keyring == nil {
		cfg.Keyring = oskeyringtest.New()
	}
	if cfg.Providers == nil {
		cfg.Providers = []Provider{routerProvider, twinProvider}
	}
	s, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(s.Close)
	return s
}

// sealedKeyring fails the test on any call: no provider with no key may reach
// the keyring.
type sealedKeyring struct{ t *testing.T }

func (k sealedKeyring) Get(account string) (string, error) {
	k.t.Errorf("the keyring was read: %s", account)
	return "", oskeyring.ErrNoEntry
}

func (k sealedKeyring) Set(account, _ string) error {
	k.t.Errorf("the keyring was written: %s", account)
	return nil
}

func (k sealedKeyring) Delete(account string) error {
	k.t.Errorf("the keyring was deleted from: %s", account)
	return nil
}

// state is id's state, which the test knows is there.
func state(t *testing.T, s *Service, id string) State {
	t.Helper()
	st, ok := s.State(id)
	require.True(t, ok, id)
	return st
}

// request is a request to url, unsigned.
func request(t *testing.T, url string) *http.Request {
	t.Helper()
	r, err := http.NewRequest(http.MethodPost, url, nil)
	require.NoError(t, err)
	return r
}

// fakeConnector is a provider's connector: it signs with "connector" while it has
// a session.
type fakeConnector struct {
	sessions *watch.Hub[uint64]
	// restored is Restored's channel, closed unless the connector is restoring;
	// restoredSession is the session its restore published.
	restored        chan struct{}
	restoredSession uint64

	mu      sync.Mutex
	session uint64
	refused int
}

func newFakeConnector() *fakeConnector {
	c := newRestoringConnector()
	close(c.restored)
	return c
}

// newRestoringConnector is a connector whose restore has not published.
func newRestoringConnector() *fakeConnector {
	return &fakeConnector{sessions: watch.New[uint64](0), restored: make(chan struct{})}
}

// restore publishes session id, 0 for none, then ends the restore.
func (c *fakeConnector) restore(id uint64) {
	c.publishRestore(id)
	close(c.restored)
}

// publishRestore publishes the restore's session id and leaves the restore
// unended, as a connector does for a moment.
func (c *fakeConnector) publishRestore(id uint64) {
	c.mu.Lock()
	c.restoredSession = id
	c.mu.Unlock()
	c.connect(id)
}

func (c *fakeConnector) Restored() <-chan struct{} { return c.restored }

func (c *fakeConnector) RestoredSession() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.restoredSession
}

// connect starts session id; 0 ends it.
func (c *fakeConnector) connect(id uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = id
	c.sessions.Sender().Send(id) //nolint:errcheck // never closed
}

func (c *fakeConnector) Sign(r *http.Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == 0 {
		return llm.ErrSignedOut
	}
	r.Header.Set("Authorization", "Bearer connector")
	return nil
}

func (c *fakeConnector) Refused(*http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refused++
}

func (c *fakeConnector) Bind() llm.Signer {
	select {
	case <-c.restored:
	default:
		return signer.Pending(c.restored, nil, c.restoredBinding)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == 0 {
		return signer.Ended{}
	}
	return connectorBinding{c: c, session: c.session}
}

// restoredBinding is the binding of the session the restore published.
func (c *fakeConnector) restoredBinding() llm.Signer {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.restoredSession == 0 {
		return signer.Ended{}
	}
	return connectorBinding{c: c, session: c.restoredSession}
}

func (c *fakeConnector) WatchSessionGenerationState() (<-chan uint64, func()) {
	rx := c.sessions.Receiver()
	return rx.Chan(), rx.Close
}

// connectorBinding signs under one session of the connector's.
type connectorBinding struct {
	c       *fakeConnector
	session uint64
}

func (b connectorBinding) Sign(r *http.Request) error {
	b.c.mu.Lock()
	defer b.c.mu.Unlock()
	if b.c.session != b.session {
		return llm.ErrSignedOut
	}
	r.Header.Set("Authorization", "Bearer connector")
	return nil
}

func (b connectorBinding) Refused(*http.Request) {}

// await reads the state watch until id's state meets ok, failing on the timeout.
func await(t *testing.T, s *Service, id, what string, ok func(State) bool) State {
	t.Helper()
	frames, cancel := s.WatchCredentialsState()
	defer cancel()
	for {
		for _, st := range testutil.Recv(t, frames, what) {
			if st.ID == id && ok(st) {
				return st
			}
		}
	}
}

// signedBy is a state whose source is src.
func signedBy(src Source) func(State) bool {
	return func(st State) bool { return st.Source == src }
}
