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
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// An account name is a persisted format: this table changes only with a
// migration.
func TestTheAccountNames(t *testing.T) {
	t.Parallel()
	for id, want := range map[string]string{
		"anthropic":   "anthropic-api-key",
		"openai":      "openai-api-key",
		"gemini":      "gemini-api-key",
		"groq":        "groq-api-key",
		"mistral":     "mistral-api-key",
		"deepseek":    "deepseek-api-key",
		"xai":         "xai-api-key",
		"openrouter":  "openrouter-saved-api-key",
		"together":    "together-api-key",
		"fireworks":   "fireworks-api-key",
		"opencode-go": "opencode-go-api-key",
	} {
		assert.Equal(t, want, account(id), id)
	}
}

func TestNewRestoresWhatTheKeyringHolds(t *testing.T) {
	t.Parallel()
	const key = "restored-key-0123456789abcdef"
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), `{"v":1,"key":"`+key+`"}`))
	s := newService(t, Config{Keyring: k})
	st := state(t, s, twinID)
	assert.Equal(t, SourceSavedKey, st.Source)
	assert.True(t, st.HasSavedKey)
	assert.True(t, safe.HasSecret(key), "a restored key is registered with the redactor")
	assert.Equal(t, SourceNone, state(t, s, routerID).Source)
}

func TestANewerEntryIsLeft(t *testing.T) {
	t.Parallel()
	const entry = `{"v":2,"key":"newer-key-0123456789abcdef"}`
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), entry))
	s := newService(t, Config{Keyring: k})
	assert.False(t, state(t, s, twinID).HasSavedKey)
	assert.Equal(t, entry, k.Entries()[account(twinID)])
}

func TestAnEntryThatDoesNotParseIsDeleted(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{
		`not json`,
		`{"v":0,"key":"k-0123456789abcdef"}`,
		`{"v":1,"key":""}`,
		`{"v":1,"key":"too-short"}`,
		`{"v":1,"key":"k-0123456789\nabcdef"}`,
	} {
		k := oskeyringtest.New()
		require.NoError(t, k.Set(account(twinID), entry))
		s := newService(t, Config{Keyring: k})
		assert.False(t, state(t, s, twinID).HasSavedKey, entry)
		assert.Empty(t, k.Entries(), entry)
	}
}

func TestAnUnreadableKeyringWritesNothing(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), `not json`))
	k.FailGet(account(twinID), errors.New("locked"))
	s := newService(t, Config{Keyring: k})
	assert.False(t, state(t, s, twinID).HasSavedKey)
	assert.Equal(t, map[string]string{account(twinID): `not json`}, k.Entries())
}

// An unparsable entry the keyring will not delete is still no key.
func TestAnUndeletableBadEntryIsNoKey(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), `not json`))
	k.FailDelete = errors.New("locked")
	s := newService(t, Config{Keyring: k})
	assert.False(t, state(t, s, twinID).HasSavedKey)
}

// awaitCall waits for k to receive c.
func awaitCall(t *testing.T, k *oskeyringtest.Keyring, c oskeyringtest.Call) {
	t.Helper()
	require.Eventually(t, func() bool { return slices.Contains(k.Calls(), c) }, testutil.Timeout, time.Millisecond, "%v", c)
}

// assertUnpublished fails if s's restore has published.
func assertUnpublished(t *testing.T, s *Service) {
	t.Helper()
	select {
	case <-s.Restored():
		t.Fatal("the restore published")
	default:
	}
}

// New returns before the keyring answers, every provider Restoring, and Close
// leaves a read that never answers behind.
func TestCloseLeavesAReadThatNeverAnswers(t *testing.T) {
	t.Parallel()
	k, _ := held(t, savedEntry, oskeyringtest.Get)
	s := startService(t, Config{Keyring: k})

	assert.Equal(t, SourceRestoring, state(t, s, twinID).Source)
	testutil.WaitReturn(t, s.Close, "Close")
	assert.Equal(t, SourceRestoring, state(t, s, twinID).Source)
	assertUnpublished(t, s)
}

// Released, the restore publishes what the keyring held, with no relaunch.
func TestTheRestorePublishesWhatItRead(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		entry   string
		failGet bool
		want    Source
	}{
		"a saved key": {entry: savedEntry, want: SourceSavedKey},
		"none":        {want: SourceNone},
		"unreadable":  {entry: savedEntry, failGet: true, want: SourceNone},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			k := oskeyringtest.New()
			if c.entry != "" {
				require.NoError(t, k.Set(account(twinID), c.entry))
			}
			if c.failGet {
				k.FailGet(account(twinID), errors.New("locked"))
			}
			release := k.Hold(oskeyringtest.Get)
			t.Cleanup(release)
			s := startService(t, Config{Keyring: k})
			require.Equal(t, SourceRestoring, state(t, s, twinID).Source)

			release()
			testutil.Wait(t, s.Restored(), "the restore")

			assert.Equal(t, c.want, state(t, s, twinID).Source)
		})
	}
}

// The restore lands only once its delete of an unusable entry has answered;
// until then the provider is Restoring and refuses every change.
func TestTheRestoreWaitsForItsCleanup(t *testing.T) {
	t.Parallel()
	k, release := held(t, `not json`, oskeyringtest.Delete)
	s := startService(t, Config{Keyring: k})
	awaitCall(t, k, oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: account(twinID)})

	assert.Equal(t, SourceRestoring, state(t, s, twinID).Source)
	assert.ErrorIs(t, s.Save(twinID, otherKey), ErrRestoring)

	release()
	testutil.Wait(t, s.Restored(), "the restore")
	assert.Equal(t, SourceNone, state(t, s, twinID).Source)
	assert.Empty(t, k.Entries())
}

// A Close during the restore's delete returns and lands nothing.
func TestCloseLeavesACleanupThatNeverAnswers(t *testing.T) {
	t.Parallel()
	k, _ := held(t, `not json`, oskeyringtest.Delete)
	s := startService(t, Config{Keyring: k})
	awaitCall(t, k, oskeyringtest.Call{Kind: oskeyringtest.Delete, Account: account(twinID)})

	testutil.WaitReturn(t, s.Close, "Close")

	assert.Equal(t, SourceRestoring, state(t, s, twinID).Source)
	assertUnpublished(t, s)
}
