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

//go:build unix

package loginshell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// Commander builds a sandboxed command: *sandbox.Sandbox, or a test's fake.
type Commander interface {
	Command(ctx context.Context, r sandbox.Run) (*exec.Cmd, error)
}

// TempDir makes one run's TMPDIR, the one folder the login shell writes, and
// the cleanup that removes it once the shell has exited.
type TempDir func() (dir string, cleanup func(), err error)

// errNoScratch is a TMPDIR that could not be made.
var errNoScratch = errors.New("no scratch folder")

// In is the Start that runs the login shell through sb: it reads everything but
// deny and Kubetail's directories, writes nothing but the TMPDIR tmp makes for the
// run, and reaches no network. A nil sb is no sandbox, and the shell runs as a
// plain command. deny is the run's denied-always list: the PATH resolution's
// output leaves the sandbox, so it passes the sandbox's Never list; the
// snapshot's reaches only commands that read the home anyway, so it passes none.
func In(sb Commander, deny, kubetailDirs []string, tmp TempDir) Start {
	if sb == nil {
		return plainStart
	}
	return func(ctx context.Context, name string, args, env []string) (*exec.Cmd, func(), error) {
		scratch, cleanup, err := tmp()
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %w", errNoScratch, err)
		}
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		cmd, err := sb.Command(ctx, sandbox.Run{
			Shell: name, Args: args, Dir: home, Env: sandboxedEnv(env, scratch),
			Policy: sandbox.Policy{
				Files:  sandbox.FilePolicy{Read: readable()},
				Always: sandbox.AlwaysPolicy{Deny: deny, Kubetail: kubetailDirs, Write: []string{scratch}},
			},
		})
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		return cmd, cleanup, nil
	}
}

// readable is what the login shell reads: everything, and this executable at
// its resolved path, since the sandbox runs it as the run's forwarder and a run's
// private /tmp would hide one there.
func readable() []string {
	read := []string{"/"}
	if self, err := os.Executable(); err == nil {
		read = append(read, realpath.Resolved(self))
	}
	return read
}

// sandboxedEnv is env less every variable the sandbox refuses to pass, with
// TMPDIR the scratch folder.
func sandboxedEnv(env []string, scratch string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if name != "TMPDIR" && !sandbox.Unpassable(name) {
			out = append(out, kv)
		}
	}
	return append(out, "TMPDIR="+scratch)
}

// plainStart runs the shell with nothing but Launch's kill to end it.
func plainStart(_ context.Context, name string, args, env []string) (*exec.Cmd, func(), error) {
	cmd := exec.Command(name, args...)
	cmd.Dir, _ = os.UserHomeDir()
	// Never nil, which exec reads as the process's environment.
	cmd.Env = append([]string{}, env...)
	return cmd, func() {}, nil
}
