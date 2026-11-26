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
	"bytes"
	"context"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// testDeadline bounds a Resolve that should not need it. Every test using it
// ends on the shell's answer or exit, so it is only a backstop, and generous:
// under the race detector on a loaded runner, the command's two dozen spawns
// can take seconds.
const testDeadline = testutil.Timeout

func TestResolveImportsThePathTheShellBuilds(t *testing.T) {
	dir, plugin := fixturePlugin(t)
	useAccountShell(t, fakeShell(t, dir, "", ""))
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("KUBETAIL_UNRELATED", "kept")
	_, err := exec.LookPath(plugin)
	require.Error(t, err, "fixture must be off the inherited PATH")

	got, f := Resolve(context.Background(), plainStart, nil)
	require.Nil(t, f)
	require.Contains(t, got.Env["PATH"], dir)

	require.NoError(t, os.Setenv("PATH", got.Env["PATH"]))
	found, err := exec.LookPath(plugin)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, plugin), found)
	require.Equal(t, "kept", os.Getenv("KUBETAIL_UNRELATED"))
}

func TestResolveStopsAtTheLastMarker(t *testing.T) {
	dir, _ := fixturePlugin(t)
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	// A startup file backgrounds a process that inherits stdout, then the shell
	// answers and never exits. Neither sleep is a wait for time to pass: they
	// are a pipe nobody will close, which is the trap.
	pre := "sleep 300 &\necho $! > '" + pidFile + "'\n"
	useAccountShell(t, fakeShell(t, dir, pre, "sleep 300\n"))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f, "an answer in hand must not wait on the pipe closing")
	require.Contains(t, got.Env["PATH"], dir)

	// The kill goes to the group, so the grandchild dies with the shell.
	require.Eventually(t, func() bool {
		pid, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(pid)))
		return err == nil && syscall.Kill(n, 0) != nil
	}, testDeadline, 10*time.Millisecond)
}

func TestResolveImportsTheVariablesTheShellExports(t *testing.T) {
	dir, _ := fixturePlugin(t)
	// EvalSymlinks because pwd reports the physical directory, and a temp dir is
	// reached through a symlink on macOS.
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	// The second entry is relative, so it means nothing outside the shell that
	// reported it.
	pre := "export KUBECONFIG='/etc/k.yaml:.kube/work.yaml'\nexport AWS_PROFILE=work\n"
	useAccountShell(t, fakeShell(t, dir, pre, ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)
	require.Contains(t, got.Env["PATH"], dir)
	require.Equal(t, "/etc/k.yaml:"+filepath.Join(home, ".kube/work.yaml"), got.Env["KUBECONFIG"])
	require.Equal(t, "work", got.Env["AWS_PROFILE"])
}

// The keys asked for come back as the shell set them, apart from the allowlist;
// one unset or empty is left out.
func TestResolveReadsTheKeysItIsAskedFor(t *testing.T) {
	dir, _ := fixturePlugin(t)
	pre := "export ANTHROPIC_API_KEY='sk-ant shell'\nexport OPENAI_API_KEY=\n"
	useAccountShell(t, fakeShell(t, dir, pre, ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GROQ_API_KEY"})
	require.Nil(t, f)
	require.Contains(t, got.Env["PATH"], dir)
	require.Equal(t, map[string]string{"ANTHROPIC_API_KEY": "sk-ant shell"}, got.Keys)
	require.NotContains(t, got.Env, "ANTHROPIC_API_KEY")
}

func TestResolveResolvesAgainstTheDirectoryTheShellEndedIn(t *testing.T) {
	dir, _ := fixturePlugin(t)
	// EvalSymlinks because pwd reports the physical directory, and a temp dir is
	// reached through a symlink on macOS.
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	// A startup file that changes directory: the entry means the directory the
	// shell ended in, not the one it was started in.
	pre := "mkdir -p work\ncd work\nexport KUBECONFIG=config\n"
	useAccountShell(t, fakeShell(t, dir, pre, ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)
	require.Equal(t, filepath.Join(home, "work", "config"), got.Env["KUBECONFIG"])
}

// A shell that answers with a PATH that finds nothing is refused: installing it
// would leave the process unable to find any command.
func TestResolveRefusesAPathThatFindsNothing(t *testing.T) {
	dir, _ := fixturePlugin(t)
	useAccountShell(t, fakeShell(t, dir, "PATH=\n", ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	_, f := Resolve(ctx, plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, reasonBadOutput, f.Reason)
}

func TestResolveOmitsWhatTheShellDoesNotSet(t *testing.T) {
	dir, _ := fixturePlugin(t)
	useAccountShell(t, fakeShell(t, dir, "", ""))
	// Empty is unset as far as a frame goes, and whoever runs this may have
	// either one set for real.
	t.Setenv("KUBECONFIG", "")
	t.Setenv("AWS_PROFILE", "")

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)
	require.NotContains(t, got.Env, "KUBECONFIG")
	require.NotContains(t, got.Env, "AWS_PROFILE")
}

func TestResolveFallsBackWhenTheShellCannotAnswer(t *testing.T) {
	tests := map[string]struct {
		script string
		reason string
		code   int
	}{
		"exits without answering": {"#!/bin/sh\nexit 3\n", reasonShellExited, 3},
		"prints something else":   {"#!/bin/sh\necho 'command not found'\n", reasonBadOutput, 0},
		// One marker is not an answer; the reader must keep waiting for the second.
		"answers halfway": {"#!/bin/sh\n/usr/bin/printf '\\000" + marker + "\\000/usr/bin\\n'\n", reasonBadOutput, 0},
		"floods stdout":   {"#!/bin/sh\nawk 'BEGIN { printf \"%100000s\", \"\" }'\n", reasonOutputLimit, -1},
		// Draining stderr is what stops a full pipe blocking the shell, but a
		// stream this far past the cap is not output we understand.
		"floods stderr": {"#!/bin/sh\nawk 'BEGIN { printf \"%100000s\", \"\" }' >&2\nsleep 300\n", reasonOutputLimit, -1},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			useAccountShell(t, writeScript(t, "shell", tc.script))

			ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
			defer cancel()

			_, f := Resolve(ctx, plainStart, nil)
			require.NotNil(t, f)
			require.Equal(t, tc.reason, f.Reason)
			if tc.code >= 0 {
				require.Equal(t, tc.code, f.ExitCode)
			}
		})
	}
}

func TestTheShellRunsInASessionOfItsOwn(t *testing.T) {
	// The bug this pins only appears where a controlling terminal exists, which
	// `tauri dev` has and the test binary does not — so assert the detachment
	// itself rather than the hang it prevents.
	cmd := exec.Command("/bin/sh", "-c", "exec cat")
	cmd.SysProcAttr = shellProcAttr()
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})

	sid, err := unix.Getsid(cmd.Process.Pid)
	require.NoError(t, err)
	require.Equal(t, cmd.Process.Pid, sid, "the shell leads its own session")

	ours, err := unix.Getsid(0)
	require.NoError(t, err)
	require.NotEqual(t, ours, sid)
}

func TestResolveFallsBackWhenTheShellNeverAnswers(t *testing.T) {
	// The deadline is the assertion, not a wait: this shell never answers, so
	// only ctx can end the call. Short because production's ten seconds is the
	// caller's to choose.
	useAccountShell(t, writeScript(t, "shell", "#!/bin/sh\nsleep 300\n"))

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	_, f := Resolve(ctx, plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, reasonTimeout, f.Reason)
}

func TestResolveLeavesTheEnvironmentAloneOnFailure(t *testing.T) {
	useAccountShell(t, writeScript(t, "shell", "#!/bin/sh\nexit 1\n"))
	t.Setenv("PATH", "/usr/bin:/bin")

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	_, f := Resolve(ctx, plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, "/usr/bin:/bin", os.Getenv("PATH"), "a fallback must keep the inherited PATH")
}

func TestLaunchReportsAShellThatWillNotStart(t *testing.T) {
	_, f := Launch(t.Context(), plainStart, filepath.Join(t.TempDir(), "gone"), nil, nil, maxOutputBytes, func([]byte, int) bool { return true })
	require.NotNil(t, f)
	require.Equal(t, reasonNoShell, f.Reason)
	require.Equal(t, -1, f.ExitCode)
}

// A start whose command already writes its output somewhere leaves Launch no
// stream to read: the shell is never started.
func TestLaunchReportsAShellWhoseOutputIsTaken(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	for name, take := range map[string]func(*exec.Cmd){
		"stdout": func(c *exec.Cmd) { c.Stdout = io.Discard },
		"stderr": func(c *exec.Cmd) { c.Stderr = io.Discard },
	} {
		start := func(_ context.Context, shell string, args, _ []string) (*exec.Cmd, func(), error) {
			c := exec.Command(shell, args...)
			take(c)
			return c, func() {}, nil
		}
		_, f := Launch(t.Context(), start, "/bin/sh", []string{"-c", ": > '" + marker + "'"}, nil, maxOutputBytes, func([]byte, int) bool { return true })
		require.NotNil(t, f, name)
		require.Equal(t, reasonNoShell, f.Reason, name)
		require.NoFileExists(t, marker, name)
	}
}

func TestFindDeclinesARelativeSHELL(t *testing.T) {
	// Resolving it would mean searching a PATH the shell is the source of.
	t.Setenv("SHELL", "zsh")

	_, ok := Find()
	require.False(t, ok)
}

func TestFindTakesAnAbsoluteExecutableSHELL(t *testing.T) {
	shell := writeScript(t, "myshell", "#!/bin/sh\nexit 0\n")
	t.Setenv("SHELL", shell)
	got, ok := Find()
	require.True(t, ok)
	require.Equal(t, shell, got)

	notExecutable := filepath.Join(t.TempDir(), "zsh")
	require.NoError(t, os.WriteFile(notExecutable, nil, 0o600))
	t.Setenv("SHELL", notExecutable)
	_, ok = Find()
	require.False(t, ok)
}

func TestParseTakesEachVariablesFrame(t *testing.T) {
	want := map[string]string{
		"PATH":        "/usr/local/bin:/usr/bin",
		"KUBECONFIG":  "/home/ren/.kube/config",
		"AWS_PROFILE": "work",
	}

	got, ok := parseAnswer(markedEnv("/Users/ren", want))
	require.True(t, ok)
	require.Equal(t, "/Users/ren", got.dir)
	require.Equal(t, want, got.env)
}

func TestParseReadsAnEmptyFrameAsUnset(t *testing.T) {
	got, ok := parseAnswer(markedEnv("/Users/ren", map[string]string{"PATH": "/usr/bin"}))
	require.True(t, ok)
	require.NotContains(t, got.env, "KUBECONFIG")
}

func TestParseIgnoresOutputAroundTheMarkers(t *testing.T) {
	out := append([]byte("Welcome to zsh!\nnvm: loaded\n"), markedEnv("/Users/ren", map[string]string{"PATH": "/opt/bin"})...)
	out = append(out, "\nbye\n"...)

	got, ok := parseAnswer(out)
	require.True(t, ok)
	require.Equal(t, "/opt/bin", got.env["PATH"])
}

func TestParseKeepsAValueByteForByte(t *testing.T) {
	// Spaces, non-ASCII, a newline, and a trailing empty entry are all legal in a
	// directory name, and none of them is the shell's doing to undo. Only the one
	// newline printenv itself appends is removed, so an interior one is data.
	const path = "/Users/ren/Applications/My Tools:/opt/日本語/bin:/odd\nname:"

	got, ok := parseAnswer(markedEnv("/Users/ren", map[string]string{"PATH": path}))
	require.True(t, ok)
	require.Equal(t, path, got.env["PATH"])
}

func TestParseWaitsForTheLastMarker(t *testing.T) {
	partial := bytes.TrimSuffix(markedEnv("/Users/ren", map[string]string{"PATH": "/usr/bin"}), delimiter())

	_, ok := parseAnswer(partial)
	require.False(t, ok)
}

func TestParseRejectsUnusablePayloads(t *testing.T) {
	tests := map[string][]byte{
		"no markers at all":  []byte("command not found: printf\n"),
		"NUL inside a value": markedEnv("/Users/ren", map[string]string{"PATH": "/usr/bin\x00/opt/bin"}),
	}
	for name, out := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := parseAnswer(out)
			require.False(t, ok)
		})
	}
}

func TestResolveEnvAppliesTheKindOfEachVariable(t *testing.T) {
	got, ok := resolveEnv(map[string]string{
		"PATH":            "/usr/bin::bin",
		"KUBECONFIG":      "/etc/k.yaml:.kube/work.yaml",
		"AWS_PROFILE":     "work",
		"AWS_CONFIG_FILE": ".aws/config",
	}, "/Users/ren")

	require.True(t, ok)
	require.Equal(t, map[string]string{
		"PATH":            "/usr/bin:/Users/ren/bin",
		"KUBECONFIG":      "/etc/k.yaml:/Users/ren/.kube/work.yaml",
		"AWS_PROFILE":     "work",
		"AWS_CONFIG_FILE": "/Users/ren/.aws/config",
	}, got)
}

func TestResolveEnvRefusesAPathThatWouldFindNothing(t *testing.T) {
	tests := map[string]map[string]string{
		"missing":             {"AWS_PROFILE": "work"},
		"resolves to nothing": {"PATH": ":"},
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := resolveEnv(raw, "/Users/ren")
			require.False(t, ok)
		})
	}
}

func TestResolveEnvDropsAVariableThatResolvesToNothing(t *testing.T) {
	got, ok := resolveEnv(map[string]string{"PATH": "/usr/bin", "SSL_CERT_DIR": "::"}, "/Users/ren")

	require.True(t, ok)
	require.Equal(t, map[string]string{"PATH": "/usr/bin"}, got)
}

// A cwd the shell could not report is no base: a relative entry passes through.
func TestResolveEnvKeepsARelativeEntryWithNoDirectory(t *testing.T) {
	got, ok := resolveEnv(map[string]string{"PATH": "/usr/bin:bin"}, "")

	require.True(t, ok)
	require.Equal(t, map[string]string{"PATH": "/usr/bin:bin"}, got)
}

// A Wait that failed without the shell exiting has no status to report.
func TestExitCodeIsUnknownWithoutAnExit(t *testing.T) {
	require.Equal(t, 0, exitCode(nil))
	require.Equal(t, -1, exitCode(os.ErrProcessDone))
}

// markedEnv wraps a cwd and values the way the shell command does: a NUL-delimited
// marker before every frame and one after the last, with the trailing newline
// pwd and printenv add inside each. A name absent from values is an unset
// variable, whose frame is empty because printenv printed nothing.
func markedEnv(dir string, values map[string]string) []byte {
	out := append(delimiter(), dir+"\n"...)
	for _, v := range imported {
		out = append(out, delimiter()...)
		if value, ok := values[v.name]; ok {
			out = append(out, value+"\n"...)
		}
	}
	return append(out, delimiter()...)
}

func delimiter() []byte { return []byte("\x00" + marker + "\x00") }

// writeScript writes an executable script into a fresh temp dir and returns its path.
func writeScript(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(body), 0o700))
	return path
}

// fixturePlugin creates a stand-in credential plugin in its own directory, which
// nothing puts on the inherited PATH.
func fixturePlugin(t *testing.T) (dir, name string) {
	t.Helper()
	path := writeScript(t, "kubetail-fake-credential-plugin", "#!/bin/sh\nexit 0\n")
	return filepath.Dir(path), filepath.Base(path)
}

// fakeShell writes a script standing in for the user's login shell. It puts dir on
// the PATH the way a startup file would, emits noise, then runs the -c command for
// real — so the command constant itself is under test. pre and post are shell
// source run before and after it, for tests that need the shell to misbehave.
func fakeShell(t *testing.T, dir, pre, post string) string {
	t.Helper()
	return writeScript(t, "fake-login-shell", "#!/bin/sh\n"+
		"echo 'a noisy startup file' >&2\n"+
		"printf 'welcome to the shell\\n'\n"+
		"PATH='"+dir+"':\"$PATH\"\n"+
		"export PATH\n"+
		pre+
		"/bin/sh -c \"$4\"\n"+
		post)
}

func TestResolvePathMakesAnEntryMeaningfulOutsideTheShell(t *testing.T) {
	const home = "/Users/ren"
	tests := map[string]struct{ value, dir, want string }{
		"absolute untouched":         {"/opt/bin", home, "/opt/bin"},
		"relative joined":            {".kube/work.yaml", home, "/Users/ren/.kube/work.yaml"},
		"leading tilde untouched":    {"~/.kube/config", home, "~/.kube/config"},
		"empty dropped":              {"", home, ""},
		"no base passes through":     {".kube/work.yaml", "", ".kube/work.yaml"},
		"a colon is not a separator": {"/opt/a:b/bin", home, "/opt/a:b/bin"},
		// Cleaning `link/..` away lexically names a different file whenever link
		// is a symlink, so the entry keeps the components the shell reported.
		"a parent traversal is kept":  {"link/../config", home, "/Users/ren/link/../config"},
		"a base of / does not double": {"work.yaml", "/", "/work.yaml"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, resolvePath(tc.value, tc.dir))
		})
	}
}

func TestResolveListResolvesEveryEntry(t *testing.T) {
	const home = "/Users/ren"
	tests := map[string]struct{ value, want string }{
		"survivors rejoined": {"/opt/bin:.kube/work.yaml", "/opt/bin:/Users/ren/.kube/work.yaml"},
		"empties dropped":    {":/opt/bin::/bin:", "/opt/bin:/bin"},
		"tilde kept":         {"~/work.yaml:/b", "~/work.yaml:/b"},
		"nothing survives":   {"::", ""},
		"empty":              {"", ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveList(tc.value, home))
		})
	}
}

// The allowlist is the security boundary, so every row is pinned by name and kind:
// a row added without a reason beside it and a line in the security model fails
// here first.
func TestImportedIsExactlyTheAllowlist(t *testing.T) {
	require.Equal(t, []variable{
		{"PATH", pathList},
		{"KUBECONFIG", pathList},
		{"AWS_PROFILE", plain},
		{"AWS_REGION", plain},
		{"AWS_DEFAULT_REGION", plain},
		{"AWS_CONFIG_FILE", path},
		{"AWS_SHARED_CREDENTIALS_FILE", path},
		{"AWS_SDK_LOAD_CONFIG", plain},
		{"CLOUDSDK_CONFIG", path},
		{"CLOUDSDK_CORE_PROJECT", plain},
		{"CLOUDSDK_ACTIVE_CONFIG_NAME", plain},
		{"GOOGLE_CLOUD_PROJECT", plain},
		{"GOOGLE_CLOUD_LOCATION", plain},
		{"GOOGLE_APPLICATION_CREDENTIALS", path},
		{"AZURE_CONFIG_DIR", path},
		{"HTTP_PROXY", plain},
		{"HTTPS_PROXY", plain},
		{"NO_PROXY", plain},
		{"http_proxy", plain},
		{"https_proxy", plain},
		{"no_proxy", plain},
		{"SSL_CERT_FILE", path},
		{"SSL_CERT_DIR", pathList},
		{"OLLAMA_HOST", plain},
	}, imported)
}

// done is handed the whole read so far and where the latest read began, so a
// caller can look at the new bytes alone.
func TestLaunchHandsDoneWhereTheLatestReadBegan(t *testing.T) {
	shell := writeScript(t, "shell", "#!/bin/sh\nprintf 'one'\nprintf 'two'\nprintf 'END'\nsleep 300\n")
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	var seen int
	out, f := Launch(ctx, plainStart, shell, nil, nil, maxOutputBytes, func(buf []byte, from int) bool {
		require.Equal(t, seen, from)
		seen = len(buf)
		return bytes.HasSuffix(buf, []byte("END"))
	})
	require.Nil(t, f)
	require.Equal(t, "onetwoEND", string(out))
}

func TestLaunchKeepsItsCap(t *testing.T) {
	shell := writeScript(t, "shell", "#!/bin/sh\nawk 'BEGIN { printf \"%2000s\", \"\" }'\nsleep 300\n")
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	_, f := Launch(ctx, plainStart, shell, nil, nil, 1000, func([]byte, int) bool { return false })
	require.NotNil(t, f)
	require.Equal(t, reasonOutputLimit, f.Reason)
}

// A caller off this package holds a Fault as an error, its reason the message.
func TestAFaultIsAnError(t *testing.T) {
	var err error = fault(reasonTimeout)
	require.Equal(t, reasonTimeout, err.Error())
}

// Launch runs the shell with the arguments and the environment it is handed, and
// nothing of the process's.
func TestLaunchRunsWithWhatItIsGiven(t *testing.T) {
	t.Setenv("KUBETAIL_TEST_PROCESS_VAR", "leaked")
	shell := writeScript(t, "shell", "#!/bin/sh\nprintf '%s|%s|%s|' \"$*\" \"$GIVEN\" \"$KUBETAIL_TEST_PROCESS_VAR\"\nprintf END\nsleep 300\n")
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	out, f := Launch(ctx, plainStart, shell, []string{"-l", "-c", "cmd"}, []string{"GIVEN=yes"}, maxOutputBytes, func(buf []byte, _ int) bool {
		return bytes.HasSuffix(buf, []byte("END"))
	})
	require.Nil(t, f)
	require.Equal(t, "-l -c cmd|yes||END", string(out))
}

// Resolve runs the account record's shell, whatever $SHELL names.
func TestResolveRunsTheAccountShell(t *testing.T) {
	dir, _ := fixturePlugin(t)
	t.Setenv("SHELL", writeScript(t, "shell", "#!/bin/sh\nexit 7\n"))
	useAccountShell(t, fakeShell(t, dir, "", ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)
	require.Contains(t, got.Path, dir)
}

// Path is PATH as the shell exported it, split, with nothing resolved or dropped.
func TestResolveAnswersThePathAsExported(t *testing.T) {
	dir, _ := fixturePlugin(t)
	useAccountShell(t, fakeShell(t, dir, "PATH=\"$PATH:bin::~/x\"\n", ""))

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	got, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)
	want := append(append([]string{dir}, filepath.SplitList(DefaultPath)...), "bin", "", "~/x")
	require.Equal(t, want, got.Path)
}

// The resolution's shell sees a fixed list and nothing else of the process's,
// so a dev run from a terminal and a Finder launch resolve the same PATH.
func TestTheShellsEnvironmentIsScrubbed(t *testing.T) {
	dir, _ := fixturePlugin(t)
	seen := filepath.Join(t.TempDir(), "env")
	shell := writeScript(t, "shell", "#!/bin/sh\n/usr/bin/env > '"+seen+"'\n/bin/sh -c \"$4\"\n")
	useAccountShell(t, shell)
	kept := map[string]string{
		"HOME": t.TempDir(), "USER": "ren", "LOGNAME": "ren", "TMPDIR": t.TempDir(), "LANG": "en_US.UTF-8", "TZ": "UTC",
		"SSH_AUTH_SOCK": "/tmp/agent.sock", "SSH_AGENT_PID": "42", "GPG_AGENT_INFO": "/tmp/gpg:1:1", "XDG_RUNTIME_DIR": t.TempDir(),
	}
	for name, value := range kept {
		t.Setenv(name, value)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("KUBETAIL_TEST_LEAK", "leaked")
	t.Setenv("LC_ALL", "C")

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()
	_, f := Resolve(ctx, plainStart, nil)
	require.Nil(t, f)

	out, err := os.ReadFile(seen)
	require.NoError(t, err)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		name, value, _ := strings.Cut(line, "=")
		switch name {
		case "PWD", "OLDPWD", "SHLVL", "_":
			// The shell sets these itself.
		default:
			got[name] = value
		}
	}
	want := map[string]string{"SHELL": shell, "TERM": "dumb", "DISABLE_AUTO_UPDATE": "true", "PATH": DefaultPath}
	maps.Copy(want, kept)
	require.Equal(t, want, got)
}

// With no record and none of the platform's shells, there is nothing to ask.
func TestResolveFallsBackWhenThereIsNoShell(t *testing.T) {
	useAccountShell(t, "")
	defer func(shells []string) { defaultShells = shells }(defaultShells)
	defaultShells = []string{filepath.Join(t.TempDir(), "zsh")}

	_, f := Resolve(t.Context(), plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, reasonNoShell, f.Reason)
	require.Equal(t, -1, f.ExitCode)
}

// parseAnswer is parse over a posix run's frames, read as an answer.
func parseAnswer(out []byte) (answer, bool) {
	frames, ok := parse(out, posix(nil).frames)
	if !ok {
		return answer{}, false
	}
	return answerOf(frames, nil), true
}

// Each kind of shell is run with its own flags and asked in its own language,
// and either answers the PATH it built.
func TestResolveReadsEachShellKind(t *testing.T) {
	m := `\000` + marker + `\000`
	tests := map[string]struct {
		flags, answer string
		env           map[string]string
	}{
		// The posix fake runs the command it is handed, so the command is under test.
		"zsh": {"-i -l -c", "PATH=/a:/b /bin/sh -c \"$4\"", map[string]string{"PATH": "/a:/b"}},
		// A fake cannot speak nushell, so it answers as the nushell command would.
		"nu": {"-l -c", "/usr/bin/printf '" + m + "/home\\n" + m + "/a:/b\\n" + m + "'", map[string]string{}},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			shell := filepath.Join(t.TempDir(), name)
			require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\n"+
				"[ \"$1 $2 $3\" = \""+tc.flags+"\" ] || [ \"$1 $2\" = \""+tc.flags+"\" ] && [ $# = "+
				strconv.Itoa(len(strings.Fields(tc.flags))+1)+" ] || exit 9\n"+tc.answer+"\n"), 0o700))
			useAccountShell(t, shell)

			ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
			defer cancel()
			got, f := Resolve(ctx, plainStart, nil)
			require.Nil(t, f)
			require.Equal(t, []string{"/a", "/b"}, got.Path)
			require.Equal(t, tc.env, got.Env)
		})
	}
}

// Path is Resolve's PATH, or the Fault as its error.
func TestAFaultAnswersNoPath(t *testing.T) {
	dir, _ := fixturePlugin(t)
	useAccountShell(t, fakeShell(t, dir, "", ""))
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()
	path, err := Path(ctx, plainStart)
	require.NoError(t, err)
	require.Equal(t, dir, path[0])

	for script, reason := range map[string]string{
		"#!/bin/sh\nexit 3\n":                   reasonShellExited,
		"#!/bin/sh\necho 'command not found'\n": reasonBadOutput,
	} {
		useAccountShell(t, writeScript(t, "shell", script))
		path, err := Path(ctx, plainStart)
		require.Nil(t, path)
		var f *Fault
		require.ErrorAs(t, err, &f)
		require.Equal(t, reason, f.Reason)
	}
}

// A nushell that answers an empty PATH is bad output, as a posix one is.
func TestResolveRefusesAnEmptyNuPath(t *testing.T) {
	m := `\000` + marker + `\000`
	shell := filepath.Join(t.TempDir(), "nu")
	require.NoError(t, os.WriteFile(shell, []byte("#!/bin/sh\n/usr/bin/printf '"+m+"/home\\n"+m+"\\n"+m+"'\n"), 0o700))
	useAccountShell(t, shell)

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()
	_, f := Resolve(ctx, plainStart, nil)
	require.NotNil(t, f)
	require.Equal(t, reasonBadOutput, f.Reason)
}
