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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// systemPastas are where a distribution puts pasta, in the order looked for.
// pasta is never found off PATH.
var systemPastas = []string{"/usr/bin/pasta", "/bin/pasta", "/usr/local/bin/pasta", "/run/current-system/sw/bin/pasta"}

// hostResolvConf is the host's resolv.conf, whose resolved path a run's
// resolver is bound over: a test's seam.
var hostResolvConf = "/etc/resolv.conf"

// probePasta decides whether a run on s can be given the internet, setting
// s.pasta to the first of pastas that runs once with s's bwrap inside it, or
// s.networkReason to every one's reason. It checks only what holds for the
// sidecar's life, never a route, so a Kubetail started offline still offers
// network once the machine is online.
func (s *Sandbox) probePasta(ctx context.Context, pastas []string, bound time.Duration) {
	if len(pastas) == 0 {
		s.networkReason = "pasta not found"
		return
	}
	var reasons []string
	for _, pasta := range pastas {
		ownUserNS, err := s.tryPasta(ctx, pasta, bound)
		if err != nil {
			reasons = append(reasons, pasta+": "+err.Error())
			continue
		}
		s.pasta, s.pastaOwnUserNS = pasta, ownUserNS
		return
	}
	s.networkReason = strings.Join(reasons, "; ")
}

// pastaProbeScript prints the run's first uid_map line, then its uid, gid
// and effective capabilities, with the shell's builtins alone.
const pastaProbeScript = `read -r m < /proc/self/uid_map; echo "$m"
while read -r k v rest; do case "$k" in Uid:|Gid:|CapEff:) echo "$k $v";; esac; done < /proc/self/status`

// tryPasta runs a shell under pasta, as a run with the internet but with no
// network configured, and answers whether its user namespace is its own. It
// fails unless the shell ran as the user, holding no capability.
func (s *Sandbox) tryPasta(ctx context.Context, pasta string, bound time.Duration) (bool, error) {
	dir, err := os.MkdirTemp("", "kubetail-probe-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)
	home, _ := os.UserHomeDir()
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	p, err := s.probePolicyWithin(ctx, "/bin/sh", dir, home)
	if err != nil {
		return false, fmt.Errorf("no answer in %s", bound)
	}
	p.Network.Internet = true
	r := Run{
		Shell: "/bin/sh", Args: []string{"-c", pastaProbeScript}, Dir: dir,
		Env: []string{"PATH=/usr/bin:/bin", "HOME=" + dir, "TMPDIR=" + dir}, Policy: p,
	}
	bwrap := s.bwrap
	line, err := bounded(ctx, func() (commandLine, error) {
		rules, err := s.checkRun(r)
		if err != nil {
			return commandLine{}, err
		}
		return commandLine{s.self, s.pastaArgs(bwrap, pasta, r, rules, "", false)}, nil
	})
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return false, fmt.Errorf("no answer in %s", bound)
	case err != nil:
		return false, err
	}
	cmd, err := s.command(ctx, line, r.Env)
	if err != nil {
		return false, err
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runProbe(cmd)
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return false, fmt.Errorf("no answer in %s", bound)
	case err != nil:
		if line := firstLine(stderr.String()); line != "" {
			return false, errors.New(line)
		}
		return false, err // coverage:ignore under coverage the launcher, a test binary, always warns on stderr, so no failure here is silent
	}
	return readPastaProbe(stdout.String())
}

// readPastaProbe reads pastaProbeScript's output: whether the run's user
// namespace was its own, or why the run did not hold as the user's.
func readPastaProbe(out string) (bool, error) {
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	got := map[string]string{}
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, " ")
		got[k] = v
	}
	want := map[string]string{"Uid:": strconv.Itoa(os.Getuid()), "Gid:": strconv.Itoa(os.Getgid()), "CapEff:": "0000000000000000"}
	for k, v := range want {
		if got[k] != v {
			return false, fmt.Errorf("the run's %s is %q, not %q", strings.TrimSuffix(k, ":"), got[k], v)
		}
	}
	own, _ := os.ReadFile("/proc/self/uid_map")
	return ownNamespace(lines[0], firstLine(string(own))), nil
}

// NetworkStatus is whether a run here can be given the internet, and why not.
func (s *Sandbox) NetworkStatus() (bool, string) {
	return s.pasta != "", s.networkReason
}

// probeNet is the probe's network in place of --config-net: an interface,
// addresses and a MAC of its own, so pasta looks for no route: an older pasta,
// Ubuntu 24.04's among them, will not start without one.
var probeNet = []string{"-4", "-i", "lo", "-I", "kubetail0", "-a", "192.0.2.2", "-g", "192.0.2.1", "-M", "02:00:00:00:00:01"}

// pastaArgs is the arguments to this executable that run r, which has the
// internet, under the bwrap at bwrap inside the pasta at pasta, which
// sandbox-pasta starts: pasta makes a user and network namespace and runs
// bwrap inside it, where bwrap makes the run's own user namespace as the user. resolverAt is where r's resolver is bound, "" for none; configNet
// is false for the probe alone, which passes probeNet. sandbox-pasta watches
// the lifeline, not the forwarder: pasta closes every descriptor it did not
// open but the standard streams.
func (s *Sandbox) pastaArgs(bwrap, pasta string, r Run, rules []rule, resolverAt string, configNet bool) []string {
	args := slices.Concat([]string{PastaCommand}, s.lifelineArgs(), []string{"--", pasta})
	if configNet {
		args = append(args, "--config-net")
	} else {
		args = append(args, probeNet...)
	}
	// --no-map-gw maps no address to the host's loopback; -T none -U none
	// forward no port to it, and -t none -u none none inward.
	args = append(args, "--quiet", "--no-map-gw", "-t", "none", "-u", "none", "-T", "none", "-U", "none",
		"--dns-forward", ResolverAddress)
	args = append(args, "--", bwrap)
	return append(args, s.args(r, rules, resolverAt)...)
}

// resolverTarget is where r's resolver is bound: the host's resolv.conf as
// resolved now, so a link the run reads at /etc/resolv.conf leads to it, or
// /etc/resolv.conf itself on a host with none. It refuses a path that would
// replace a mount every run has, as a rule's is refused, and one under a
// denial, where the bind would show what the denial hides.
func resolverTarget(p Policy) (string, error) {
	at := hostResolvConf
	if r, err := filepath.EvalSymlinks(hostResolvConf); err == nil {
		at = r
	}
	if overFixedMount(at) || realpath.UnderAny(at, slices.Concat(p.Files.Deny, p.Always.Deny, p.Always.Kubetail)) {
		return "", fmt.Errorf("the host's resolv.conf is %s, where a run's resolver cannot be bound", at)
	}
	return at, nil
}
