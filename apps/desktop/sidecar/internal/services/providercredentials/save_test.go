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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
)

func TestSaveRefusesWhatIsNotAKey(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	for _, key := range []string{
		"",
		strings.Repeat("k", safe.MinSecretLen-1),
		strings.Repeat("k", 513),
		"key-0123456789 abcdef",
		"key-0123456789\x01abcdef",
		"key-0123456789éabcdef",
	} {
		assert.ErrorIs(t, s.Save(twinID, key), ErrBadKey, "%q", key)
	}
	assert.Empty(t, k.Entries())
	assert.ErrorIs(t, s.Save("nope", savedKey), ErrUnknownProvider)
}

func TestAProviderWithNoKeyRefusesSaveAndDelete(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{Keyring: sealedKeyring{t}, Providers: []Provider{planProvider}})
	assert.ErrorIs(t, s.Save(planID, savedKey), ErrNoKey)
	assert.ErrorIs(t, s.Delete(planID), ErrNoKey)
}

func TestSaveTrimsAndKeepsTheKey(t *testing.T) {
	t.Parallel()
	const key = "trimmed-key-0123456789abcdef"
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Save(twinID, "  "+key+"\n"))
	assert.Equal(t, map[string]string{account(twinID): `{"v":1,"key":"` + key + `"}`}, k.Entries())
	assert.Equal(t, SourceSavedKey, state(t, s, twinID).Source)
	assert.True(t, safe.HasSecret(key))

	// A key at the longest allowed still saves.
	require.NoError(t, s.Save(twinID, strings.Repeat("k", 512)))
	assert.False(t, safe.HasSecret(key), "a replaced key is forgotten")
}

func TestARefusedSaveChangesNothing(t *testing.T) {
	t.Parallel()
	const held, refused = "held-key-0123456789abcdef", "refused-key-0123456789abcdef"
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Save(twinID, held))
	k.FailSet = errors.New("locked")

	assert.ErrorIs(t, s.Save(twinID, refused), ErrNotSaved)
	assert.Equal(t, encode(held), k.Entries()[account(twinID)])
	assert.True(t, state(t, s, twinID).HasSavedKey)
	assert.False(t, safe.HasSecret(refused), "a refused key is forgotten")
	assert.True(t, safe.HasSecret(held), "the held key is still registered")
}

func TestDeleteFallsBack(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k, EnvKeys: map[string]string{twinID: envKey}})
	require.NoError(t, s.Save(twinID, savedKey))
	require.NoError(t, s.Save(routerID, otherKey))
	credential, cancel := s.WatchCredentialGenerationState(routerID)
	defer cancel()
	assert.Equal(t, uint64(1), testutil.Recv(t, credential, "the credential"))

	require.NoError(t, s.Delete(twinID))
	assert.Equal(t, SourceEnvironmentKey, state(t, s, twinID).Source)
	require.NoError(t, s.Delete(routerID))
	assert.Equal(t, SourceNone, state(t, s, routerID).Source)
	assert.Equal(t, uint64(0), testutil.Recv(t, credential, "no credential"))
	assert.Empty(t, k.Entries())

	require.NoError(t, s.Delete(routerID), "a delete with no key is a no-op")
	assert.ErrorIs(t, s.Delete("nope"), ErrUnknownProvider)
}

// With no key held, Delete leaves the entry: it is a newer Kubetail's.
func TestDeleteLeavesAnEntryItDoesNotHold(t *testing.T) {
	t.Parallel()
	const entry = `{"v":2,"key":"newer"}`
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), entry))
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Delete(twinID))
	assert.Equal(t, entry, k.Entries()[account(twinID)])
}

// A keyring unread at launch may hold a key: Delete still deletes the entry.
func TestDeleteReachesAnEntryUnreadAtLaunch(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(twinID), encode("unread-key-0123456789abcdef")))
	k.FailGet(account(twinID), errors.New("locked"))
	s := newService(t, Config{Keyring: k})
	require.False(t, state(t, s, twinID).HasSavedKey)

	require.NoError(t, s.Delete(twinID))
	assert.Empty(t, k.Entries())
}

// stallingKeyring holds each Delete until release closes.
type stallingKeyring struct {
	*oskeyringtest.Keyring
	entered chan struct{}
	release chan struct{}
}

func (k stallingKeyring) Delete(account string) error {
	k.entered <- struct{}{}
	<-k.release
	return k.Keyring.Delete(account)
}

// Every window learns the key is out of use before the keyring answers, which
// can wait on the user.
func TestDeletePublishesBeforeTheKeyringAnswers(t *testing.T) {
	t.Parallel()
	k := stallingKeyring{Keyring: oskeyringtest.New(), entered: make(chan struct{}), release: make(chan struct{})}
	s := newService(t, Config{Keyring: k, EnvKeys: map[string]string{twinID: envKey}})
	require.NoError(t, s.Save(twinID, savedKey))

	done := make(chan error)
	go func() { done <- s.Delete(twinID) }()
	testutil.Recv(t, k.entered, "the keyring delete")
	await(t, s, twinID, "the environment key signs", signedBy(SourceEnvironmentKey))
	close(k.release)
	require.NoError(t, testutil.Recv(t, done, "the delete"))
}

func TestAFailedDeleteIsNotARemoval(t *testing.T) {
	t.Parallel()
	const key = "undeleted-key-0123456789abcdef"
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Save(twinID, key))
	b := s.Signer(twinID).Bind()
	k.FailDelete = errors.New("locked")

	assert.ErrorIs(t, s.Delete(twinID), ErrNotDeleted)
	st := state(t, s, twinID)
	assert.Equal(t, NotDeleted, st.Failure)
	assert.False(t, st.HasSavedKey, "the key is out of memory")
	assert.False(t, safe.HasSecret(key))
	assert.ErrorIs(t, b.Sign(request(t, "https://twin.test/openai/v1/chat")), llm.ErrCredentialChanged)

	assert.ErrorIs(t, s.Delete(twinID), ErrNotDeleted, "a retry reaches the keyring")
	k.FailDelete = nil
	require.NoError(t, s.Delete(twinID))
	assert.Empty(t, state(t, s, twinID).Failure)
	assert.Empty(t, k.Entries())
}

// A Save while undeleted overwrites the entry and clears the mark.
func TestASaveClearsAFailedDelete(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k})
	require.NoError(t, s.Save(twinID, savedKey))
	k.FailDelete = errors.New("locked")
	require.ErrorIs(t, s.Delete(twinID), ErrNotDeleted)
	require.NoError(t, s.Save(twinID, otherKey))
	assert.Empty(t, state(t, s, twinID).Failure)
}

// While Restoring, Save and Delete are refused, and neither reaches the
// keyring.
func TestAChangeIsRefusedWhileRestoring(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	release := k.Hold(oskeyringtest.Get)
	t.Cleanup(release)
	s := startService(t, Config{Keyring: k})

	assert.ErrorIs(t, s.Save(twinID, savedKey), ErrRestoring)
	assert.ErrorIs(t, s.Delete(twinID), ErrRestoring)

	for _, c := range k.Calls() {
		assert.Equal(t, oskeyringtest.Get, c.Kind, "no write before the read answered")
	}
}
