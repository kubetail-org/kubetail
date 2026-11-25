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

package bash

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// node reserves about 10 GiB of address space for a WebAssembly memory, which
// fits under the memory limit.
func TestNodeRunsUnderTheMemoryLimit(t *testing.T) {
	tl := proxyTool(t, &fakeLease{})
	tl.sandboxer = confining(t)
	withTool(t, tl, "node")

	text, isError := tl.Run(t.Context(), testRuntime(t), command(`node -e 'new WebAssembly.Memory({initial: 1})'`))

	require.False(t, isError, text)
}

// A call with the internet runs through the real sandbox under pasta: it
// reads the resolver its run wrote at /etc/resolv.conf, holds no capability,
// and answers as any call does.
func TestANetworkCallRunsUnderPasta(t *testing.T) {
	tl := proxyTool(t, &fakeLease{})
	s := confining(t)
	if available, reason := s.NetworkStatus(); !available {
		testutil.RequireSandbox(t, "no network for a run: "+reason)
	}
	tl.sandboxer = s

	text, isError := tl.RunApproved(t.Context(), testRuntime(t), command(`cat /etc/resolv.conf; grep CapEff /proc/self/status`),
		tools.Approval{Sandboxed: true, Network: session.NetworkApproved})

	require.False(t, isError, text)
	assert.Equal(t, "nameserver "+sandbox.ResolverAddress+"\nCapEff:\t0000000000000000\n", text)
}

// Without XDG_RUNTIME_DIR Kubetail's runtime directory is /tmp/kubetail-<uid>: a
// run's own paths there are inside a fixed mount no grant may name, and the
// run, with a grant beside them, still starts.
func TestARunStartsWithItsRuntimeDirUnderTmp(t *testing.T) {
	tl := confiningTool(t)
	runtime, err := os.MkdirTemp("/tmp", "kubetail-"+strconv.Itoa(os.Getuid())+"-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	tl.runsDir = filepath.Join(runtime, "runs")
	require.NoError(t, os.MkdirAll(tl.runsDir, 0o700))
	tl.denied = append(tl.denied, runtime)
	assert.True(t, sandbox.FixedMount(runtime))

	// Under a home of the test's own: the real one is another user's to it.
	home, p := folders(t, "code")
	tl.home = home
	rt := testRuntime(t)
	rt.Session.Folders = func(context.Context) ([]session.Folder, error) { return []session.Folder{{Path: p[0]}}, nil }
	text, isError := tl.Run(t.Context(), rt, command("ls "+p[0]+" && echo ok"))
	require.False(t, isError, text)
	assert.Equal(t, "ok\n", text)
}
