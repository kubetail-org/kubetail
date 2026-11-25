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

package app

import (
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
)

// setShellEnv sets the part of the user's login-shell environment we carry
// on the process. A GUI launch inherits launchd's minimal one, so without this a
// kubeconfig `exec` credential plugin sitting on the shell PATH is not found,
// and a KUBECONFIG or an AWS_PROFILE the user exports is invisible — the same
// kubeconfig that works from a terminal fails here. The log names the
// variables, never a value.
func setShellEnv(env map[string]string) {
	names := slices.Sorted(maps.Keys(env))
	for _, name := range names {
		// Setenv fails only on a malformed name, which the compile-time list
		// rules out — so there is no half-set environment to unwind.
		_ = os.Setenv(name, env[name])
	}
	slog.Info("shell environment imported",
		// Joined here: the log renderer reduces a value it cannot read to its
		// type, so a []string would reach the file as "<unrendered []string>".
		"variables", strings.Join(names, ","),
	)
}

// skipResolution is false: the environment import needs the login shell
// whether or not there is a sandbox to run it in.
func skipResolution(sandboxer) bool { return false }
