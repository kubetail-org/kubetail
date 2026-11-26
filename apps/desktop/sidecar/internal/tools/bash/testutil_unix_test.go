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

//go:build !windows

package bash

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/settings"
)

// sandboxed is this machine's sandbox, for a test that runs a command through
// it, or testutil.RequireSandbox's verdict where there is none.
func sandboxed(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	s, v, err := sandbox.Probe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	if v.Available {
		return s
	}
	testutil.RequireSandbox(t, "no sandbox: "+v.Reason)
	return nil
}

// confining is this machine's sandbox, for a test that needs it to confine,
// or testutil.RequireSandbox's verdict where there is none.
func confining(t *testing.T) *sandbox.Sandbox {
	t.Helper()
	s, v, err := sandbox.Probe(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	if s != nil && s.Confines() {
		return s
	}
	testutil.RequireSandbox(t, "no confining sandbox: "+v.Reason)
	return nil
}

// withTool puts name, where the machine has it, on tl's PATH through a folder
// the user typed that holds a link to it: the folder it is in may be one
// no run searches, as a CI runner's world-writable /usr/local/bin is. The
// folder is in a home of tl's own, since a typed folder under /tmp is refused
// on Linux and one in another home is never read. A machine without it skips
// the test.
func withTool(t *testing.T, tl *Tool, name string) {
	t.Helper()
	bin, err := exec.LookPath(name)
	if err != nil {
		t.Skip("no " + name + " on PATH")
	}
	target, err := filepath.EvalSymlinks(bin)
	require.NoError(t, err)
	tl.home = testutil.GrantableDir(t)
	dir := filepath.Join(tl.home, "bin")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Symlink(target, filepath.Join(dir, name)))
	list, err := tl.pathList(t.Context())
	require.NoError(t, err)
	list.Search = append([]settings.PathFolder{{Path: dir, Origin: settings.OriginTyped}}, list.Search...)
	tl.pathList = func(context.Context) (settings.RunPath, error) { return list, nil }
}
