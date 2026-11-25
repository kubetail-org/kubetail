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
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/oskeyring/oskeyringtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/providercredentials"
)

// The test binary is started as a sidecar by the tests that need one of its
// own: "main" runs main, "scrub" runs the re-exec and reports what it yields.
const childEnv = "KUBETAIL_SIDECAR_TEST_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "main":
		main()
	case "scrub":
		testutil.ExitHelper(scrubChild())
	}
	// run's sandbox probe starts this binary as sandbox-init and sandbox-shell.
	if code, ok := sandbox.Main(os.Args); ok {
		testutil.ExitHelper(code)
	}
	os.Exit(m.Run())
}

// scrubChild runs the re-exec as main does, prints the providers whose
// environment key it holds, then waits for its stdin to close so the test can
// read its launch strings while it lives.
func scrubChild() int {
	args, getenv, err := scrubExecEnvironment()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := configFromArgs(args[1:], getenv)
	if err != nil {
		return 2
	}
	keys, err := providercredentials.New(providercredentials.Config{Keyring: oskeyringtest.New(), Providers: catalog.Providers(nil, ""), EnvKeys: cfg.App.LLMKeys})
	if err != nil {
		return 2
	}
	defer keys.Close()
	<-keys.Restored()
	states, stop := keys.WatchCredentialsState()
	defer stop()
	var ids []string
	for _, st := range <-states {
		if st.Source == providercredentials.SourceEnvironmentKey {
			ids = append(ids, st.ID)
		}
	}
	fmt.Println(strings.Join(ids, " "))
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

const fakeKey = "sk-ant-test-0123456789abcdef"

// The re-exec'd sidecar carries no key in the strings the kernel kept from the
// launch, and still holds the key as its provider's environment key.
func TestTheReexecKeepsTheKeyOutOfTheLaunchStrings(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "--socket", "/nowhere")
	cmd.Env = append(os.Environ(), childEnv+"=scrub", "ANTHROPIC_API_KEY="+fakeKey, "HOME="+t.TempDir())
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	// The line prints after the re-exec, in the image whose strings are read,
	// and only a child that kept the rest of its environment reaches it:
	// TestMain reads childEnv to get there.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	assert.Contains(t, strings.Fields(line), "anthropic", "the provider is keyed from the environment")

	launch := launchStrings(t, cmd.Process.Pid)
	assert.NotContains(t, launch, fakeKey, "the value is out of the launch strings")
	assert.NotContains(t, launch, "ANTHROPIC_API_KEY", "and so is the name")
}

// A --keys-fd that is not a readable pipe stops the sidecar with an error, in
// main, rather than start it without the keys.
func TestABadKeysFDStopsTheSidecar(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	for _, fd := range []string{"--keys-fd=999", "--keys-fd=x"} {
		cmd := exec.Command(exe, "--socket", "/nowhere", fd)
		cmd.Env = append(os.Environ(), childEnv+"=main")
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, "%s: %s", fd, out)
		assert.Equal(t, 2, exit.ExitCode(), "%s: %s", fd, out)
		assert.Contains(t, string(out), fd)
	}
}

// With no key in the environment there is nothing to hide: no exec, and the
// arguments and environment pass through.
func TestNoKeyMeansNoReexec(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:11434")
	execd := false
	args, getenv, err := scrubExec([]string{"sidecar", "--socket", "s"}, []string{"OLLAMA_HOST=x"}, func(string, []string, []string) error {
		execd = true
		return nil
	})
	require.NoError(t, err)
	assert.False(t, execd)
	assert.Equal(t, []string{"sidecar", "--socket", "s"}, args)
	assert.Equal(t, "http://127.0.0.1:11434", getenv("OLLAMA_HOST"))
}

// A key in the environment execs the binary with the same arguments plus the
// pipe's number, the key variables left out and the rest kept; the pipe holds
// the keys and survives the exec. An exec that fails is the start failing.
func TestAKeyInTheEnvironmentIsMovedToThePipe(t *testing.T) {
	execErr := errors.New("exec refused")
	var gotArgv, gotEnv []string
	var gotKeys map[string]string
	var cloexec int
	exe, err := os.Executable()
	require.NoError(t, err)
	_, _, err = scrubExec(
		[]string{"sidecar", "--socket", "s"},
		[]string{"OLLAMA_HOST=x", "ANTHROPIC_API_KEY=" + fakeKey, "OPENAI_API_KEY=sk-openai-0123456789"},
		func(argv0 string, argv, envv []string) error {
			assert.Equal(t, exe, argv0)
			gotArgv, gotEnv = argv, envv
			fd, err := strconv.Atoi(strings.TrimPrefix(argv[len(argv)-1], keysFDFlag))
			require.NoError(t, err)
			cloexec, err = unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
			require.NoError(t, err)
			// A dup of its own: scrubExecEnvironment closes fd once the exec fails.
			dup, err := unix.Dup(fd)
			require.NoError(t, err)
			f := os.NewFile(uintptr(dup), "keys")
			defer f.Close()
			payload, err := io.ReadAll(f)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(payload, &gotKeys))
			return execErr
		})

	require.ErrorIs(t, err, execErr)
	assert.Equal(t, []string{"sidecar", "--socket", "s"}, gotArgv[:3])
	assert.True(t, strings.HasPrefix(gotArgv[3], keysFDFlag), "%q", gotArgv)
	assert.Equal(t, []string{"OLLAMA_HOST=x"}, gotEnv)
	assert.Equal(t, map[string]string{"ANTHROPIC_API_KEY": fakeKey, "OPENAI_API_KEY": "sk-openai-0123456789"}, gotKeys)
	assert.Zero(t, cloexec&unix.FD_CLOEXEC, "the pipe survives the exec")
}

// A key too large for the pipe is an error before the write, never a write that
// blocks with nothing reading until the exec.
func TestAnOversizedKeyIsAnErrorNotAHang(t *testing.T) {
	execd := false
	_, _, err := scrubExec([]string{"sidecar"}, []string{"ANTHROPIC_API_KEY=" + strings.Repeat("k", 1<<20)}, func(string, []string, []string) error {
		execd = true
		return nil
	})
	require.ErrorContains(t, err, "fit")
	assert.False(t, execd)
}

// In the new image the flag names the pipe: the keys answer from it, the flag
// leaves the arguments, and the rest of the environment is the process's own.
func TestTheKeysFDFeedsTheConfig(t *testing.T) {
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:11434")
	r, err := writeKeysPipe(map[string]string{"ANTHROPIC_API_KEY": fakeKey})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	fd := dupFD(t, r)

	args, getenv, err := scrubExec([]string{"sidecar", keysFDFlag + strconv.Itoa(fd), "--socket", "s"}, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"sidecar", "--socket", "s"}, args)
	assert.Equal(t, fakeKey, getenv("ANTHROPIC_API_KEY"))
	assert.Equal(t, "http://127.0.0.1:11434", getenv("OLLAMA_HOST"))
	cfg := mustConfigFromArgs(t, args[1:], env{"ANTHROPIC_API_KEY": getenv("ANTHROPIC_API_KEY")})
	assert.Equal(t, map[string]string{"anthropic": fakeKey}, cfg.App.LLMKeys)
}

// A flag naming anything but a readable pipe is an error, never a keyless start.
func TestAKeysFDThatIsNotAPipeIsAnError(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "keys")
	require.NoError(t, err)
	t.Cleanup(func() { _ = file.Close() })
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_, err = w.WriteString("not json")
	require.NoError(t, err)
	_ = w.Close()
	t.Cleanup(func() { _ = r.Close() })

	// A pipe's write end passes the pipe check and fails the read.
	_, writeEnd, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = writeEnd.Close() })

	for _, fd := range []string{strconv.Itoa(dupFD(t, file)), "999", "x", strconv.Itoa(dupFD(t, r)), strconv.Itoa(dupFD(t, writeEnd))} {
		_, _, err := scrubExec([]string{"sidecar", keysFDFlag + fd}, nil, nil)
		assert.Error(t, err, fd)
	}
}

// dupFD is a descriptor of f's own for the code under test to take and close,
// so f stays the test's to close and never holds a number another file reuses.
func dupFD(t *testing.T, f *os.File) int {
	t.Helper()
	fd, err := unix.Dup(int(f.Fd()))
	require.NoError(t, err)
	return fd
}

// The flag is read wherever it sits, and only the flag leaves.
func TestTheFlagIsTakenFromAnywhereInTheArguments(t *testing.T) {
	rest, fd, found, err := takeKeysFD([]string{"sidecar", "--keys-fd=7", "--socket", "s"})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 7, fd)
	assert.Equal(t, []string{"sidecar", "--socket", "s"}, rest)

	rest, _, found, err = takeKeysFD([]string{"sidecar", "--socket", "s"})
	require.NoError(t, err)
	assert.False(t, found)
	assert.Equal(t, []string{"sidecar", "--socket", "s"}, rest)
}

// Every set variable in the table is moved, and only those: an empty one and a
// name beside the table stay. Of a name set twice the first wins, as it does
// for os.Getenv, and neither copy stays.
func TestSplitKeysTakesExactlyTheTable(t *testing.T) {
	var environ []string
	for _, name := range catalog.KeyVars() {
		environ = append(environ, name+"=v")
	}
	environ = append(environ, "PATH=/bin", "ANTHROPIC_API_KEY_BACKUP=keep", "ANTHROPIC_API_KEY=second")

	keys, kept := splitKeys(environ)

	assert.ElementsMatch(t, slices.Collect(maps.Values(catalog.KeyVars())), slices.Collect(maps.Keys(keys)))
	assert.Equal(t, "v", keys["ANTHROPIC_API_KEY"])
	assert.Equal(t, []string{"PATH=/bin", "ANTHROPIC_API_KEY_BACKUP=keep"}, kept)

	keys, kept = splitKeys([]string{"ANTHROPIC_API_KEY=", "PATH=/bin"})
	assert.Empty(t, keys, "an empty variable hides nothing")
	assert.Equal(t, []string{"ANTHROPIC_API_KEY=", "PATH=/bin"}, kept)
}

// A key variable set beside the flag would stay in the launch strings, since
// the re-exec is behind it: an error, never a start.
func TestAKeyBesideTheFlagIsAnError(t *testing.T) {
	r, err := writeKeysPipe(map[string]string{"ANTHROPIC_API_KEY": fakeKey})
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	_, _, err = scrubExec([]string{"sidecar", keysFDFlag + strconv.Itoa(dupFD(t, r))}, []string{"OPENAI_API_KEY=sk-openai-0123456789"}, nil)
	require.ErrorContains(t, err, "beside")
}
