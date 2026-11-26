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
	"log/slog"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
)

// shims follow the user's definitions in the snapshot, so they win. They are a
// courtesy, not a boundary: /bin/kill passes them. The unalias comes first
// because a function's name goes through alias expansion, so under
// alias kill='kill -9' the definition would not parse. kill is the builtin
// underneath, which keeps a job spec (%%) working. pkill asks pgrep first with
// the same arguments less the ones pgrep does not take — a signal, and procps's
// -e, -H and -q — which only widens what it matches. A signal name is matched
// with [[:upper:]]: macOS's bash 3.2 matches a range like [A-Z] by the locale's
// collation, which takes -ef for one. A pgrep that cannot answer
// is a refusal, since an unreadable answer is not permission; a machine without
// pgrep is left to pkill.
const shims = `
\builtin unalias kill pkill pgrep 2>/dev/null
__kubetail_refuse() {
	\builtin printf '%s\n' "refused: ${1:-that would stop Kubetail}" >&2
	return 1
}
__kubetail_protected() {
	for __kubetail_p in "$KUBETAIL_SIDECAR_PID" "$KUBETAIL_DESKTOP_PID"; do
		\builtin test -n "$__kubetail_p" || continue
		\builtin test "$1" = "$__kubetail_p" && return 0
		\builtin test "$1" = "-$__kubetail_p" && return 0
	done
	return 1
}
kill() {
	for __kubetail_a in "$@"; do
		__kubetail_protected "$__kubetail_a" && { __kubetail_refuse; return 1; }
	done
	\builtin kill "$@"
}
pkill() {
	\builtin command -v pgrep >/dev/null 2>&1 || { \builtin command pkill "$@"; return; }
	__kubetail_args=()
	__kubetail_first=1
	__kubetail_skip=0
	for __kubetail_a in "$@"; do
		if \builtin test "$__kubetail_skip" = 1; then
			__kubetail_skip=0
		else
			case "$__kubetail_first:$__kubetail_a" in
			1:-[0-9]* | 1:-[[:upper:]][[:upper:]]*) ;;
			*:--signal | *:-q | *:--queue) __kubetail_skip=1 ;;
			*:--signal=* | *:--queue=* | *:-e | *:--echo | *:-H | *:--require-handler) ;;
			*) __kubetail_args+=("$__kubetail_a") ;;
			esac
		fi
		__kubetail_first=0
	done
	__kubetail_pids=$(\builtin command pgrep "${__kubetail_args[@]}" 2>/dev/null)
	\builtin test $? -gt 1 && { __kubetail_refuse 'pgrep could not check what that would stop'; return 1; }
	__kubetail_nl='
'
	for __kubetail_p in "$KUBETAIL_SIDECAR_PID" "$KUBETAIL_DESKTOP_PID"; do
		\builtin test -n "$__kubetail_p" || continue
		case "$__kubetail_nl$__kubetail_pids$__kubetail_nl" in
		*"$__kubetail_nl$__kubetail_p$__kubetail_nl"*) __kubetail_refuse; return 1 ;;
		esac
	done
	\builtin command pkill "$@"
}
`

// launchDump runs the dump in the login shell, in the process's environment,
// since a command outside the sandbox sources what it finds. On a machine with
// a sandbox the shell runs in it, reading the whole home but Kubetail's
// directories, since only commands that read the home themselves source what
// it builds, and writing a TMPDIR of its own under the tool's. reason is ""
// when the dump arrived whole.
func (t *Tool) launchDump(ctx context.Context) (out []byte, reason string, code int) {
	var sb loginshell.Commander
	if t.sandboxer != nil {
		sb = t.sandboxer
	} else {
		slog.Warn("shell snapshot taken unconfined: this machine has no sandbox")
	}
	start := loginshell.In(sb, nil, t.denied, TempDir(t.tmpDir))
	out, f := loginshell.Launch(ctx, start, t.shell, loginshell.InteractiveLogin(dumpCommand(t.kind)), loginshell.ProcessEnv(), t.snapLimit, snapshotDone)
	if f != nil {
		return nil, f.Reason, f.ExitCode
	}
	return out, "", 0
}
