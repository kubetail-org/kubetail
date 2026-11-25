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

package oskeyringtest

import (
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
)

func TestTheFakeKeyring(t *testing.T) {
	k := New()
	_, err := k.Get("a")
	require.ErrorIs(t, err, oskeyring.ErrNoEntry)

	require.NoError(t, k.Set("a", "v"))
	v, err := k.Get("a")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
	assert.Equal(t, map[string]string{"a": "v"}, k.Entries())

	require.NoError(t, k.Delete("a"))
	require.NoError(t, k.Delete("a"), "a delete of nothing is not an error")
	assert.Empty(t, k.Entries())
}

// A keyring built as a literal, to set a Fail field, holds entries too.
func TestTheZeroKeyringIsEmpty(t *testing.T) {
	k := &Keyring{FailDelete: errors.New("boom")}
	assert.Empty(t, k.Entries())
	require.NoError(t, k.Set("a", "v"))
	assert.Equal(t, map[string]string{"a": "v"}, k.Entries())
}

func TestTheFakeKeyringFailsOnRequest(t *testing.T) {
	boom := errors.New("boom")
	k := New()
	require.NoError(t, k.Set("a", "v"))
	k.FailSet, k.FailDelete = boom, boom

	assert.ErrorIs(t, k.Set("a", "w"), boom)
	assert.ErrorIs(t, k.Delete("a"), boom)
	assert.Equal(t, map[string]string{"a": "v"}, k.Entries(), "a failed call changes nothing")
}

// Calls lists every call's kind and account, in order, failed ones included.
func TestCallsListsEveryCall(t *testing.T) {
	k := New()
	k.FailDelete = errors.New("boom")
	_, _ = k.Get("a")
	_ = k.Set("b", "v")
	_ = k.Delete("c")
	assert.Equal(t, []Call{{Get, "a"}, {Set, "b"}, {Delete, "c"}}, k.Calls())
}

// FailGets fails the next n Gets, whatever the account, and then no more.
func TestFailGetsFailsTheNextGets(t *testing.T) {
	boom := errors.New("boom")
	k := New()
	require.NoError(t, k.Set("a", "v"))
	k.FailGets(2, boom)

	_, err := k.Get("a")
	assert.ErrorIs(t, err, boom)
	_, err = k.Get("b")
	assert.ErrorIs(t, err, boom)
	v, err := k.Get("a")
	require.NoError(t, err)
	assert.Equal(t, "v", v)
}

// FailGet fails every Get of one account and no other's.
func TestFailGetFailsOneAccount(t *testing.T) {
	boom := errors.New("boom")
	k := New()
	require.NoError(t, k.Set("a", "v"))
	require.NoError(t, k.Set("b", "w"))
	k.FailGet("a", boom)

	for range 2 {
		_, err := k.Get("a")
		assert.ErrorIs(t, err, boom)
	}
	v, err := k.Get("b")
	require.NoError(t, err)
	assert.Equal(t, "w", v)
}

// Hold holds the calls of its kinds until release, and no others.
func TestHoldHoldsItsKindsUntilRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := New()
		release := k.Hold(Get)
		got := make(chan error, 1)
		go func() { _, err := k.Get("a"); got <- err }()

		require.NoError(t, k.Set("a", "v"), "a Set is not held")
		synctest.Wait()
		select {
		case <-got:
			t.Fatal("the Get answered while held")
		default:
		}

		release()
		release()
		assert.NoError(t, <-got)
		_, err := k.Get("a")
		assert.NoError(t, err, "a call made after release is not held")
	})
}

// Hold with no kind holds every call.
func TestHoldWithNoKindHoldsEveryCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		k := New()
		release := k.Hold()
		done := make(chan struct{}, 3)
		go func() { _, _ = k.Get("a"); done <- struct{}{} }()
		go func() { _ = k.Set("a", "v"); done <- struct{}{} }()
		go func() { _ = k.Delete("a"); done <- struct{}{} }()
		synctest.Wait()
		assert.Empty(t, done)
		assert.Len(t, k.Calls(), 3, "a held call is listed when made")

		release()
		for range 3 {
			<-done
		}
	})
}

// A wrapped keyring is recorded and held as the fake is, and answered by the
// keyring it wraps.
func TestWrapRecordsAndHoldsThenDelegates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := New()
		require.NoError(t, inner.Set("a", "v"))
		k := Wrap(inner)
		release := k.Hold(Get)
		got := make(chan string, 1)
		go func() { v, _ := k.Get("a"); got <- v }()
		synctest.Wait()
		assert.Empty(t, got, "the Get is held")

		release()
		assert.Equal(t, "v", <-got)
		require.NoError(t, k.Set("b", "w"))
		require.NoError(t, k.Delete("a"))
		assert.Equal(t, map[string]string{"b": "w"}, inner.Entries())
		assert.Equal(t, []Call{{Get, "a"}, {Set, "b"}, {Delete, "a"}}, k.Calls())
	})
}

// A call the wrapped keyring holds blocks no other call, nor Calls.
func TestAHeldInnerCallBlocksNoOther(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := New()
		release := inner.Hold(Get)
		k := Wrap(inner)
		go func() { _, _ = k.Get("a") }()
		synctest.Wait()

		require.NoError(t, k.Set("b", "w"))
		assert.Equal(t, []Call{{Get, "a"}, {Set, "b"}}, k.Calls())
		release()
	})
}
