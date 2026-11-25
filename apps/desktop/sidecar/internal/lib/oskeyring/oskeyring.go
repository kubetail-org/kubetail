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

// Package oskeyring reads and writes the OS keyring. A read may wait on the
// user, for an unlock prompt, so it has no deadline: Read lets its caller
// abandon one, and a Gate opens the keyring once for every reader.
package oskeyring

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// ErrNoEntry is an account the keyring holds nothing under.
var ErrNoEntry = errors.New("oskeyring: no keyring entry")

// ErrUnavailable is a keyring that is not there to ask yet.
var ErrUnavailable = errors.New("oskeyring: the keyring is not available")

// Keyring is the OS keyring under one service name.
type Keyring interface {
	// Get answers ErrNoEntry for an account that holds nothing.
	Get(account string) (string, error)
	Set(account, value string) error
	// Delete of an account that holds nothing is not an error.
	Delete(account string) error
}

// New is the OS keyring under service, behind a gate.
func New(service string) Keyring { return Gate(osKeyring{service: service}, DefaultPace) }

// osKeyring is the OS keyring through go-keyring.
type osKeyring struct {
	service string
}

func (k osKeyring) Get(account string) (string, error) {
	v, err := keyring.Get(k.service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoEntry
	}
	return v, unavailable(err)
}

func (k osKeyring) Set(account, value string) error {
	return unavailable(keyring.Set(k.service, account, value))
}

func (k osKeyring) Delete(account string) error {
	if err := keyring.Delete(k.service, account); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return unavailable(err)
	}
	return nil
}

// Check writes and deletes a throwaway entry under account, so a caller learns
// the keyring cannot keep a credential before it asks for one.
func Check(k Keyring, account string) error {
	if err := k.Set(account, "ok"); err != nil {
		return err
	}
	return k.Delete(account)
}
