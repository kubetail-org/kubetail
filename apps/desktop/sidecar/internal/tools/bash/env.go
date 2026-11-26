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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools/internal/fileguard"
)

// outsideEnv is a run's environment outside the sandbox: the process's whole,
// then kubetail, the user's approved variables followed by what Kubetail adds, then
// PWD. exec sets PWD from Dir only when Env is nil; without it the shell keeps
// the sidecar's, finds it wrong and reads the physical path, so pwd would print
// something other than the directory the approval request showed.
func outsideEnv(environ, kubetail []string, dir string) []string {
	env := make([]string, 0, len(environ)+len(kubetail)+1)
	env = append(env, environ...)
	env = append(env, kubetail...)
	return append(env, "PWD="+dir)
}

// lastOfEachName is env with only the last entry of each name kept, in order,
// as exec.Cmd keeps it on Unix. Windows hands a child the whole block and the
// child reads the first, so there the block is built through this. key is an
// entry's name as the platform compares it.
func lastOfEachName(env []string, key func(entry string) string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(env))
	for _, e := range slices.Backward(env) {
		if k := key(e); !seen[k] {
			seen[k] = true
			out = append(out, e)
		}
	}
	slices.Reverse(out)
	return out
}

// sandboxedRunEnv is a sandboxed run's environment, built from one table so
// nothing of the sidecar's rides in but LANG and TZ: PATH the run's own, the
// folders it froze at its start; HOME the workspace and PWD the start
// directory; TMPDIR the run's own, in the cache; ZDOTDIR the run's directory,
// made for the run and so holding no startup file, so zsh -c never sources a
// .zshenv a command left in the workspace; KUBECONFIG and KUBECACHEDIR for a
// run with a cluster (cluster not nil); LANG the sidecar's or the platform's
// default, and its TZ; then user, the user's variables, which may repeat LANG
// or TZ to replace it but name nothing else of the table (envvars.Check);
// TERM=dumb; then the variables that point each tool into toolHome; then
// toolchain, each found location's variables and asdf's versions; then cloud,
// each mounted cloud platform's variables (cloudEnv); then what Kubetail adds.
func sandboxedRunEnv(environ, kubetail []string, path, workspace, dir string, rd *runDir, cluster *target, toolHome string, user, toolchain, cloud []string) []string {
	var lang, tz string
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		switch name {
		case "LANG":
			lang = value
		case "TZ":
			tz = value
		}
	}
	env := []string{"PATH=" + path}
	env = append(env, "HOME="+workspace, "PWD="+dir, "TMPDIR="+rd.tmp, "ZDOTDIR="+rd.path)
	if cluster != nil {
		env = append(env, "KUBECONFIG="+rd.kubeconfig(), "KUBECACHEDIR="+cluster.cacheDir)
	}
	if lang == "" {
		lang = defaultLang(runtime.GOOS, fileExists)
	}
	env = append(env, "LANG="+lang)
	if tz != "" {
		env = append(env, "TZ="+tz)
	}
	env = append(env, user...)
	env = append(env, "TERM=dumb")
	env = append(env, toolHomeEnv(toolHome)...)
	env = append(env, toolchain...)
	env = append(env, cloud...)
	return append(env, kubetail...)
}

// defaultLang is LANG for a run whose sidecar has none: en_US.UTF-8 on
// macOS, which always has it; on Linux C.UTF-8 where the system has that
// locale, else C.
func defaultLang(goos string, exists func(string) bool) string {
	switch {
	case goos == "darwin":
		return "en_US.UTF-8"
	case exists("/usr/lib/locale/C.utf8") || exists("/usr/lib/locale/C.UTF-8"):
		return "C.UTF-8"
	}
	return "C"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// toolVersionsLimit is the most of ~/.tool-versions read.
const toolVersionsLimit = 64 << 10

// toolVersions is asdf's global versions, read from the user's
// ~/.tool-versions as parseToolVersions reads them. asdf reads them from
// $HOME, which in the sandbox is the workspace, so they ride as variables.
// Anything but a plain file holds none.
func toolVersions(home string) []string {
	f, err := fileguard.Open(filepath.Join(home, ".tool-versions"))
	if err != nil {
		return nil
	}
	defer f.Close()
	text, err := io.ReadAll(io.LimitReader(f, toolVersionsLimit))
	if err != nil { // coverage:ignore a regular file just opened fails to read only on an I/O error
		return nil
	}
	return parseToolVersions(string(text))
}

var (
	asdfTool    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	asdfVersion = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

// parseToolVersions is ASDF_<TOOL>_VERSION for each line of text whose tool
// and first version match asdf's plain shapes, the tool upper-cased with -
// spelled _, as asdf spells it. Any other line is skipped.
func parseToolVersions(text string) []string {
	var env []string
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || !asdfTool.MatchString(f[0]) || !asdfVersion.MatchString(f[1]) {
			continue
		}
		name := strings.ToUpper(strings.ReplaceAll(f[0], "-", "_"))
		env = append(env, "ASDF_"+name+"_VERSION="+f[1])
	}
	return env
}
