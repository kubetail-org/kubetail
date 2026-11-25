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

package sandbox

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// cpuSeconds is the CPU time ps reports for pid, as [[dd-]hh:]mm:ss.cc
// reads, or -1 when it cannot.
func cpuSeconds(pid string) float64 {
	out, err := exec.Command("ps", "-o", "time=", "-p", pid).Output()
	if err != nil {
		return -1
	}
	total := 0.0
	for part := range strings.SplitSeq(strings.TrimSpace(string(out)), ":") {
		n, err := strconv.ParseFloat(part, 64)
		if err != nil {
			return -1
		}
		total = total*60 + n
	}
	return total
}

// XNU sends SIGXCPU once, at the soft limit, and enforces no hard one, so a
// process that ignores the signal runs on. This pins that residual: the day
// macOS kills it at the hard limit, the test fails and the residual comes off.
func TestAnIgnoredSIGXCPUOutlivesTheLimit(t *testing.T) {
	s := confining(t)
	m := standIn(t)
	r := m.on(s)
	r.Policy.Limits = Limits{CPUSeconds: 1}
	r.Args = []string{"-c", `trap '' XCPU; echo $$; while :; do :; done`}
	cmd := command(t, s, t.Context(), r)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	require.NoError(t, err)
	pid := strings.TrimSpace(line)

	require.Eventually(t, func() bool { return cpuSeconds(pid) >= cpuGrace+2 }, 3*testutil.Timeout, 100*time.Millisecond,
		"the spin stopped before seven seconds of CPU")
}

// deniedAt is where p's compiled Deny rules lie.
func deniedAt(t *testing.T, p Policy) []string {
	t.Helper()
	var at []string
	for _, r := range compiledRules(t, p) {
		if r.kind == ruleDeny {
			at = append(at, r.at)
		}
	}
	return at
}

// A Read granted in the disk's spelling reaches a denied-always path whose
// link spells it in another case, so its Deny is compiled.
func TestADenyLinkedInAnotherCaseIsCompiled(t *testing.T) {
	folder, link := linkInAnotherCase(t)
	p := Policy{
		Files:  FilePolicy{Read: []string{filepath.Dir(folder)}},
		Always: AlwaysPolicy{Deny: []string{link}},
	}

	assert.Equal(t, []string{folder}, deniedAt(t, p))
}

// A Kubetail root linked in another case, as a dotfiles-managed config folder
// can be, is denied under a Read granted in the disk's spelling.
func TestAKubetailRootLinkedInAnotherCaseIsDenied(t *testing.T) {
	folder, link := linkInAnotherCase(t)
	p := Policy{
		Files:  FilePolicy{Read: []string{filepath.Dir(folder)}},
		Always: AlwaysPolicy{Kubetail: []string{link}},
	}

	assert.Equal(t, []string{folder}, deniedAt(t, p))
}

// Check compares in the disk's spelling: a Read on a Kubetail root that a link
// spells in another case lies on a path no rule opens.
func TestCheckRefusesAReadOnAKubetailRootLinkedInAnotherCase(t *testing.T) {
	folder, link := linkInAnotherCase(t)
	p := Policy{
		Files:  FilePolicy{Read: []string{folder}},
		Always: AlwaysPolicy{Kubetail: []string{link}},
	}

	assert.ErrorContains(t, p.Check(), "lies on or inside a path no rule opens")
}

// Outside compares in the disk's spelling: a Read inside a path that a link
// spells in another case is dropped.
func TestOutsideDropsARuleInsideAPathLinkedInAnotherCase(t *testing.T) {
	folder, link := linkInAnotherCase(t)
	f := FilePolicy{Read: []string{folder, filepath.Dir(folder)}}

	assert.Equal(t, []string{filepath.Dir(folder)}, f.Outside(link).Read)
}

// A toolchain or shell folder that is the home in another case would compile
// to a Read on the home.
func TestAFolderLinkedInAnotherCaseIsBroad(t *testing.T) {
	folder, link := linkInAnotherCase(t)

	assert.True(t, isBroad(link, broadDirs(folder)))
}

func TestAPathEntryLinkedInAnotherCaseIsBroad(t *testing.T) {
	folder, link := linkInAnotherCase(t)

	_, _, reason := SearchFolder(link, nil, Broad(folder))
	assert.Equal(t, "broad", reason)

	got, _, reason := SearchFolder(link, nil, nil)
	assert.Empty(t, reason)
	assert.Equal(t, folder, got, "the folder as the disk spells it")
}
