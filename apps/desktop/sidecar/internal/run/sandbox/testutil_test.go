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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// confining is this machine's sandbox, for a test that needs it to confine,
// or testutil.RequireSandbox's verdict where there is none.
func confining(t *testing.T) *Sandbox {
	t.Helper()
	s, v, err := Probe(t.Context())
	require.NoError(t, err)
	closing(t, s)
	if s != nil && s.Confines() {
		return s
	}
	why := "no sandbox: " + v.Reason
	if s != nil {
		why = "the sandbox confines nothing: " + v.Reason
	}
	testutil.RequireSandbox(t, why)
	return nil
}

// closing closes s, this one whatever the variable holds later, when the
// test ends.
func closing(t *testing.T, s *Sandbox) {
	t.Cleanup(func() { _ = s.Close() })
}

// command is the command s makes for r, which must make one.
func command(t *testing.T, s *Sandbox, ctx context.Context, r Run) *exec.Cmd {
	t.Helper()
	cmd, err := s.Command(ctx, r)
	require.NoError(t, err)
	return cmd
}

// mkdirs makes each path under base, and answers them joined to it.
func mkdirs(t *testing.T, base string, rels ...string) []string {
	t.Helper()
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = filepath.Join(base, filepath.FromSlash(r))
		require.NoError(t, os.MkdirAll(out[i], 0o700))
	}
	return out
}

// coverWarning is what this test binary prints as it exits under coverage,
// as a run's forwarder or a helper a run starts, since its GOCOVERDIR is
// outside the sandbox.
const coverWarning = "warning: GOCOVERDIR not set, no coverage data emitted\n"

// withoutCoverWarning is out less every coverWarning in it.
func withoutCoverWarning(out []byte) string {
	return strings.ReplaceAll(string(out), coverWarning, "")
}

// canonicalOf is paths, canonical.
func canonicalOf(t *testing.T, paths ...string) []string {
	t.Helper()
	c, err := realpath.Canonical(paths)
	require.NoError(t, err)
	return c
}

// compiledRules is the rules p compiles to, unchecked.
func compiledRules(t *testing.T, p Policy) []rule {
	t.Helper()
	c, err := p.canonicalPaths()
	require.NoError(t, err)
	return p.rules(c)
}

// checked is why r cannot start, or nil.
func checked(r Run) error {
	_, err := r.check()
	return err
}
