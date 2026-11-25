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
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/internal/signer"
)

const routerChat = routerAPI + "/chat/completions"

// bearer is the Authorization header sign leaves on a request to url.
func bearer(t *testing.T, sg llm.Signer, url string) (string, error) {
	t.Helper()
	r := request(t, url)
	err := sg.Sign(r)
	return r.Header.Get("Authorization"), err
}

func TestTheHighestCredentialSigns(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	s := newService(t, Config{EnvKeys: map[string]string{routerID: envKey}, Connectors: map[string]Connector{routerID: c}})
	sg := s.Signer(routerID)

	got, err := bearer(t, sg.Bind(), routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+envKey, got)

	require.NoError(t, s.Save(routerID, savedKey))
	got, err = bearer(t, sg.Bind(), routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+savedKey, got)

	c.connect(5)
	got, err = bearer(t, sg.Bind(), routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer connector", got)
	got, err = bearer(t, sg, routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer connector", got, "the unbound signer follows the same order")

	c.connect(0)
	got, err = bearer(t, sg, routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+savedKey, got, "the unbound signer falls back to the key")
}

func TestAProviderWithNoCredentialBindsEnded(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{})
	assert.Equal(t, signer.Ended{}, s.Signer(twinID).Bind())
	_, err := bearer(t, s.Signer(twinID), "https://twin.test/openai/v1/chat")
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

func TestAProviderWithNoKeyIsItsConnectors(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	s := newService(t, Config{Keyring: sealedKeyring{t}, Providers: []Provider{planProvider}, Connectors: map[string]Connector{planID: c}})
	sg := s.Signer(planID)

	c.connect(4)
	await(t, s, planID, "the connector signs", signedBy(SourceConnector))
	got, err := bearer(t, sg.Bind(), routerChat)
	require.NoError(t, err)
	assert.Equal(t, "Bearer connector", got)

	c.connect(0)
	await(t, s, planID, "the session ends", signedBy(SourceNone))
	assert.Equal(t, signer.Ended{}, sg.Bind())
	_, err = bearer(t, sg, routerChat)
	assert.ErrorIs(t, err, llm.ErrSignedOut)
}

func TestTheKeyGoesNowhereElse(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{EnvKeys: map[string]string{twinID: envKey}})
	b := s.Signer(twinID).Bind()

	r := request(t, "https://twin.test/openai/v1/chat/completions")
	require.NoError(t, b.Sign(r))
	assert.Equal(t, "Bearer "+envKey, r.Header.Get("Authorization"))
	assert.Empty(t, r.Header.Get("X-Api-Key"))

	r = request(t, "https://twin.test/anthropic/v1/messages")
	require.NoError(t, b.Sign(r))
	assert.Equal(t, envKey, r.Header.Get("X-Api-Key"))
	assert.Empty(t, r.Header.Get("Authorization"))

	for _, url := range []string{
		"https://twin.test/other",
		"https://twin.test/openai/v1x",
		"http://twin.test/openai/v1/chat",
		"https://evil.test/openai/v1/chat",
	} {
		r := request(t, url)
		assert.ErrorIs(t, b.Sign(r), errOffBase, url)
		assert.Empty(t, r.Header, url)
	}
}

// One stub under both bases takes both headers: a debug build's.
func TestABaseUnderTwoRoutesTakesBothHeaders(t *testing.T) {
	t.Parallel()
	stub := Provider{ID: "stub", KeyVar: "STUB_API_KEY", Routes: []Route{
		{BaseURL: "http://127.0.0.1:9", Header: "Authorization", Prefix: "Bearer "},
		{BaseURL: "http://127.0.0.1:9", Header: "X-Api-Key"},
	}}
	s := newService(t, Config{Providers: []Provider{stub}, EnvKeys: map[string]string{"stub": envKey}})
	r := request(t, "http://127.0.0.1:9/v1/messages")
	require.NoError(t, s.Signer("stub").Bind().Sign(r))
	assert.Equal(t, "Bearer "+envKey, r.Header.Get("Authorization"))
	assert.Equal(t, envKey, r.Header.Get("X-Api-Key"))
}

func TestABindingEndsWithItsKey(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	c := newFakeConnector()
	s := newService(t, Config{Keyring: k, EnvKeys: map[string]string{routerID: envKey}, Connectors: map[string]Connector{routerID: c}})
	sg := s.Signer(routerID)

	underEnv := sg.Bind()
	k.FailSet = errors.New("locked")
	require.ErrorIs(t, s.Save(routerID, savedKey), ErrNotSaved)
	_, err := bearer(t, underEnv, routerChat)
	require.NoError(t, err, "a refused Save leaves the binding")
	require.NoError(t, s.Delete(routerID))
	_, err = bearer(t, underEnv, routerChat)
	require.NoError(t, err, "a Delete with no key held leaves the binding")

	k.FailSet = nil
	require.NoError(t, s.Save(routerID, savedKey))
	_, err = bearer(t, underEnv, routerChat)
	assert.ErrorIs(t, err, llm.ErrCredentialChanged, "a saved key ends the binding")

	underSaved := sg.Bind()
	k.FailDelete = errors.New("locked")
	require.ErrorIs(t, s.Delete(routerID), ErrNotDeleted)
	_, err = bearer(t, underSaved, routerChat)
	assert.ErrorIs(t, err, llm.ErrCredentialChanged, "a Delete that drops the key ends its binding")

	underEnv = sg.Bind()
	c.connect(9)
	await(t, s, routerID, "the connector's session", signedBy(SourceConnector))
	_, err = bearer(t, underEnv, routerChat)
	assert.ErrorIs(t, err, llm.ErrCredentialChanged, "the connector's connect ends a key's binding")

	underConnector := sg.Bind()
	require.ErrorIs(t, s.Delete(routerID), ErrNotDeleted)
	got, err := bearer(t, underConnector, routerChat)
	require.NoError(t, err, "the connector's binding is the connector's")
	assert.Equal(t, "Bearer connector", got)
}

// refusedWith tells sg of a 401 to a request it signed.
func refusedWith(t *testing.T, sg llm.Signer) {
	t.Helper()
	r := request(t, routerChat)
	require.NoError(t, sg.Sign(r))
	sg.Refused(r)
}

func TestARejectedKeyIsKept(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{})
	require.NoError(t, s.Save(routerID, savedKey))
	old := s.Signer(routerID).Bind()
	r := request(t, routerChat)
	require.NoError(t, old.Sign(r))

	require.NoError(t, s.Save(routerID, otherKey))
	old.Refused(r)
	assert.Empty(t, state(t, s, routerID).Failure, "a 401 to a replaced key marks nothing")

	b := s.Signer(routerID).Bind()
	refusedWith(t, b)
	assert.Equal(t, KeyRejected, state(t, s, routerID).Failure)
	got, err := bearer(t, b, routerChat)
	require.NoError(t, err, "a rejected key still signs")
	assert.Equal(t, "Bearer "+otherKey, got)

	require.NoError(t, s.Save(routerID, savedKey))
	assert.Empty(t, state(t, s, routerID).Failure, "a new key clears the mark")
}

func TestTheConnectorHearsItsOwn401(t *testing.T) {
	t.Parallel()
	c := newFakeConnector()
	c.connect(1)
	s := newService(t, Config{EnvKeys: map[string]string{routerID: envKey}, Connectors: map[string]Connector{routerID: c}})
	refusedWith(t, s.Signer(routerID))
	c.mu.Lock()
	assert.Equal(t, 1, c.refused)
	c.mu.Unlock()
	assert.Empty(t, state(t, s, routerID).Failure, "the connector's 401 is not the key's")
}

func TestARejectionDoesNotHideAFailedDelete(t *testing.T) {
	t.Parallel()
	k := oskeyringtest.New()
	s := newService(t, Config{Keyring: k, EnvKeys: map[string]string{routerID: envKey}})
	require.NoError(t, s.Save(routerID, savedKey))
	k.FailDelete = errors.New("locked")
	require.ErrorIs(t, s.Delete(routerID), ErrNotDeleted)

	refusedWith(t, s.Signer(routerID).Bind())
	assert.Equal(t, NotDeleted, state(t, s, routerID).Failure)

	k.FailDelete = nil
	require.NoError(t, s.Delete(routerID))
	assert.Equal(t, KeyRejected, state(t, s, routerID).Failure, "the environment key's mark shows once the delete lands")
}

// A 401 to a request off the provider's bases marks nothing.
func TestARejectionOffTheBasesMarksNothing(t *testing.T) {
	t.Parallel()
	s := newService(t, Config{EnvKeys: map[string]string{routerID: envKey}})
	r := request(t, "https://evil.test/v1")
	r.Header.Set("Authorization", "Bearer "+envKey)
	s.Signer(routerID).Bind().Refused(r)
	assert.Empty(t, state(t, s, routerID).Failure)
}

// restoring is a service over a saved router key and c, whose reads are held
// until the release it answers.
func restoring(t *testing.T, c *fakeConnector) (*Service, func()) {
	t.Helper()
	k := oskeyringtest.New()
	require.NoError(t, k.Set(account(routerID), savedEntry))
	release := k.Hold(oskeyringtest.Get)
	t.Cleanup(release)
	return startService(t, Config{Keyring: k, Connectors: map[string]Connector{routerID: c}}), release
}

// signature is what a Sign left on a request, and its error.
type signature struct {
	header string
	err    error
}

// signLater signs a request to the router with sg on a goroutine.
func signLater(t *testing.T, sg llm.Signer) <-chan signature {
	t.Helper()
	got := make(chan signature, 1)
	go func() {
		h, err := bearer(t, sg, routerChat)
		got <- signature{h, err}
	}()
	return got
}

// Nothing signs until both the saved key and the connector's session are
// known: a request made while Restoring carries the connector's key when it
// restores connected, and the saved key when it restores signed out. A
// binding that resolved to the connector stays the connector's, and fails
// after a Disconnect, never signing with the saved key.
func TestNothingSignsBeforeBothAreKnown(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		session uint64
		want    string
	}{
		"connected":  {session: 3, want: "Bearer connector"},
		"signed out": {session: 0, want: "Bearer " + savedKey},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			conn := newRestoringConnector()
			s, release := restoring(t, conn)
			bound := s.Signer(routerID).Bind()
			unbound := signLater(t, s.Signer(routerID))
			viaBinding := signLater(t, bound)

			release()
			conn.restore(c.session)

			for _, got := range []<-chan signature{unbound, viaBinding} {
				sig := testutil.Recv(t, got, "the signature")
				assert.NoError(t, sig.err)
				assert.Equal(t, c.want, sig.header)
			}
		})
	}
	t.Run("connected after the landing", func(t *testing.T) {
		t.Parallel()
		conn := newRestoringConnector()
		s, release := restoring(t, conn)
		conn.restore(0)
		require.Eventually(t, func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.byID[routerID].landed
		}, testutil.Timeout, time.Millisecond, "the landing")
		conn.connect(3)
		viaBinding := signLater(t, s.Signer(routerID).Bind())

		release()

		sig := testutil.Recv(t, viaBinding, "the signature")
		assert.NoError(t, sig.err)
		assert.Equal(t, "Bearer connector", sig.header)
	})
	t.Run("disconnected before the follower", func(t *testing.T) {
		t.Parallel()
		conn := newRestoringConnector()
		s, release := restoring(t, conn)
		bound := s.Signer(routerID).Bind()
		conn.publishRestore(3)
		conn.connect(0)
		close(conn.restored)
		release()
		testutil.Wait(t, s.Restored(), "the restore")

		_, err := bearer(t, bound, routerChat)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
	})
	t.Run("disconnected after the landing", func(t *testing.T) {
		t.Parallel()
		conn := newRestoringConnector()
		s, release := restoring(t, conn)
		bound := s.Signer(routerID).Bind()
		release()
		conn.restore(3)
		testutil.Wait(t, s.Restored(), "the restore")

		conn.connect(0)
		await(t, s, routerID, "the session ends", signedBy(SourceSavedKey))

		_, err := bearer(t, bound, routerChat)
		assert.ErrorIs(t, err, llm.ErrSignedOut)
	})
}

// A wait for the restore ends with the request's context, and signed out when
// the service closes with the read still held, bound or not.
func TestAWaitEndsWithClose(t *testing.T) {
	t.Parallel()
	s, _ := restoring(t, newFakeConnector())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, s.Signer(routerID).Sign(request(t, routerChat).WithContext(ctx)), context.Canceled)

	bound := s.Signer(routerID).Bind()
	unbound := signLater(t, s.Signer(routerID))
	viaBinding := signLater(t, bound)

	s.Close()

	for _, got := range []<-chan signature{unbound, viaBinding} {
		assert.ErrorIs(t, testutil.Recv(t, got, "the wait").err, llm.ErrSignedOut)
	}
}
