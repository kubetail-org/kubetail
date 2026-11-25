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

package kubeconn

import (
	"context"

	"github.com/amorey/gobus/watch"
)

// StateWatch is a state watch of one claim: the claim's State when it attached, then each
// State published for it. One reader at a time. Close it when done — an abandoned one keeps
// its slot.
//
// Keyed by context, not by the credentials behind it: a receiver is bound to its key for life,
// and credentials move under a context that never does.
//
// Every value is a level, never an edge: the hub keeps only the latest per key, so a reader
// that falls behind skips what came between. The receiver registers before the first State is
// read, so a pass landing between the two is never missed; it can arrive once more after it.
type StateWatch struct {
	attached *State // the first value, until it is read
	rx       *watch.Receiver[string, State]
}

// NewStateWatch is a watch that delivers attached first, then what rx receives. rx must have
// been registered before attached was read.
func NewStateWatch(attached State, rx *watch.Receiver[string, State]) *StateWatch {
	return &StateWatch{attached: &attached, rx: rx}
}

// RecvContext returns the next State, or an error once ctx ends or the watch is closed.
func (w *StateWatch) RecvContext(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	if st := w.attached; st != nil {
		w.attached = nil
		return *st, nil
	}
	ev, err := w.rx.RecvContext(ctx)
	return ev.Value, err
}

// Close releases the watch's slot.
func (w *StateWatch) Close() { w.rx.Close() }
