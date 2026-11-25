// Copyright 2024 The Kubetail Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build unix

// Package loginshell runs the user's login shell. Launch runs one command in it,
// through a Start that confines it where the machine has a sandbox (In), and
// the bash tool's profile snapshot uses it on every Unix. Resolve runs the
// account's login shell once, in a scrubbed environment, and reads three things
// from it: the PATH the sandbox finds programs on; an allowlisted set of the
// environment, which a macOS GUI launch does not inherit from launchd: without
// it, kubeconfig `exec` credential plugins on the shell PATH are not found, and
// the ones that are run against the wrong identity; and the model-provider keys
// the caller names, which a GUI launch does not inherit either.
package loginshell

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds the whole resolution — spawn, read, kill, reap. A shell
// that answers ends it early, so the bound costs time only for a slow one, and a
// failure leaves macOS without the environment its credential plugins need. Long
// enough for startup files that do real work, short enough that a stalled one
// does not hold up the sidecar's READY line for long.
const DefaultTimeout = 10 * time.Second

// marker delimits the PATH in the shell's stdout. It must begin with a letter:
// printf reads `\0` as the start of an octal escape, so a marker starting 0-7
// would be swallowed into it and vanish.
const marker = "kubetail_loginshell_v1"

// kind says what a variable's value is, and is the whole of the per-variable
// knowledge: a path is resolved, a list is split on `:` first, a plain value is
// copied through.
type kind int

const (
	plain kind = iota
	path
	pathList
)

type variable struct {
	name string
	kind kind
}

// imported is the allowlist, and it is the security boundary. We spawn credential
// plugins as children, so every name here is one a startup file can set inside
// them: deny by default, no prefix wildcards, and adding a row is a security
// change. The two SSL_CERT_* rows serve those children alone — Go reads them in
// crypto/x509/root_unix.go, which is not built on darwin, the one platform this
// runs on.
var imported = []variable{
	{"PATH", pathList},                       // finds the plugin binary
	{"KUBECONFIG", pathList},                 // which kubeconfig files exist at all
	{"AWS_PROFILE", plain},                   // which identity the plugin assumes
	{"AWS_REGION", plain},                    // which region it authenticates against
	{"AWS_DEFAULT_REGION", plain},            // the same, as the SDK's own spelling
	{"AWS_CONFIG_FILE", path},                // where those profiles are defined
	{"AWS_SHARED_CREDENTIALS_FILE", path},    // where their credentials are
	{"AWS_SDK_LOAD_CONFIG", plain},           // whether the profile file is read at all
	{"CLOUDSDK_CONFIG", path},                // gcloud's config, holding the active account
	{"CLOUDSDK_CORE_PROJECT", plain},         // which project gke-gcloud-auth-plugin targets
	{"CLOUDSDK_ACTIVE_CONFIG_NAME", plain},   // which gcloud configuration is active
	{"GOOGLE_CLOUD_PROJECT", plain},          // which project Vertex AI is signed in
	{"GOOGLE_CLOUD_LOCATION", plain},         // which Vertex AI location serves it
	{"GOOGLE_APPLICATION_CREDENTIALS", path}, // a service-account key
	{"AZURE_CONFIG_DIR", path},               // the Azure CLI's token cache
	{"HTTP_PROXY", plain},                    // reaching the API server from a corporate network
	{"HTTPS_PROXY", plain},                   // the same, for TLS
	{"NO_PROXY", plain},                      // a list, but not of paths
	{"http_proxy", plain},                    // Go's ProxyFromEnvironment honours both spellings
	{"https_proxy", plain},                   // the same
	{"no_proxy", plain},                      // the same
	{"SSL_CERT_FILE", path},                  // a private CA, for the plugins we spawn
	{"SSL_CERT_DIR", pathList},               // the same, as a directory list
	{"OLLAMA_HOST", plain},                   // where the user's Ollama daemon is; an endpoint, not a credential
}

// buildCommand builds the only thing the shell is asked to run, from imported,
// the caller's key names and constants and nothing else — no part of it comes
// from a kubeconfig, cluster data, or the socket. Both utilities are absolute
// because a builtin printf varies by shell, echo cannot emit a NUL, and looking
// either one up would search the very PATH we are here to replace. It parses
// unchanged under zsh, bash, and fish (whose `||` dates from 3.0).
//
// It frames the shell's own cwd, then one printenv per imported variable, then
// one per key, between markers. The cwd leads because a startup file may have
// changed it, and a relative value the shell reports means the directory it
// ended in. printenv exits 1 for an unset variable, so `|| true` stops a
// startup file's `set -e` from ending the run there.
func buildCommand(keys []string) string {
	var b strings.Builder
	b.WriteString(`/usr/bin/printf '\000` + marker + `\000'; /bin/pwd || true; `)
	printenv := func(name string) {
		b.WriteString(`/usr/bin/printf '\000` + marker + `\000'; `)
		b.WriteString(`/usr/bin/printenv ` + name + ` || true; `)
	}
	for _, v := range imported {
		printenv(v.name)
	}
	for _, key := range keys {
		printenv(key)
	}
	b.WriteString(`/usr/bin/printf '\000` + marker + `\000'`)
	return b.String()
}

// nuCommand is command's two frames in nushell: the cwd, then PATH, a list
// there, joined as the posix shells export it.
var nuCommand = `^/usr/bin/printf '\000` + marker + `\000'; ^/bin/pwd; ` +
	`^/usr/bin/printf '\000` + marker + `\000'; $env.PATH | str join ':' | print; ` +
	`^/usr/bin/printf '\000` + marker + `\000'`

// A shellKind is how a family of shells is asked: its arguments, how many
// frames its command prints, and what those frames answer for the keys asked.
type shellKind struct {
	args   []string
	frames int
	read   func(frames [][]byte, keys []string) (Result, bool)
}

// posix is every shell but nushell: fish parses the command too.
func posix(keys []string) shellKind {
	return shellKind{args: InteractiveLogin(buildCommand(keys)), frames: 1 + len(imported) + len(keys), read: readPosix}
}

// nu reads env.nu, config.nu and login.nu under -c only as a login shell.
var nu = shellKind{args: []string{"-l", "-c", nuCommand}, frames: 2, read: readNu}

// kindOf is the kind shell is, read off its base name.
func kindOf(shell string, keys []string) shellKind {
	if filepath.Base(shell) == "nu" {
		return nu
	}
	return posix(keys)
}

// readPosix answers the PATH as exported, the allowlist resolved and the keys
// as the shell set them. It reports false for a PATH that is missing or
// resolves to nothing.
func readPosix(frames [][]byte, keys []string) (Result, bool) {
	a := answerOf(frames, keys)
	env, ok := resolveEnv(a.env, a.dir)
	if !ok {
		return Result{}, false
	}
	res := Result{Path: filepath.SplitList(a.env["PATH"]), Env: env, Keys: map[string]string{}}
	for _, key := range keys {
		if value, ok := a.env[key]; ok {
			res.Keys[key] = value
		}
	}
	return res, true
}

// readNu answers the PATH alone, since the allowlist and the keys are read
// from a posix shell's printenv. It reports false for an empty PATH.
func readNu(frames [][]byte, _ []string) (Result, bool) {
	path := string(frames[1])
	if path == "" {
		return Result{}, false
	}
	return Result{Path: filepath.SplitList(path), Env: map[string]string{}, Keys: map[string]string{}}, true
}

// maxOutputBytes caps each of the shell's two streams. Startup files are chatty,
// but not this chatty; past it we are reading something we do not understand.
const maxOutputBytes = 64 << 10

// The fixed fallback reasons. They are the whole vocabulary of the warning line —
// short and closed on purpose, because the log must never carry PATH, shell
// output, or any other environment value.
const (
	reasonNoShell     = "no shell"
	reasonShellExited = "shell exited"
	reasonBadOutput   = "bad output"
	reasonOutputLimit = "output limit"
	reasonTimeout     = "timeout"
	// A Start that failed: the TMPDIR, or the sandbox's policy.
	reasonNoScratch      = "no scratch"
	reasonSandboxRefused = "sandbox refused"
)

// Fault is a resolution failure, carrying exactly what the caller may log.
type Fault struct {
	Reason string
	// ExitCode is -1 when the shell never ran, or never exited on its own.
	ExitCode int
}

// Error is the reason alone, so the message is as safe to log as the Fault.
func (f *Fault) Error() string { return f.Reason }

func fault(reason string) *Fault { return &Fault{Reason: reason, ExitCode: -1} }

// answer is what the shell reported: the directory it ended in, and the raw value
// of every variable it had set.
type answer struct {
	dir string
	env map[string]string
}

// outcome is what one of the reader goroutines concluded. reason is empty on
// success, and is one of the fixed fallback reasons otherwise.
type outcome struct {
	out    []byte
	reason string
}

// Result is what one run of the login shell answered.
type Result struct {
	Path []string          // PATH as the shell exported it, split on ":", unfiltered
	Env  map[string]string // the imported allowlist, resolved; what the app sets on macOS
	// Keys is each key Resolve was asked for that the shell had set, by name,
	// as the shell gave it. They are secrets, and nothing sets them on the
	// process.
	Keys map[string]string
}

// Resolve runs the account's login shell once, through start, and reads its
// PATH, the allowlist and the keys named, variables holding secrets the caller
// wants. The deadline is ctx's. Resolve never touches the process's
// environment, so a failure leaves the inherited one exactly as it was.
func Resolve(ctx context.Context, start Start, keys []string) (Result, *Fault) {
	shell, f := findShellOrTimeout(ctx)
	if f != nil {
		return Result{}, f
	}
	kind := kindOf(shell, keys)
	out, f := Launch(ctx, start, shell, kind.args, scrubbedEnv(shell), maxOutputBytes, func(buf []byte, _ int) bool {
		_, ok := parse(buf, kind.frames)
		return ok
	})
	if f != nil {
		return Result{}, f
	}
	frames, _ := parse(out, kind.frames)
	res, ok := kind.read(frames, keys)
	if !ok {
		// The shell answered, but with a PATH that finds nothing.
		return Result{}, fault(reasonBadOutput)
	}
	return res, nil
}

// findShell is accountShell, or a test's stand-in.
var findShell = accountShell

// findShellOrTimeout is findShell, or a timeout when ctx ends first. Finding
// the shell stats the one the record names and reads the account, which can
// block on a dead network mount past any cancel, so it runs on a goroutine
// abandoned when ctx ends.
func findShellOrTimeout(ctx context.Context) (string, *Fault) {
	type found struct {
		shell string
		f     *Fault
	}
	answer := make(chan found, 1)
	find := findShell
	go func() {
		shell, f := find(ctx)
		answer <- found{shell, f}
	}()
	select {
	case a := <-answer:
		return a.shell, a.f
	case <-ctx.Done():
		return "", fault(reasonTimeout)
	}
}

// Path is Resolve's Path alone, bounded by DefaultTimeout, for a caller that
// has no use for the environment. Its error is a *Fault.
func Path(ctx context.Context, start Start) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	res, f := Resolve(ctx, start, nil)
	if f != nil {
		return nil, f
	}
	return res.Path, nil
}

// scrubbedEnv is the resolution's environment: the identity, locale and agent
// sockets copied from the process when set, and the rest fixed, PATH at the
// platform's login default, so no startup file sees the sidecar's variables and
// every launch resolves from the same start. The agent sockets are copied because
// a startup file that starts an agent when none is set would otherwise start one
// on every run, and the agent leaves the shell's session, so the kill misses it.
func scrubbedEnv(shell string) []string {
	env := []string{"SHELL=" + shell, "TERM=dumb", "DISABLE_AUTO_UPDATE=true", "PATH=" + DefaultPath}
	for _, name := range []string{
		"HOME", "USER", "LOGNAME", "TMPDIR", "LANG", "TZ",
		"SSH_AUTH_SOCK", "SSH_AGENT_PID", "GPG_AGENT_INFO", "XDG_RUNTIME_DIR",
	} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// InteractiveLogin is the arguments that run command in an interactive login
// shell, so the shell reads the startup files where a user sets PATH. The
// flags are separate because fish does not cluster them.
func InteractiveLogin(command string) []string {
	return []string{"-i", "-l", "-c", command}
}

// ProcessEnv is the process's environment with DISABLE_AUTO_UPDATE set, so
// oh-my-zsh does not stop to ask.
func ProcessEnv() []string {
	return append(os.Environ(), "DISABLE_AUTO_UPDATE=true")
}

// Start builds the command that runs the login shell, and a cleanup Launch
// calls once the shell has exited. The command's Env is never nil, and its
// SysProcAttr is unset: Launch sets it.
type Start func(ctx context.Context, name string, args, env []string) (cmd *exec.Cmd, cleanup func(), err error)

// Launch runs shell with args in env, through start, and returns its stdout as
// read up to the moment done says it is complete. done is handed the whole read
// so far and where the latest read began. Each of the shell's two streams is
// capped at limit. The deadline is ctx's, and the shell's session is killed and
// reaped before Launch returns, whatever happened.
func Launch(ctx context.Context, start Start, shell string, args, env []string, limit int, done func(buf []byte, from int) bool) ([]byte, *Fault) {
	cmd, cleanup, err := start(ctx, shell, args, env)
	if err != nil {
		return nil, startFault(ctx, err)
	}
	defer cleanup()
	// nil stdin is /dev/null: a startup file that reads it gets EOF, not a hang.
	cmd.Stdin = nil
	cmd.SysProcAttr = shellProcAttr()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fault(reasonNoShell)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fault(reasonNoShell)
	}
	if err := cmd.Start(); err != nil {
		return nil, fault(reasonNoShell)
	}

	// Buffered to each goroutine's send, so neither blocks after Launch returns.
	results := make(chan outcome, 2)
	go func() { results <- readMarked(stdout, limit, done) }()
	go func() {
		// Draining stderr at all is what keeps a chatty startup file from
		// filling the pipe and blocking the shell forever. The bytes are never
		// read back: shell output must not reach a log line.
		if overflowed(stderr, limit) {
			results <- outcome{reason: reasonOutputLimit}
		}
	}()

	var res outcome
	select {
	case res = <-results:
	case <-ctx.Done():
		res = outcome{reason: reasonTimeout}
	}
	if res.reason != "" {
		// An answer already in hand outranks a drain failure or a deadline
		// that raced it: it is complete either way.
		select {
		case other := <-results: // coverage:ignore only an answer landing between a drain failure or the deadline and this check reaches it
			if other.reason == "" {
				res = other
			}
		default:
		}
	}

	// Unconditional, so a shell still running — or a grandchild holding stdout
	// open — cannot outlive the call or extend it. A group that already exited
	// ignores this. Wait then closes our pipe ends, which unblocks the reader
	// goroutines even if a grandchild that left the group keeps the far ends open.
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	code := exitCode(cmd.Wait())

	if res.reason == "" {
		return res.out, nil
	}
	// The shell's status gates only when nothing usable was captured, and a
	// nonzero one names the failure better than the empty output it caused.
	if res.reason == reasonBadOutput && code > 0 {
		res.reason = reasonShellExited
	}
	return nil, &Fault{Reason: res.reason, ExitCode: code}
}

// startFault is the Fault of a Start that failed. Any failure once ctx has
// ended is a shell that ran out of time: a sandbox answers ctx's error when the
// deadline passes while it builds the run.
func startFault(ctx context.Context, err error) *Fault {
	switch {
	case ctx.Err() != nil:
		return fault(reasonTimeout)
	case errors.Is(err, errNoScratch):
		return fault(reasonNoScratch)
	}
	return fault(reasonSandboxRefused)
}

// shellProcAttr puts the shell in a session of its own. One kill then reaches
// whatever the startup files spawned, and — a session leader starting with none —
// the shell has no controlling terminal to contend for. Sharing ours is what
// hangs: an interactive shell in a background process group is stopped by SIGTTIN
// the first time it touches the tty, and the answer never comes. A GUI launch has
// no terminal to inherit, so only a run from one is affected.
func shellProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// Find is $SHELL when it names an executable file by absolute path. A
// relative one is never looked up: resolving it would search a PATH the shell
// is the source of. The bash tool picks its shell with it.
func Find() (string, bool) {
	shell := os.Getenv("SHELL")
	if filepath.IsAbs(shell) && executable(shell) {
		return shell, true
	}
	return "", false
}

func executable(file string) bool {
	info, err := os.Stat(file)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// readMarked reads r until done says it holds a complete answer, it exceeds
// limit, or it ends. Stopping at done is the point: waiting for EOF would hand a
// startup file that backgrounds a daemon the power to stall every launch.
func readMarked(r io.Reader, limit int, done func([]byte, int) bool) outcome {
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 4096)
	for {
		n, err := r.Read(chunk)
		if n > 0 {
			from := len(buf)
			buf = append(buf, chunk[:n]...)
			if done(buf, from) {
				return outcome{out: buf}
			}
			if len(buf) > limit {
				return outcome{reason: reasonOutputLimit}
			}
		}
		if err != nil {
			return outcome{reason: reasonBadOutput}
		}
	}
}

// parse returns the n frames the shell printed between the markers. It
// reports false while the last marker has not arrived yet, so a caller reading
// a stream can keep going; out is the bytes read so far.
func parse(out []byte, n int) ([][]byte, bool) {
	delimiter := []byte("\x00" + marker + "\x00")
	_, rest, ok := bytes.Cut(out, delimiter)
	if !ok {
		return nil, false
	}
	frames := make([][]byte, 0, n)
	for range n {
		var frame []byte
		frame, rest, ok = bytes.Cut(rest, delimiter)
		if !ok {
			return nil, false
		}
		// The utility's own trailing newline, and only that one: a value may
		// hold any byte but NUL, newlines included, so nothing further is
		// trimmed and nothing else is judged.
		frame = bytes.TrimSuffix(frame, []byte("\n"))
		// A NUL cannot come from either utility, so one here means the markers
		// framed something other than the payload we asked for.
		if bytes.IndexByte(frame, 0) >= 0 {
			return nil, false
		}
		frames = append(frames, frame)
	}
	return frames, true
}

// answerOf reads a posix run's frames: the cwd first, then the raw value of
// each imported variable and each key it had set, keyed by name.
func answerOf(frames [][]byte, keys []string) answer {
	a := answer{dir: string(frames[0]), env: make(map[string]string, len(frames)-1)}
	names := make([]string, 0, len(frames)-1)
	for _, v := range imported {
		names = append(names, v.name)
	}
	names = append(names, keys...)
	for i, name := range names {
		// printenv prints nothing for an unset variable and `|| true` swallows
		// its status, so an empty frame is the shell saying it has no value.
		if frame := frames[i+1]; len(frame) > 0 {
			a.env[name] = string(frame)
		}
	}
	return a
}

// resolveEnv resolves each raw value by its kind and drops the ones left empty.
// It reports false for a PATH that is missing or resolves to nothing: installing
// that would leave the process unable to find any command at all, which is worse
// than the PATH launchd handed us.
func resolveEnv(raw map[string]string, dir string) (map[string]string, bool) {
	// A cwd the shell could not report is no base at all, and a relative entry
	// then passes through as the shell gave it.
	if !filepath.IsAbs(dir) {
		dir = ""
	}
	env := make(map[string]string, len(raw))
	for _, v := range imported {
		value, ok := raw[v.name]
		if !ok {
			continue
		}
		switch v.kind {
		case path:
			value = resolvePath(value, dir)
		case pathList:
			value = resolveList(value, dir)
		}
		if value == "" {
			continue
		}
		env[v.name] = value
	}
	if env["PATH"] == "" {
		return nil, false
	}
	return env, true
}

// resolvePath makes one path the shell reported meaningful outside it: the shell
// ran in dir, so a relative entry means the same thing joined to dir. It returns
// "" for a dropped entry. A leading `~` is answered first, because IsAbs says it
// is relative and joining would invent a path the shell never produced.
func resolvePath(value, dir string) string {
	switch {
	case value == "":
		return ""
	case strings.HasPrefix(value, "~"):
		return value
	case filepath.IsAbs(value), dir == "":
		return value
	default:
		// Prefixed rather than joined: filepath.Join cleans, and cleaning
		// `link/..` away lexically names a different file than the kernel would
		// resolve whenever link is a symlink.
		return strings.TrimSuffix(dir, "/") + "/" + value
	}
}

// resolveList resolves each `:`-separated entry and rejoins the survivors.
// Splitting is the list's business alone: a single path may legally contain a
// colon, so only the kind knows whether one is a separator.
func resolveList(value, dir string) string {
	var out []string
	for _, entry := range filepath.SplitList(value) {
		if resolved := resolvePath(entry, dir); resolved != "" {
			out = append(out, resolved)
		}
	}
	return strings.Join(out, string(filepath.ListSeparator))
}

// overflowed discards r, reporting whether it ran past limit.
func overflowed(r io.Reader, limit int) bool {
	n, _ := io.Copy(io.Discard, io.LimitReader(r, int64(limit)+1))
	return n > int64(limit)
}

// exitCode returns the shell's status, or -1 when it never exited on its own —
// including the kill above, which is why a success never consults it.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode()
	}
	return -1
}
