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

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// socketPath returns a bindable AF_UNIX path for one test.
//
// Not t.TempDir(): it embeds the test's name, and a socket path over sun_path —
// 104 bytes on macOS — fails to bind with EINVAL.
func socketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sidecar")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

// unbindableEndpoint names a socket under a directory that does not exist.
func unbindableEndpoint(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "missing", "sidecar.sock")
}

// The full lifecycle: bind, announce READY, serve, then shut down on stdin EOF
// (the host's parent-died signal) and clean the socket file up. Serving is
// proven by a real request over the socket, which is also what makes the
// shutdown deterministic — no wall-clock wait for the server to come up.
func TestRunServesUntilStdinCloses(t *testing.T) {
	sock := socketPath(t)
	stdin, hostEnd := net.Pipe() // hostEnd.Close() is the host exiting
	out := &syncBuffer{}

	cfg := runArgs(t, "--socket", sock, "--runtime-dir", t.TempDir(), "--kubeconfig", filepath.Join(t.TempDir(), "none"))

	var code int
	var done sync.WaitGroup
	done.Go(func() {
		code = run(t.Context(), cfg, stdin, out)
	})

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	// The socket appears when run binds it; a served response proves Serve is up.
	require.Eventually(t, func() bool {
		resp, err := client.Get("http://sidecar/graphql")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 10*time.Second, 10*time.Millisecond, "sidecar never served over its socket")

	hostEnd.Close()
	done.Wait()

	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if got := out.String(); !strings.Contains(got, "READY unix:"+sock) {
		t.Errorf("stdout = %q, want the READY line for %s", got, sock)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Errorf("socket file still present after shutdown: %v", err)
	}
}

// syncBuffer collects run's stdout while the test reads it from another
// goroutine.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runSidecar runs run on its own goroutine over ctx and cfg, returning a client
// over its socket once it serves, and the func that ends it the way the host does
// — by closing its end of stdin — and returns the exit code.
func runSidecar(t *testing.T, ctx context.Context, cfg config) (*http.Client, func() int) {
	t.Helper()
	stdin, hostEnd := net.Pipe()
	out := &syncBuffer{}
	code := make(chan int, 1)
	go func() { code <- run(ctx, cfg, stdin, out) }()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", cfg.Socket)
		},
	}}
	require.Eventually(t, func() bool {
		resp, err := client.Get("http://sidecar/graphql")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return true
	}, 10*time.Second, 10*time.Millisecond, "sidecar never served over its socket")
	require.Contains(t, out.String(), "READY unix:"+cfg.Socket)

	return client, func() int {
		hostEnd.Close()
		return <-code
	}
}

// The context main hands run is the shutdown signals': ending it shuts the
// sidecar down cleanly, the way stdin's EOF does.
func TestRunShutsDownWhenItsContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir(), "--kubeconfig", filepath.Join(t.TempDir(), "none"))
	_, end := runSidecar(t, ctx, cfg)

	cancel()

	require.Equal(t, 0, end())
	_, err := os.Stat(cfg.Socket)
	require.True(t, os.IsNotExist(err), "socket file still present after shutdown")
}

// A quit before startup finishes is a clean exit, with no READY.
func TestRunExitsCleanlyWhenItsContextEndsDuringStartup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cfg := runArgs(t, "--socket", socketPath(t), "--runtime-dir", t.TempDir(), "--kubeconfig", filepath.Join(t.TempDir(), "none"))
	out := &syncBuffer{}

	code := run(ctx, cfg, strings.NewReader(""), out)

	require.Equal(t, 0, code)
	require.NotContains(t, out.String(), "READY")
}

// runMain starts the test binary as a sidecar running main over args (TestMain
// reads childEnv) and answers its exit code and what it wrote.
func runMain(t *testing.T, args ...string) (int, string) {
	t.Helper()
	return runMainAt(t, t.TempDir(), args...)
}

// runMainAt is runMain with home as HOME, the root of every folder the run
// places: the XDG variables are cleared, so none names a folder outside it.
func runMainAt(t *testing.T, home string, args ...string) (int, string) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), childEnv+"=main", "HOME="+home,
		"XDG_CONFIG_HOME=", "XDG_DATA_HOME=", "XDG_CACHE_HOME=", "XDG_STATE_HOME=")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "%s", out)
	return exit.ExitCode(), string(out)
}

// A sandbox subcommand is the forwarder's or the shell launcher's, run before
// the sidecar's flags are read.
func TestMainHandsASandboxSubcommandToTheSandbox(t *testing.T) {
	code, out := runMain(t, sandbox.ShellCommand)
	assert.NotEqual(t, 2, code, "the sidecar's flags were never parsed: %s", out)
	assert.Contains(t, out, sandbox.ShellCommand+": bad arguments")
}

// A bad flag exits 2, after the flag package says why.
func TestMainExitsTwoOnABadFlag(t *testing.T) {
	code, out := runMain(t, "--no-such-flag")
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "no-such-flag")
}

// -h prints the usage and exits 0, with no FATAL for a host to show.
func TestMainExitsZeroOnHelp(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-h")
	cmd.Env = append(os.Environ(), childEnv+"=main", "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()

	require.NoError(t, err, "%s", out)
	assert.Contains(t, string(out), "-runtime-dir")
	assert.NotContains(t, string(out), "FATAL")
}

// A sidecar with no host still logs to a file: it makes the layout's log
// folder owner-only and writes sidecar.log there.
func TestMainMakesTheLogFolderAndLogsThere(t *testing.T) {
	home := t.TempDir()
	logs := layoutOver(env{"HOME": home}, debugBuild).Logs

	code, out := runMainAt(t, home, "--socket", unbindableEndpoint(t), "--runtime-dir", t.TempDir())

	assert.Equal(t, 1, code, out)
	info, err := os.Stat(logs)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	b, err := os.ReadFile(filepath.Join(logs, "sidecar.log"))
	require.NoError(t, err)
	assert.Contains(t, string(b), "listen", "the run's error reached the file")
}

// The host's build profile, not the sidecar's, picks the leaf.
func TestMainTakesTheFoldersOfTheProfileItIsPassed(t *testing.T) {
	home := t.TempDir()
	for _, dev := range []bool{true, false} {
		logs := layoutOver(env{"HOME": home}, dev).Logs
		runMainAt(t, home, "--socket", unbindableEndpoint(t), "--runtime-dir", t.TempDir(), fmt.Sprintf("--dev=%t", dev))
		assert.FileExists(t, filepath.Join(logs, "sidecar.log"), "dev=%t", dev)
	}
}

// A root the sidecar cannot place folders on stops it before anything is
// written, and the reason reaches the host's start screen as FATAL.
func TestMainSendsARootItCannotUseAsFatal(t *testing.T) {
	code, out := runMainAt(t, "home/x", "--runtime-dir", t.TempDir())

	assert.Equal(t, 2, code, out)
	assert.Contains(t, out, `FATAL HOME is "home/x", not an absolute path`)
}

// A folder the sidecar cannot resolve stops it before the log opens, and the
// reason reaches the host's start screen as FATAL.
func TestMainSendsAFolderItCannotResolveAsFatal(t *testing.T) {
	code, out := runMainAt(t, t.TempDir())

	assert.Equal(t, 2, code, out)
	assert.Contains(t, out, `FATAL the runtime directory must be an absolute path, not ""`)
}

// A log file that cannot be opened leaves stderr as the log, which says so, and
// the run's own exit code is main's. A file where the log folder goes is no
// log folder.
func TestMainLogsToStderrWhenTheLogFileCannotOpen(t *testing.T) {
	home := t.TempDir()
	logs := layoutOver(env{"HOME": home}, debugBuild).Logs
	require.NoError(t, os.MkdirAll(filepath.Dir(logs), 0o700))
	require.NoError(t, os.WriteFile(logs, nil, 0o600))

	code, out := runMainAt(t, home, "--socket", unbindableEndpoint(t),
		"--runtime-dir", t.TempDir())

	assert.Equal(t, 1, code, out)
	assert.Contains(t, out, "no log file; logging to stderr only")
	assert.Contains(t, out, "listen", "the run's error reaches stderr too")
}

// The host names its socket as it names the runtime folder, through any link
// above it; SweepStale lists the resolved folder, so the sidecar's own socket
// is named there too, and only when it sits in that folder.
func TestOwnSocketIsNamedInTheResolvedRuntimeFolder(t *testing.T) {
	root, err := realpath.Canonical([]string{t.TempDir()})
	require.NoError(t, err)
	target := filepath.Join(root[0], "private", "var")
	require.NoError(t, os.MkdirAll(target, 0o700))
	linked := filepath.Join(root[0], "var")
	require.NoError(t, os.Symlink(target, linked))
	runtime := filepath.Join(linked, "kubetail-501")
	sock := filepath.Join(runtime, "kubetail-sidecar-1-1.sock")

	cfg := runArgs(t, "--socket", sock, "--runtime-dir", runtime)
	assert.Equal(t, filepath.Join(target, "kubetail-501", "kubetail-sidecar-1-1.sock"), ownSocket(cfg))

	elsewhere := filepath.Join(t.TempDir(), "s.sock")
	cfg.Socket = elsewhere
	assert.Equal(t, elsewhere, ownSocket(cfg))
}
