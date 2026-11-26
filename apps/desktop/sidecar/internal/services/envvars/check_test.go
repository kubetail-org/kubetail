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

package envvars

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckPassesAVariableItsTargetCanHold(t *testing.T) {
	for _, target := range []Target{Sandbox, Outside, Both} {
		assert.NoError(t, Check("KUBECTL_EXTERNAL_DIFF", "colordiff -u", target))
		assert.NoError(t, Check("_x1", "", target))
		assert.NoError(t, Check("LANG", "de_DE.UTF-8", target), "LANG is the sidecar's, which the user may replace")
		assert.NoError(t, Check("NOTE", "two\nlines", target))
		assert.NoError(t, Check("BIG", strings.Repeat("a", maxValue), target))
	}
}

func TestCheckRefusesForEveryCommand(t *testing.T) {
	for name, c := range map[string]struct{ name, value, reason string }{
		"an empty name":            {"", "x", "A name is"},
		"a name starting with a 1": {"1X", "x", "A name is"},
		"a name with a dash":       {"MY-VAR", "x", "A name is"},
		"a name with an =":         {"A=B", "x", "A name is"},
		"a name past 128 bytes":    {strings.Repeat("A", 129), "x", "A name is"},
		"a loader variable":        {"LD_PRELOAD", "x", "LD_PRELOAD changes how a program starts"},
		"a loader prefix on macOS": {"DYLD_FALLBACK_LIBRARY_PATH", "x", "changes how a program starts"},
		"a glibc unsecure name":    {"GCONV_PATH", "x", "GCONV_PATH changes how a program starts"},
		"a Go runtime variable":    {"GOMEMLIMIT", "1", "GOMEMLIMIT changes how a program starts"},
		"a macOS malloc variable":  {"MallocStackLogging", "1", "changes how a program starts"},
		"a glibc malloc variable":  {"MALLOC_PERTURB_", "165", "changes how a program starts"},
		"the search path, in case": {"Path", "/tmp", "Kubetail sets Path itself."},
		"a loader name, in case":   {"ld_preload", "x", "ld_preload changes how a program starts"},
		"a Go name, in case":       {"godebug", "x", "godebug changes how a program starts"},
		"a shell startup file":     {"BASH_ENV", "/tmp/x", "BASH_ENV can make the shell run code"},
		"a shell option":           {"SHELLOPTS", "xtrace", "SHELLOPTS can make the shell run code"},
		"the search path":          {"PATH", "/tmp", "Kubetail sets PATH itself."},
		"Kubetail's marker":        {"KUBETAIL", "1", "Kubetail sets KUBETAIL itself."},
		"the sidecar's pid":        {"KUBETAIL_SIDECAR_PID", "1", "Kubetail sets KUBETAIL_SIDECAR_PID itself."},
		"the desktop's pid":        {"KUBETAIL_DESKTOP_PID", "1", "Kubetail sets KUBETAIL_DESKTOP_PID itself."},
		"a value with a NUL":       {"X", "a\x00b", "no NUL"},
		"a value past the bound":   {"X", strings.Repeat("a", maxValue+1), "at most 4096 bytes"},
	} {
		for _, target := range []Target{Sandbox, Outside, Both} {
			assert.ErrorContains(t, Check(c.name, c.value, target), c.reason, "%s, %s", name, target)
		}
	}
	var refusal Refusal
	assert.ErrorAs(t, Check("X", "x", "chat"), &refusal, "a target outside the list")
}

// Only the three variables a run sets are Kubetail's: the CLI's own, which
// share their prefix, are the user's to set.
func TestCheckPassesAKubetailVariableARunDoesNotSet(t *testing.T) {
	for _, target := range []Target{Sandbox, Outside, Both} {
		assert.NoError(t, Check("KUBETAIL_NAMESPACE", "prod", target), target)
	}
}

// What the sandbox never passes or sets itself reaches commands outside it,
// and those alone.
func TestCheckRefusesForASandboxedCommandAlone(t *testing.T) {
	for name, reason := range map[string]string{
		"AWS_PROFILE":      "AWS_PROFILE is never passed to a sandboxed command.",
		"SSH_AUTH_SOCK":    "never passed",
		"HOME":             "Kubetail sets HOME itself in a sandboxed command.",
		"KUBECONFIG":       "Kubetail sets KUBECONFIG itself",
		"https_proxy":      "Kubetail sets https_proxy itself",
		"REQUEST_METHOD":   "turns off the proxy",
		"Npm_Config_Cache": "Kubetail sets Npm_Config_Cache itself",
		"aws_profile":      "aws_profile is never passed",
	} {
		assert.NoError(t, Check(name, "x", Outside), name)
		for _, target := range []Target{Sandbox, Both} {
			err := Check(name, "x", target)
			var refusal Refusal
			if assert.ErrorAs(t, err, &refusal, name) {
				assert.Contains(t, refusal.Error(), reason, name)
				assert.Contains(t, refusal.Error(), "outside the sandbox alone", name)
			}
		}
	}
}
