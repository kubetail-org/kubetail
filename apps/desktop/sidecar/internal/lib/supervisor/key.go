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

// The typed read: a name paired with the value type behind it.
package supervisor

// Key pairs a JOB's registration name with the type it observes, declared once beside the job
// rather than restated at every read. The pairing is checked where the read checks it — when a
// value lands, which in practice is the first test that reads one — and a key declared with the
// same name constant its job registers under cannot get the name half wrong.
//
// A freestanding declaration: registration never hears about it, it works against any Snapshot,
// and a caller that declares none loses nothing.
type Key[T any] struct {
	name string
}

// NewKey pairs name with the value type read through it.
func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }

// Name is the registration name this key addresses.
func (k Key[T]) Name() string { return k.name }

// From is this job's observation out of snap, with the name and type already paired. It panics on
// the same wiring bugs GetJobObservation does.
func (k Key[T]) From(snap Snapshot) JobObservation[T] { return GetJobObservation[T](snap, k.name) }
