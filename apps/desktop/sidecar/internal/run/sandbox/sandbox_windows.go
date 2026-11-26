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

package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// errNone is why a command cannot start sandboxed on Windows.
var errNone = errors.New("no sandbox on Windows")

// Probe answers no sandbox: native Windows has none, and WSL2 runs the Linux
// build's.
func Probe(context.Context) (*Sandbox, State, error) {
	return nil, State{Reason: "no sandbox on native Windows; run Kubetail in WSL2"}, nil
}

// Command answers errNone and no command, so nothing runs unconfined in the
// sandbox's name.
func (s *Sandbox) Command(context.Context, Run) (*exec.Cmd, error) {
	return nil, errNone
}

// Close has nothing to free, since no command runs sandboxed.
func (s *Sandbox) Close() error { return nil }

// Confines reports whether a command run through s is confined: never, here.
func (s *Sandbox) Confines() bool { return false }

// NeedsResolver answers false, since no command runs sandboxed.
func (s *Sandbox) NeedsResolver() bool { return false }

// Port answers no port, since no command runs sandboxed.
func (s *Sandbox) Port() (int, error) { return 0, errNone }

// Never answers nothing, since no command runs sandboxed.
func (s *Sandbox) Never(string) []string { return nil }

// overFixedMount and worldWritable answer false, and broadDirs nothing, since
// no command runs sandboxed and a Windows mode is not a Unix permission.
func overFixedMount(string) bool     { return false }
func worldWritable(os.FileInfo) bool { return false }
func broadDirs(string) []string      { return nil }

// NoWrite answers nothing, since no command runs sandboxed.
func NoWrite(string) []string { return nil }

// FixedMount answers false, since no command runs sandboxed.
func FixedMount(string) bool { return false }

// System answers the zero System, since no command runs sandboxed.
func (s *Sandbox) System(string, string) System { return System{} }

// NetworkStatus answers no network, since no command runs sandboxed.
func (s *Sandbox) NetworkStatus() (bool, string) { return false, errNone.Error() }
