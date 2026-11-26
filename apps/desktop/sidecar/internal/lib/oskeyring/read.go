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
	"context"
	"log/slog"
	"time"
)

// SlowAfter is how long a read may take before it is logged.
const SlowAfter = 30 * time.Second

// Read is account's value. It has no deadline, and logs once past slowAfter.
// When ctx ends first it answers ctx's error and leaves the keyring's own call
// to finish alone.
func Read(ctx context.Context, k Keyring, account string, slowAfter time.Duration) (string, error) {
	slow := time.AfterFunc(slowAfter, func() {
		slog.Warn("keyring: still waiting for the keyring", "account", account)
	})
	defer slow.Stop()

	type answer struct {
		value string
		err   error
	}
	// Buffered, so an abandoned call's answer is dropped.
	got := make(chan answer, 1)
	go func() {
		v, err := k.Get(account)
		got <- answer{v, err}
	}()
	select {
	case a := <-got:
		return a.value, a.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Delete is k.Delete(account), left to finish alone when ctx ends first.
func Delete(ctx context.Context, k Keyring, account string) error {
	got := make(chan error, 1)
	go func() { got <- k.Delete(account) }()
	select {
	case err := <-got:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
