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

// Package oskeyringtest is an oskeyring.Keyring for tests: in memory, or
// wrapping another keyring to record, hold and fail its calls.
package oskeyringtest

import (
	"maps"
	"slices"
	"sync"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring"
)

// Kind is a keyring call's method.
type Kind string

const (
	Get    Kind = "Get"
	Set    Kind = "Set"
	Delete Kind = "Delete"
)

// Call is one call made to the keyring.
type Call struct {
	Kind    Kind
	Account string
}

// Keyring is the OS keyring in memory, the zero value empty, or one Wrap made
// over another keyring. A non-nil FailSet or FailDelete is the error every call
// of its kind answers, changing nothing: set them before sharing it. Gets fail
// through FailGets and FailGet.
type Keyring struct {
	FailSet, FailDelete error

	// inner, when set, answers every call in place of entries. It is called
	// without mu, so a call it holds blocks no other.
	inner oskeyring.Keyring

	mu      sync.Mutex
	entries map[string]string
	calls   []Call
	holds   []hold
	// The next failGets Gets answer failGetsErr; failGet is each account's own.
	failGets    int
	failGetsErr error
	failGet     map[string]error
}

// hold makes calls of its kinds, every kind when it has none, wait for released.
type hold struct {
	kinds    []Kind
	released chan struct{}
}

// New is an empty keyring.
func New() *Keyring { return &Keyring{} }

// Wrap is a keyring that records, holds and fails calls as New's does, and
// passes the rest to inner, which keeps the entries.
func Wrap(inner oskeyring.Keyring) *Keyring { return &Keyring{inner: inner} }

// Hold makes every call of kinds, or every call with no kind, wait from now
// until release. Release before the test ends, so no goroutine outlives it.
func (k *Keyring) Hold(kinds ...Kind) (release func()) {
	released := make(chan struct{})
	k.mu.Lock()
	defer k.mu.Unlock()
	k.holds = append(k.holds, hold{kinds, released})
	return sync.OnceFunc(func() { close(released) })
}

// FailGets makes the next n Gets answer err, whatever the account.
func (k *Keyring) FailGets(n int, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.failGets, k.failGetsErr = n, err
}

// FailGet makes every Get of account answer err.
func (k *Keyring) FailGet(account string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.failGet == nil {
		k.failGet = map[string]error{}
	}
	k.failGet[account] = err
}

// Calls is every call made so far, in order.
func (k *Keyring) Calls() []Call {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.calls)
}

// enter records a call, then waits out the holds on its kind.
func (k *Keyring) enter(kind Kind, account string) {
	k.mu.Lock()
	k.calls = append(k.calls, Call{kind, account})
	holds := slices.Clone(k.holds)
	k.mu.Unlock()
	for _, h := range holds {
		if len(h.kinds) == 0 || slices.Contains(h.kinds, kind) {
			<-h.released
		}
	}
}

func (k *Keyring) Get(account string) (string, error) {
	k.enter(Get, account)
	if err := k.getFailure(account); err != nil {
		return "", err
	}
	if k.inner != nil {
		return k.inner.Get(account)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.entries[account]
	if !ok {
		return "", oskeyring.ErrNoEntry
	}
	return v, nil
}

// getFailure is the error a Get of account answers, nil to answer it.
func (k *Keyring) getFailure(account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.failGets > 0 {
		k.failGets--
		return k.failGetsErr
	}
	return k.failGet[account]
}

func (k *Keyring) Set(account, value string) error {
	k.enter(Set, account)
	if k.FailSet != nil {
		return k.FailSet
	}
	if k.inner != nil {
		return k.inner.Set(account, value)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.entries == nil {
		k.entries = map[string]string{}
	}
	k.entries[account] = value
	return nil
}

func (k *Keyring) Delete(account string) error {
	k.enter(Delete, account)
	if k.FailDelete != nil {
		return k.FailDelete
	}
	if k.inner != nil {
		return k.inner.Delete(account)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.entries, account)
	return nil
}

// Entries is a copy of every entry; none for a keyring Wrap made.
func (k *Keyring) Entries() map[string]string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return maps.Clone(k.entries)
}

var _ oskeyring.Keyring = (*Keyring)(nil)
