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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/logging"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/poke"
)

func TestNewRefusesAProviderItCannotUse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		providers []Provider
	}{
		{"an empty id", []Provider{{Routes: routerProvider.Routes}}},
		{"a duplicate id", []Provider{routerProvider, routerProvider}},
		{"a base with no host", []Provider{{ID: "x", Routes: []Route{{BaseURL: "localhost:8080"}}}}},
		{"a base that does not parse", []Provider{{ID: "x", Routes: []Route{{BaseURL: "http://[::1"}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{Providers: tc.providers})
			assert.Error(t, err)
		})
	}
}

func TestStatesFollowTheRows(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{EnvKeys: map[string]string{twinID: envKey}})
	assert.Equal(t, []State{
		{ID: routerID, Label: "Router", KeyVar: "ROUTER_API_KEY", Source: SourceNone},
		{ID: twinID, Label: "Twin", KeyVar: "TWIN_API_KEY", Source: SourceEnvironmentKey, HasEnvironmentKey: true},
	}, testutil.Current(t, s.WatchCredentialsState, "the credentials"))
	_, ok := s.State("nope")
	assert.False(t, ok)
	assert.Nil(t, s.Signer("nope"))
}

// A provider with no key has no keyring entry to read, and takes no key from
// the environment.
func TestAProviderWithNoKeyHoldsNone(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{
		Keyring:   sealedKeyring{t},
		Providers: []Provider{planProvider},
		EnvKeys:   map[string]string{planID: envKey},
	})
	assert.Equal(t, State{ID: planID, Label: "Plan", Source: SourceNone}, state(t, s, planID))
	assert.Equal(t, signer.Ended{}, s.Signer(planID).Bind())
	_, err := bearer(t, s.Signer(planID), routerChat)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

func TestTheSourceTable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		connector  bool
		session    uint64
		saved, env bool
		want       Source
	}{
		{"connected over both keys", true, 7, true, true, SourceConnector},
		{"connected with no key", true, 7, false, false, SourceConnector},
		{"a saved key over the environment's", true, 0, true, true, SourceSavedKey},
		{"a saved key with no connector", false, 0, true, false, SourceSavedKey},
		{"the environment's key", true, 0, false, true, SourceEnvironmentKey},
		{"the environment's key with no connector", false, 0, false, true, SourceEnvironmentKey},
		{"nothing", true, 0, false, false, SourceNone},
		{"nothing and no connector", false, 0, false, false, SourceNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := oskeyringtest.New()
			cfg := Config{Keyring: k, EnvKeys: map[string]string{}}
			if tc.saved {
				require.NoError(t, k.Set(account(routerID), encode(savedKey)))
			}
			if tc.env {
				cfg.EnvKeys[routerID] = envKey
			}
			var c *fakeConnector
			if tc.connector {
				c = newFakeConnector()
				cfg.Connectors = map[string]Connector{routerID: c}
			}
			s := newService(t, cfg)
			if tc.session != 0 {
				c.connect(tc.session)
				await(t, s, routerID, "the connector signs", signedBy(SourceConnector))
			}
			assert.Equal(t, tc.want, state(t, s, routerID).Source)
		})
	}
}

// The restore lands on the connector's session.
func TestTheLaunchStateHasTheConnectorsSession(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	c.connect(3)
	s := newService(t, Config{Connectors: map[string]Connector{routerID: c}})
	assert.Equal(t, SourceConnector, state(t, s, routerID).Source)
	c.connect(0)
	await(t, s, routerID, "the session ends", signedBy(SourceNone))
}

func TestWatchCredentialsStateSendsTheWholeValue(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{})
	frames, cancel := s.WatchCredentialsState()
	defer cancel()
	assert.Equal(t, []State{state(t, s, routerID), state(t, s, twinID)}, testutil.Recv(t, frames, "the current list"))
	require.NoError(t, s.Save(twinID, savedKey))
	frame := testutil.Recv(t, frames, "the change")
	assert.Equal(t, SourceSavedKey, frame[1].Source)
}

// Every change is built and sent under one lock, so the state watch never ends on a
// list older than the providers.
func TestConcurrentChangesPublishTheLatestList(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{})
	for range 50 {
		var wg sync.WaitGroup
		for _, id := range []string{routerID, twinID} {
			wg.Go(func() { assert.NoError(t, s.Save(id, savedKey)) })
			wg.Go(func() { assert.NoError(t, s.Delete(id)) })
		}
		wg.Wait()
		frames, cancel := s.WatchCredentialsState()
		assert.Equal(t, []State{state(t, s, routerID), state(t, s, twinID)}, testutil.Recv(t, frames, "the latest list"))
		cancel()
	}
}

// awaitValue reads values until one is want, failing on the timeout. Each
// value before it must be one of passing.
func awaitValue(t *testing.T, values <-chan uint64, want uint64, passing ...uint64) {
	t.Helper()
	for {
		got := testutil.Recv(t, values, fmt.Sprint("the value ", want))
		if got == want {
			return
		}
		require.Contains(t, passing, got)
	}
}

// A keyed provider's catalog is the same under every credential: its value
// stays 1 while its connector connects and disconnects over a key.
func TestWatchCredentialGenerationStateIsOneForAKeyedProvider(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	s := newService(t, Config{Connectors: map[string]Connector{routerID: c}})
	values, cancel := s.WatchCredentialGenerationState(routerID)
	defer cancel()

	awaitValue(t, values, 0)
	require.NoError(t, s.Save(routerID, savedKey))
	awaitValue(t, values, 1)
	c.connect(5)
	await(t, s, routerID, "the connector signs", signedBy(SourceConnector))
	c.connect(0)
	await(t, s, routerID, "the saved key signs", signedBy(SourceSavedKey))
	require.NoError(t, s.Delete(routerID))
	awaitValue(t, values, 0, 1)

	c.connect(5)
	awaitValue(t, values, 1, 0)
}

// A provider with no key carries its connector's session, so a second session
// is a new value with no 0 between.
func TestWatchCredentialGenerationStateIsTheSessionForAProviderWithNoKey(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	s := newService(t, Config{Providers: []Provider{planProvider}, Connectors: map[string]Connector{planID: c}})
	values, cancel := s.WatchCredentialGenerationState(planID)
	defer cancel()

	awaitValue(t, values, 0)
	c.connect(1)
	awaitValue(t, values, 1, 0)
	c.connect(5)
	awaitValue(t, values, 5, 1)
	c.connect(0)
	awaitValue(t, values, 0, 5)
}

// A second session reads the catalog again, though the watch never passed 0.
func TestSyncCatalogsReadsEachSession(t *testing.T) {
	t.Parallel()
	models := llm.New(llm.Provider{ID: planID, Dialect: llm.DialectChatCompletions})
	c := newFakeConnector()
	s := newService(t, Config{Providers: []Provider{planProvider}, Connectors: map[string]Connector{planID: c}})
	reads := make(chan struct{}, 8)
	stop := s.SyncCatalogs(poke.New(), models, func(context.Context, llm.Provider) ([]llm.Model, error) {
		reads <- struct{}{}
		return []llm.Model{{ID: "m"}}, nil
	})
	defer stop()

	c.connect(1)
	testutil.Recv(t, reads, "the first session's read")
	c.connect(2)
	testutil.Recv(t, reads, "the second session's read")
}

func TestSyncCatalogsFollowsTheCredential(t *testing.T) {
	t.Parallel()
	written := []llm.Model{{ID: "m"}}
	models := llm.New(
		llm.Provider{ID: routerID, Dialect: llm.DialectChatCompletions},
		llm.Provider{ID: twinID, Dialect: llm.DialectChatCompletions},
	)
	frames, cancel := models.WatchProvidersState()
	defer cancel()
	testutil.Recv(t, frames, "the first frame")

	s := newService(t, Config{EnvKeys: map[string]string{routerID: envKey}})
	stop := s.SyncCatalogs(poke.New(), models, func(context.Context, llm.Provider) ([]llm.Model, error) { return written, nil })
	defer stop()
	listed := func(id string) bool {
		p, _ := models.Provider(id)
		return len(p.Catalog) > 0
	}
	awaitModels := func(what string, ok func() bool) {
		t.Helper()
		for !ok() {
			testutil.Recv(t, frames, what)
		}
	}
	awaitModels("the keyed provider's models", func() bool { return listed(routerID) })
	assert.False(t, listed(twinID))

	require.NoError(t, s.Save(twinID, savedKey))
	awaitModels("the saved provider's models", func() bool { return listed(twinID) })
	require.NoError(t, s.Delete(twinID))
	awaitModels("the removed provider's models leave", func() bool { return !listed(twinID) })
}

// No saved key reaches a log line, and each is registered with the redactor
// only while held. Serial: it swaps the default logger.
func TestNoSavedKeyLeavesTheKeyring(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const first, second = "first-key-0123456789abcdef", "second-key-0123456789abcdef"
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Save(routerID, first))
	assert.True(t, safe.HasSecret(first))
	b := s.Signer(routerID).Bind()
	refusedWith(t, b)
	require.NoError(t, s.Save(routerID, second))
	assert.False(t, safe.HasSecret(first), "a replaced key is forgotten")
	k.FailDelete = errors.New("locked")
	require.ErrorIs(t, s.Delete(routerID), ErrNotDeleted)
	k.FailDelete = nil
	require.NoError(t, s.Delete(routerID))
	k.FailSet = errors.New("locked")
	require.ErrorIs(t, s.Save(routerID, first), ErrNotSaved)

	for _, key := range []string{first, second} {
		assert.NotContains(t, logs.String(), key)
		assert.False(t, safe.HasSecret(key), "%s is forgotten once removed", key)
	}
}

// A refusal is logged while its key is still registered, so an error that
// quotes the key is redacted. Serial: it swaps the default logger.
func TestARefusedSaveLogsNoKey(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(logging.Init(&logs, slog.LevelDebug))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const key = "quoted-key-0123456789abcdef"
	k := oskeyringtest.New()
	k.FailSet = errors.New("keyring refused " + encode(key))
	s := newService(t, Config{Keyring: k})
	require.ErrorIs(t, s.Save(routerID, key), ErrNotSaved)

	assert.Contains(t, logs.String(), "could not save the key")
	assert.NotContains(t, logs.String(), key)
}

// A provider stays Restoring while its connector restores, and Close returns.
// Released, it lands on the connector's restored session, never on the 0
// published while Restoring, and the landing moves no key generation.
func TestAFollowerLandsOnTheRestoredSession(t *testing.T) {
	t.Parallel()
	t.Run("held", func(t *testing.T) {
		t.Parallel()
		c := newRestoringConnector()
		s := startService(t, Config{Connectors: map[string]Connector{routerID: c}})
		testutil.Wait(t, s.byID[twinID].restored, "the twin's restore")

		assert.Equal(t, SourceRestoring, state(t, s, routerID).Source)
		testutil.WaitReturn(t, s.Close, "Close")
		assert.Equal(t, SourceRestoring, state(t, s, routerID).Source)
	})
	t.Run("released connected", func(t *testing.T) {
		t.Parallel()
		c := newRestoringConnector()
		s := startService(t, Config{Connectors: map[string]Connector{routerID: c}})

		c.restore(3)
		testutil.Wait(t, s.Restored(), "the restore")

		assert.Equal(t, SourceConnector, state(t, s, routerID).Source)
		s.mu.Lock()
		defer s.mu.Unlock()
		assert.Zero(t, s.byID[routerID].keyGen)
	})
}

// HasCredential is known while Restoring for an environment key alone: a
// launch with one lists its models at once, and one with a saved key alone
// lists them when the read answers.
func TestHasCredentialWhileRestoring(t *testing.T) {
	t.Parallel()
	k, release := held(t, savedEntry, oskeyringtest.Get)
	s := startService(t, Config{Keyring: k, EnvKeys: map[string]string{routerID: envKey}})
	router, cancelRouter := s.WatchCredentialGenerationState(routerID)
	defer cancelRouter()
	twin, cancelTwin := s.WatchCredentialGenerationState(twinID)
	defer cancelTwin()

	assert.True(t, state(t, s, routerID).HasCredential())
	assert.Equal(t, uint64(1), testutil.Recv(t, router, "the router's credential"))
	assert.False(t, state(t, s, twinID).HasCredential())
	assert.Zero(t, testutil.Recv(t, twin, "the twin's credential"))

	release()
	assert.Equal(t, uint64(1), testutil.Recv(t, twin, "the twin's saved key"))
}

// Nothing is reported failed while Restoring.
func TestFailureIsNothingWhileRestoring(t *testing.T) {
	t.Parallel()
	assert.Empty(t, failure(true, true, SourceRestoring))
}

// A service with no provider has nothing to restore.
func TestNoProviderIsRestoredAtOnce(t *testing.T) {
	t.Parallel()
	s := startService(t, Config{Providers: []Provider{}})

	testutil.Wait(t, s.Restored(), "the restore")
}

// A provider that takes no key reads no entry: it is Restoring until its
// connector has restored, and no longer.
func TestAProviderWithNoKeyRestoresWithItsConnector(t *testing.T) {
	t.Parallel()
	c := newRestoringConnector()
	s := startService(t, Config{Keyring: sealedKeyring{t}, Providers: []Provider{planProvider}, Connectors: map[string]Connector{planID: c}})

	assert.Equal(t, SourceRestoring, state(t, s, planID).Source)
	c.restore(1)
	testutil.Wait(t, s.Restored(), "the restore")
	assert.Equal(t, SourceConnector, state(t, s, planID).Source)
}
