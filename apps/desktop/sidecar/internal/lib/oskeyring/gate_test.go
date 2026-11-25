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

package oskeyring_test

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
)

var errBoom = errors.New("boom")

var probe = oskeyringtest.Call{Kind: oskeyringtest.Get, Account: "keyring-probe"}

// read is g's answer for account, on a goroutine, so a test can stagger readers.
func read(g oskeyring.Keyring, account string) <-chan error {
	got := make(chan error, 1)
	go func() {
		_, err := g.Get(account)
		got <- err
	}()
	return got
}

// The opening is one Get of the probe; every reader gets through once it
// answers, those that waited and those that came after.
func TestTheGateOpensOnAnAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := oskeyringtest.New()
		require.NoError(t, k.Set("a", "v"))
		release := k.Hold(oskeyringtest.Get)
		g := oskeyring.Gate(k, oskeyring.DefaultPace)

		first, second := read(g, "a"), read(g, "a")
		synctest.Wait()
		assert.Equal(t, []oskeyringtest.Call{{Kind: oskeyringtest.Set, Account: "a"}, probe}, k.Calls(),
			"the readers wait for the opening")

		release()
		require.NoError(t, <-first)
		require.NoError(t, <-second)
		v, err := g.Get("a")
		require.NoError(t, err)
		assert.Equal(t, "v", v)
		assert.Len(t, k.Calls(), 5, "one probe, then each read")
	})
}

// A probe that fails with an error other than ErrUnavailable is the keyring's
// only Get: every reader after it answers that error.
func TestAFailedOpeningAnswersEveryReader(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	k.FailGets(1, errBoom)
	g := oskeyring.Gate(k, oskeyring.DefaultPace)

	for _, account := range []string{"a", "b", "a"} {
		_, err := g.Get(account)
		assert.ErrorIs(t, err, errBoom)
	}
	assert.Equal(t, []oskeyringtest.Call{probe}, k.Calls())
}

// timed records when each Get reached the keyring.
type timed struct {
	*oskeyringtest.Keyring
	start time.Time
	mu    sync.Mutex
	gets  []time.Duration
}

func (k *timed) Get(account string) (string, error) {
	k.mu.Lock()
	k.gets = append(k.gets, time.Since(k.start))
	k.mu.Unlock()
	return k.Keyring.Get(account)
}

// An unavailable keyring is probed 1+Retries times at the paced backoff,
// whatever the readers; it opens if a retry answers and fails after the last.
func TestTheGateRetriesAKeyringThatIsNotThereYet(t *testing.T) {
	pace := oskeyring.Pace{Retries: 2, Backoff: time.Second}
	probes := []time.Duration{0, time.Second, 3 * time.Second}
	for _, tc := range []struct {
		name     string
		failures int
		want     error
		reads    int
	}{
		{"opens on a retry", 2, oskeyring.ErrNoEntry, 3},
		{"fails after the last", 3, oskeyring.ErrUnavailable, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				k := &timed{Keyring: oskeyringtest.New(), start: time.Now()}
				k.FailGets(tc.failures, oskeyring.ErrUnavailable)
				g := oskeyring.Gate(k, pace)

				early := read(g, "a")
				time.Sleep(2 * time.Second) // the bubble's clock: a reader during the backoff
				late := read(g, "a")
				for _, got := range []<-chan error{early, late, read(g, "a")} {
					assert.ErrorIs(t, <-got, tc.want)
				}

				require.Len(t, k.gets, len(probes)+tc.reads)
				assert.Equal(t, probes, k.gets[:len(probes)])
			})
		})
	}
}

// A negative Retries retries nothing.
func TestNegativeRetriesRetryNothing(t *testing.T) {
	k := oskeyringtest.New()
	k.FailGets(2, oskeyring.ErrUnavailable)
	g := oskeyring.Gate(k, oskeyring.Pace{Retries: -1})

	_, err := g.Get("a")
	require.ErrorIs(t, err, oskeyring.ErrUnavailable)
	assert.Equal(t, []oskeyringtest.Call{probe}, k.Calls())
}

// One entry the keyring cannot hand over fails its own read alone, whichever
// read comes first.
func TestABadEntryFailsItsOwnReadAlone(t *testing.T) {
	t.Parallel()
	for _, order := range [][]string{{"a", "b"}, {"b", "a"}} {
		k := oskeyringtest.New()
		require.NoError(t, k.Set("a", "v"))
		require.NoError(t, k.Set("b", "w"))
		k.FailGet("a", errBoom)
		g := oskeyring.Gate(k, oskeyring.DefaultPace)

		for _, account := range order {
			v, err := g.Get(account)
			if account == "a" {
				assert.ErrorIs(t, err, errBoom)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "w", v)
			}
		}
	}
}

// A Set and a Delete reach the keyring during an opening and after a failed one.
func TestWritesAreNotGated(t *testing.T) {
	writes := func(t *testing.T, g oskeyring.Keyring, k *oskeyringtest.Keyring) {
		t.Helper()
		require.NoError(t, g.Set("a", "v"))
		assert.Equal(t, map[string]string{"a": "v"}, k.Entries())
		require.NoError(t, g.Delete("a"))
		assert.Empty(t, k.Entries())
	}

	t.Run("during an opening", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			k := oskeyringtest.New()
			release := k.Hold(oskeyringtest.Get)
			g := oskeyring.Gate(k, oskeyring.DefaultPace)
			reading := read(g, "a")
			synctest.Wait()

			writes(t, g, k)
			release()
			<-reading
		})
	})

	t.Run("after a failed one", func(t *testing.T) {
		k := oskeyringtest.New()
		k.FailGets(1, errBoom)
		g := oskeyring.Gate(k, oskeyring.DefaultPace)
		_, err := g.Get("a")
		require.ErrorIs(t, err, errBoom)

		writes(t, g, k)
	})
}

// Opened waits for the opening and answers its outcome, reading no entry; a
// keyring with no gate is open.
func TestOpenedIsTheOpeningsOutcome(t *testing.T) {
	k := oskeyringtest.New()
	k.FailGets(1, errBoom)
	require.ErrorIs(t, oskeyring.Opened(oskeyring.Gate(k, oskeyring.DefaultPace)), errBoom)
	assert.Equal(t, []oskeyringtest.Call{probe}, k.Calls())

	k = oskeyringtest.New()
	require.NoError(t, oskeyring.Opened(oskeyring.Gate(k, oskeyring.DefaultPace)))
	assert.Equal(t, []oskeyringtest.Call{probe}, k.Calls())

	k = oskeyringtest.New()
	require.NoError(t, oskeyring.Opened(k))
	assert.Empty(t, k.Calls())
}
