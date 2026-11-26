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

// Package envvars keeps the variables the user gives their commands, as rows
// in app.db, and the check every one of them passes.
package envvars

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// Target is the commands a variable reaches.
type Target string

const (
	Sandbox Target = "sandbox" // commands in the sandbox
	Outside Target = "outside" // commands outside it
	Both    Target = "both"
)

// The bounds the refusals below spell out.
const (
	maxName  = 128  // bytes in a name
	maxValue = 4096 // bytes in a value
)

// envName is a shell name.
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// launcherEnv is what changes how a program starts, beside the LD_, DYLD_ and
// MALLOC prefixes (glibc's MALLOC_ and macOS's Malloc): glibc's other unsecure
// variables and the Go runtime's. The
// sandbox's launchers start outside it with the run's environment, and a
// command outside it has the user's full access.
var launcherEnv = []string{
	"GCONV_PATH", "GETCONF_DIR", "GLIBC_TUNABLES", "HOSTALIASES", "LOCALDOMAIN", "LOCPATH",
	"NIS_PATH", "NLSPATH", "RESOLV_HOST_CONF", "RES_OPTIONS", "TZDIR",
	"GODEBUG", "GOGC", "GOMAXPROCS", "GOMEMLIMIT", "GOTRACEBACK",
}

func reachesLauncher(key string) bool {
	for _, prefix := range []string{"LD_", "DYLD_", "MALLOC"} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return slices.Contains(launcherEnv, key)
}

// shellEnv is what makes a shell run code on its own: xtrace expands PS4
// before each command.
var shellEnv = []string{"BASH_ENV", "ENV", "PROMPT_COMMAND", "SHELLOPTS", "BASHOPTS", "PS4"}

// kubetailSets reports whether Kubetail sets name on every command: the
// variables a run adds, and outside the sandbox the profile snapshot's PATH.
// The CLI's KUBETAIL_* variables are the user's.
func kubetailSets(key string) bool {
	return slices.Contains([]string{"PATH", "PWD", "KUBETAIL", "KUBETAIL_SIDECAR_PID", "KUBETAIL_DESKTOP_PID"}, key)
}

// Check is why the variable name holding value cannot reach target's
// commands, a Refusal, or nil. Every list is matched by key, the name in upper
// case: Windows reads names without case, so Path is PATH there.
func Check(name, value string, target Target) error {
	key := strings.ToUpper(name)
	switch {
	case len(name) > maxName || !envName.MatchString(name):
		return Refusal(fmt.Sprintf("A name is 1 to %d letters, digits or _, and does not start with a digit.", maxName))
	case reachesLauncher(key):
		return Refusal(name + " changes how a program starts.")
	case slices.Contains(shellEnv, key):
		return Refusal(name + " can make the shell run code on its own.")
	case kubetailSets(key):
		return Refusal("Kubetail sets " + name + " itself.")
	case strings.ContainsRune(value, 0):
		return Refusal("A value holds no NUL character.")
	case len(value) > maxValue:
		return Refusal(fmt.Sprintf("A value is at most %d bytes.", maxValue))
	}
	switch target {
	case Outside:
		return nil
	case Sandbox, Both:
		return checkSandboxed(name, key)
	}
	return Refusal("A variable reaches commands in the sandbox, outside it, or both.")
}

// checkSandboxed is why no sandboxed command can be given name, whose upper
// case is key.
func checkSandboxed(name, key string) error {
	const outside = " It can reach commands outside the sandbox alone."
	switch {
	case sandbox.Unpassable(key):
		return Refusal(name + " is never passed to a sandboxed command." + outside)
	case sandbox.KubetailSets(key):
		return Refusal("Kubetail sets " + name + " itself in a sandboxed command." + outside)
	case key == "REQUEST_METHOD":
		// Go's proxy lookup ignores HTTP_PROXY when it is set.
		return Refusal("REQUEST_METHOD turns off the proxy Kubetail sets in a sandboxed command." + outside)
	}
	return nil
}

// A Refusal is a variable the user cannot have, in their words.
type Refusal string

func (r Refusal) Error() string { return string(r) }
