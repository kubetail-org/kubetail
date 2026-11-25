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
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/drain"
)

const (
	// snapshotTimeout bounds taking the snapshot: the login shell's rc files, the
	// dump, the kill and the reap.
	snapshotTimeout = 10 * time.Second
	// snapshotLimit caps what the dump may print. A profile that loads oh-my-zsh
	// or nvm prints hundreds of KB of functions.
	snapshotLimit = 4 << 20
)

// The markers frame the dump in the shell's stdout, so what an rc file prints
// around it is dropped. NUL cannot appear in a function, an alias or PATH.
const (
	snapshotStart = "\x00kubetail_snapshot_start\x00"
	snapshotEnd   = "\x00kubetail_snapshot_end\x00"
)

// dumpCommand is what the snapshot's login shell runs: the profile it has
// built, printed as a script that rebuilds it. It runs after the rc files, so
// their aliases and functions would apply to its own text. The first line turns
// alias expansion off, spelled so no alias can match it (a backslash on the
// command, every other word quoted); the dump is parsed after it through eval,
// and every command in it goes through builtin, which skips a function of the
// same name.
func dumpCommand(kind string) string {
	if kind == "zsh" {
		return `\builtin 'unsetopt' 'aliases'; \builtin 'eval' ` + quote(dumpBody(kind))
	}
	return `\builtin 'shopt' '-u' 'expand_aliases'; \builtin 'eval' ` + quote(dumpBody(kind))
}

// dumpBody prints the profile between the markers. The options come before the
// functions, since a function body is parsed under the options in force (bash
// reads +(...) only with extglob on), and a snapshot that fails to parse stops
// sourcing there, taking the rest of it and the shims with it. The aliases come
// after the functions, so a function body is not expanded twice. PATH is
// resolved as loginshell resolves it: a command runs in its own directory, not
// in the one the profile may have cd'd to, so a relative entry is prefixed
// with the dump's own cwd; a leading ~ passes through, an empty entry is
// dropped, and a PATH left empty is not exported at all.
func dumpBody(kind string) string {
	lines := []string{
		`builtin printf '\000kubetail_snapshot_start\000'`,
		`kubetail_p= kubetail_s= kubetail_r=$PATH:`,
		`while [[ -n $kubetail_r ]]; do kubetail_e=${kubetail_r%%:*} kubetail_r=${kubetail_r#*:}; case $kubetail_e in ('') ;; ('~'*|/*) kubetail_p+=$kubetail_s$kubetail_e kubetail_s=: ;; (*) case $PWD in (/*) kubetail_p+=$kubetail_s${PWD%/}/$kubetail_e ;; (*) kubetail_p+=$kubetail_s$kubetail_e ;; esac; kubetail_s=: ;; esac; done`,
	}
	if kind == "zsh" {
		lines = append(lines,
			`builtin printf '%s\n' 'unalias -a'`,
			// The options that say how this shell was started are its own, and
			// noaliases is the dump's. monitor above all: replayed, it moves a
			// command's background jobs out of its process group. Under
			// kshoptionprint setopt lists every option beside on or off.
			`builtin unsetopt kshoptionprint`,
			`for o in ${(f)"$(builtin setopt)"}; do case $o in (interactive|login|monitor|zle|shinstdin|privileged|restricted|singlecommand|noaliases) ;; (*) builtin printf 'setopt %s\n' $o ;; esac; done`,
			`builtin typeset -f`,
			// Regular aliases alone: a global or suffix alias expands anywhere in
			// a line, not only where a command is.
			`builtin alias -rL`,
			`if [[ -n $kubetail_p ]]; then builtin printf 'export PATH=%s\n' ${(q)kubetail_p}; fi`,
		)
	} else {
		lines = append(lines,
			`builtin printf '%s\n' 'unalias -a'`,
			// login_shell and restricted_shell are read-only.
			`builtin shopt -p | while IFS= builtin read -r l; do case $l in (*' login_shell'|*' restricted_shell') ;; (*) builtin printf '%s\n' "$l" ;; esac; done`,
			`builtin printf '%s\n' 'shopt -s expand_aliases'`,
			`builtin declare -f`,
			`builtin alias -p`,
			`if [[ -n $kubetail_p ]]; then builtin printf 'export PATH=%q\n' "$kubetail_p"; fi`,
		)
	}
	lines = append(lines, `builtin printf '\000kubetail_snapshot_end\000'`)
	return strings.Join(lines, "\n")
}

// snapshotDone reports whether the dump's end marker has arrived. It looks only
// at the bytes from where the latest read began, less a marker's length, so a
// long read stays linear.
func snapshotDone(buf []byte, from int) bool {
	return bytes.Contains(buf[max(from-len(snapshotEnd)+1, 0):], []byte(snapshotEnd))
}

// snapshotText is the dump between the markers.
func snapshotText(out []byte) (string, bool) {
	_, rest, ok := bytes.Cut(out, []byte(snapshotStart))
	if !ok {
		return "", false
	}
	text, _, ok := bytes.Cut(rest, []byte(snapshotEnd))
	return string(text), ok
}

// StartSnapshot is the Start of the shell snapshot lifecycle part. It makes
// the context the snapshot is taken under, since the ctx it is handed bounds
// startup only, and takes the snapshot now only on a machine with no sandbox;
// with one, the first run outside it starts it (snapshotFor). The stop it
// returns is final: it ends a shell already started and waits, under the
// drain context, for its reap, so no login shell outlives the sidecar, and no
// shell starts once it has returned.
func (t *Tool) StartSnapshot(context.Context) (func(context.Context) error, error) {
	ctx, cancel := context.WithCancel(context.Background())
	t.snapMu.Lock()
	t.snapCtx = ctx
	t.snapMu.Unlock()
	if t.sandboxer == nil {
		t.startSnapshot()
	}
	return func(drainCtx context.Context) error {
		t.snapMu.Lock()
		cancel()
		ready := t.ready
		t.snapMu.Unlock()
		if ready == nil {
			return nil
		}
		return drain.WithContext(drainCtx, func() { <-ready })
	}, nil
}

// startSnapshot takes the snapshot in the background, once, under the
// context StartSnapshot made. Before StartSnapshot or after its stop it
// starts nothing, and a call goes on with no snapshot.
func (t *Tool) startSnapshot() {
	t.snapMu.Lock()
	defer t.snapMu.Unlock()
	if t.ready != nil || t.snapCtx == nil || t.snapCtx.Err() != nil {
		return
	}
	ctx, ready := t.snapCtx, make(chan struct{})
	t.ready = ready
	go func() {
		defer close(ready)
		t.takeSnapshot(ctx)
	}()
}

// awaitSnapshot is the snapshot for a call to source, once it is written or
// given up: "" when there is none, or when ctx ends first. A tool whose snapshot
// was never started does not wait.
func (t *Tool) awaitSnapshot(ctx context.Context) string {
	if t.ready == nil {
		return t.snapshot
	}
	select {
	case <-t.ready:
		return t.snapshot
	default:
	}
	if t.onSnapshotWait != nil {
		t.onSnapshotWait()
	}
	select {
	case <-t.ready:
		return t.snapshot
	case <-ctx.Done():
		return ""
	}
}

// snapshotFor is the snapshot a run sources: none for a sandboxed run, which
// neither sources nor waits for it; for a run outside the sandbox, started if
// this is the first such run, then awaited, and refused while the directory
// the shell's own sits in is not this user's alone (checkPrivate), since every
// such command sources the file by its path.
func (t *Tool) snapshotFor(ctx context.Context, sandboxed bool) (string, error) {
	if sandboxed {
		return "", nil
	}
	t.startSnapshot()
	snapshot := t.awaitSnapshot(ctx)
	if snapshot == "" {
		return "", nil
	}
	return snapshot, checkPrivate(filepath.Dir(t.scripts))
}

// takeSnapshot runs the login shell once and writes what it built, then the
// shims, for every command to source. A dump that cannot be taken is logged and
// costs only itself: the file holds the shims alone, and where there are none,
// there is no file.
func (t *Tool) takeSnapshot(parent context.Context) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, t.snapTimeout)
	defer cancel()
	out, reason, code := t.launch(ctx)
	if parent.Err() != nil {
		return // stopping: nothing will read the file
	}
	dump := ""
	if reason == "" {
		var ok bool
		if dump, ok = snapshotText(out); !ok {
			reason = "bad output"
		}
	}
	if reason != "" {
		// The reason is from a fixed set; the shell's output never reaches the log.
		slog.Warn("shell snapshot not taken", "reason", reason, "exit_code", code, "elapsed", time.Since(started))
	}
	if dump+shims == "" {
		return
	}
	path, err := writeSnapshot(t.scripts, dump+shims)
	if err != nil {
		slog.Warn("shell snapshot not written", "err", err)
		return
	}
	t.snapshot = path
}

// writeSnapshot writes text as the snapshot under dir and returns its path,
// with forward slashes, which Git Bash reads without conversion. The file is
// new, since New swept dir, and read-only — on Windows its read-only attribute —
// so nothing appends to what every command sources.
func writeSnapshot(dir, text string) (string, error) {
	path, err := filepath.Abs(filepath.Join(dir, "snapshot.sh"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(text)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return filepath.ToSlash(path), err
}
