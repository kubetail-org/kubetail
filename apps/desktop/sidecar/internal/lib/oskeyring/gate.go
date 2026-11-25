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
	"sync"
	"time"
)

// probeAccount is never written, so the probe's answer is about the keyring
// alone, never about an entry.
const probeAccount = "keyring-probe"

// Pace is how the gate retries a keyring that is not there yet.
type Pace struct {
	// Retries is how many more times a probe that answered ErrUnavailable is made.
	Retries int
	// Backoff is the wait before the first retry; each later one doubles it.
	Backoff time.Duration
}

// DefaultPace is the OS keyring's.
var DefaultPace = Pace{Retries: 4, Backoff: 500 * time.Millisecond}

// Gate is k with its reads behind one opening: the first Get probes the
// keyring, and every Get waits for that probe. A probe that answers opens the
// gate for good; one that fails, a dismissed unlock prompt included, fails
// every Get after it with its error, calling nothing. Writes are not gated.
func Gate(k Keyring, pace Pace) Keyring {
	return &gate{Keyring: k, pace: pace, opened: make(chan struct{})}
}

type gate struct {
	Keyring
	pace Pace
	once sync.Once
	// opened closes when the opening ends; err is then its outcome.
	opened chan struct{}
	err    error
}

func (g *gate) Get(account string) (string, error) {
	if err := g.wait(); err != nil {
		return "", err
	}
	return g.Keyring.Get(account)
}

// Opened waits for k's opening and answers its error. A keyring with no gate
// is open.
func Opened(k Keyring) error {
	if g, ok := k.(*gate); ok {
		return g.wait()
	}
	return nil
}

// wait starts the opening if none has, waits for it to end, and answers its
// error.
func (g *gate) wait() error {
	// The opening runs on a goroutine of its own, so no waiter holds it.
	g.once.Do(func() { go g.open() })
	<-g.opened
	return g.err
}

// open probes the keyring, retrying one not there yet, and keeps the outcome.
func (g *gate) open() {
	defer close(g.opened)
	backoff := g.pace.Backoff
	for retries := g.pace.Retries; ; retries-- {
		_, err := g.Keyring.Get(probeAccount)
		switch {
		case err == nil, errors.Is(err, ErrNoEntry):
			return
		case !errors.Is(err, ErrUnavailable), retries <= 0:
			g.err = err
			return
		}
		time.Sleep(backoff)
		backoff *= 2
	}
}
