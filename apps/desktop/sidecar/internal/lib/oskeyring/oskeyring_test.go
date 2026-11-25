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

package oskeyring

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

var errBoom = errors.New("boom")

// The OS keyring answers ErrNoEntry for an empty account and deletes one that
// holds nothing without an error. Serial: go-keyring's mock is process state.
func TestTheOSKeyring(t *testing.T) {
	keyring.MockInit()
	k := New("Kubetail-test")

	_, err := k.Get("account")
	assert.ErrorIs(t, err, ErrNoEntry)
	require.NoError(t, k.Set("account", "v"))
	got, err := k.Get("account")
	require.NoError(t, err)
	assert.Equal(t, "v", got)
	require.NoError(t, k.Delete("account"))
	assert.NoError(t, k.Delete("account"))

	keyring.MockInitWithError(errBoom)
	assert.ErrorIs(t, k.Delete("account"), errBoom)
}

// failing is a keyring in memory whose writes and deletes fail on request.
type failing struct {
	entries   map[string]string
	setErr    error
	deleteErr error
}

func (k *failing) Get(account string) (string, error) {
	v, ok := k.entries[account]
	if !ok {
		return "", ErrNoEntry
	}
	return v, nil
}

func (k *failing) Set(account, value string) error {
	if k.setErr != nil {
		return k.setErr
	}
	k.entries[account] = value
	return nil
}

func (k *failing) Delete(account string) error {
	if k.deleteErr != nil {
		return k.deleteErr
	}
	delete(k.entries, account)
	return nil
}

// The check writes and deletes its entry, and fails when either does.
func TestCheck(t *testing.T) {
	k := &failing{entries: map[string]string{}}
	require.NoError(t, Check(k, "check"))
	assert.Empty(t, k.entries)

	k.setErr = errBoom
	assert.ErrorIs(t, Check(k, "check"), errBoom)

	k.setErr, k.deleteErr = nil, errBoom
	assert.ErrorIs(t, Check(k, "check"), errBoom)
}

// New's keyring is gated: a failed opening is kept though the keyring recovers.
// Serial: go-keyring's mock is process state.
func TestNewIsGated(t *testing.T) {
	keyring.MockInitWithError(errBoom)
	t.Cleanup(keyring.MockInit)
	k := New("Kubetail-test")
	_, err := k.Get("account")
	require.ErrorIs(t, err, errBoom)

	keyring.MockInit()
	_, err = k.Get("account")
	assert.ErrorIs(t, err, errBoom)
}
