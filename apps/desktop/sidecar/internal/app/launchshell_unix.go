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

package app

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"slices"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/agent/catalog"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/loginshell"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/bash"
)

// resolveShell is loginshell.Resolve, which a test replaces.
var resolveShell = loginshell.Resolve

// launchShell runs the account's login shell once, in sb: its environment is
// set where the platform needs it, its PATH is what the sandbox's is resolved
// from, and the provider keys are read from it, since a GUI launch inherits
// none a startup file exports. Its output leaves the sandbox, so it reads
// nothing on the denied-always list, and nothing of Kubetail's directories
// (kubetailDirs); its TMPDIR is a run's, under tmpDir. sb is nil for no sandbox,
// where the shell runs unconfined, or not at all where nothing reads its
// answer (skipResolution).
func launchShell(ctx context.Context, sb sandboxer, kubetailDirs []string, tmpDir string) launch {
	if skipResolution(sb) {
		return launch{}
	}
	var deny []string
	if sb == nil {
		slog.Warn("login shell runs unconfined: this machine has no sandbox")
	} else {
		home, _ := os.UserHomeDir()
		deny = sb.Never(home)
	}
	keyVars := slices.Sorted(maps.Values(catalog.KeyVars()))
	return runShell(ctx, loginshell.In(sb, deny, kubetailDirs, bash.TempDir(tmpDir)), keyVars, resolveShell)
}

// runShell calls resolve once, through start, under the shell's timeout, and
// hands its answer to its readers. It logs how long the shell took, so the
// timeout can be judged against real startup files. A failure is not a startup
// error: it logs the reason, never a value, and answers nothing.
func runShell(ctx context.Context, start loginshell.Start, keyVars []string, resolve func(context.Context, loginshell.Start, []string) (loginshell.Result, *loginshell.Fault)) launch {
	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, loginshell.DefaultTimeout)
	defer cancel()

	res, f := resolve(ctx, start, keyVars)
	if f != nil {
		slog.Warn("login shell not read",
			"reason", f.Reason,
			"exit_code", f.ExitCode,
			"elapsed", time.Since(started),
		)
		return launch{fault: f.Reason}
	}
	// Before the next log line, so none can carry a key.
	for _, key := range res.Keys {
		safe.AddSecret(key)
	}
	slog.Info("login shell read", "elapsed", time.Since(started))
	setShellEnv(res.Env)
	return launch{path: res.Path, keys: res.Keys}
}
