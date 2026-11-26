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
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// probeTimeout bounds the probe: its profile, and its sandboxed true under
// the forwarder, which starts this executable once more.
const probeTimeout = 5 * time.Second

// Probe answers Seatbelt, available when sandbox-exec runs true under the
// profile a run with no cluster gets, or ctx's error when it ended first.
func Probe(ctx context.Context) (*Sandbox, State, error) {
	s, status := probe(ctx, "/usr/bin/sandbox-exec", probeTimeout)
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, State{}, err
	}
	return s, status, nil
}

// probe is Probe with the launcher at path, bounded by timeout. A probe that
// runs out of time keeps the sandbox: it runs once, at startup, so a slow
// start must not leave the whole session unconfined. A run that then fails
// under the profile fails closed.
func probe(ctx context.Context, path string, timeout time.Duration) (*Sandbox, State) {
	if _, err := os.Stat(path); err != nil {
		return nil, State{Reason: "sandbox-exec not found"}
	}
	self, err := os.Executable()
	if err != nil { // coverage:ignore the kernel always knows a running process's own executable
		return nil, State{Reason: "cannot find its own executable"}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, State{Reason: "cannot find the home directory"}
	}
	dir, err := os.MkdirTemp("", "kubetail-probe-")
	if err != nil {
		return nil, State{Reason: "cannot make a directory to probe in"}
	}
	defer os.RemoveAll(dir)

	lifeline, held, err := newLifeline()
	if err != nil {
		return nil, State{Reason: "cannot make the runs' lifeline"}
	}
	s := &Sandbox{self: self, launcher: path, lifeline: lifeline, held: held}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	shell, env := "/usr/bin/true", []string{"PATH=" + os.Getenv("PATH")}
	var cmd *exec.Cmd
	p, err := s.probePolicyWithin(ctx, shell, dir, home)
	if err == nil {
		cmd, err = s.Command(ctx, Run{Shell: shell, Dir: dir, Env: env, Policy: p})
	}
	var stderr bytes.Buffer
	if err == nil {
		cmd.Stderr = &stderr
		err = cmd.Run()
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return s, State{Available: true, Reason: "Seatbelt, unconfirmed: the probe did not finish within " + timeout.String(), NetworkAvailable: true}
	case err != nil:
		_ = s.Close()
		return nil, State{Reason: probeFailure(stderr.String(), err)}
	}
	return s, State{Available: true, Reason: "Seatbelt", NetworkAvailable: true}
}

// probeFailure is why the probe's run failed: the first line of its stderr,
// else what ended it.
func probeFailure(stderr string, err error) string {
	if line := firstLine(strings.TrimSpace(stderr)); line != "" {
		return line
	}
	return err.Error()
}

// Command is the process that runs r sandboxed, not yet started, made by
// exec.CommandContext on ctx, or why r's policy cannot be enforced, a memory
// limit included: sandbox-exec over r's profile, then the forwarder with
// sandbox-shell its child, which execs the shell.
// sandbox-exec execs into it, so the run leads a session of its own, which the
// profile keeps every descendant in: kern.procargs2 is refused only for a
// process in another process group. A caller that replaces SysProcAttr keeps
// Setsid. The caller sets its output and Cancel, and starts it.
//
// Check and the profile resolve paths, which can hang on a network mount, so
// ctx bounds them too: ctx ending first answers its error.
func (s *Sandbox) Command(ctx context.Context, r Run) (*exec.Cmd, error) {
	if r.Policy.Limits.MemoryBytes > 0 {
		return nil, errNoMemoryLimit
	}
	if r.Policy.Network.Resolver != "" {
		return nil, errNoResolver
	}
	type built struct {
		text   string
		params []string
	}
	build := s.buildProfile
	if build == nil {
		build = s.profile
	}
	r = r.clone()
	b, err := bounded(ctx, func() (built, error) {
		rules, err := r.check()
		if err != nil {
			return built{}, err
		}
		text, params := build(r, rules)
		return built{text, params}, nil
	})
	if err != nil {
		return nil, err
	}
	name, args := s.argv(r)
	cmd := exec.CommandContext(ctx, s.launcher, slices.Concat([]string{"-p", b.text}, b.params, []string{name}, args)...)
	if err := s.handLifeline(cmd); err != nil {
		return nil, err
	}
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd, nil
}

// errNoResolver is a run naming a resolv.conf, which a Seatbelt run has no
// mount to put in place: it resolves through mDNSResponder.
var errNoResolver = errors.New("a macOS run takes no resolver")

// Confines reports whether a command run through s is confined: always, here.
func (s *Sandbox) Confines() bool { return true }

// NeedsResolver reports whether a run with the internet needs a resolv.conf
// of its own: never, here, since it resolves through mDNSResponder.
func (s *Sandbox) NeedsResolver() bool { return false }

// Port is a free loopback port for a run's forwarder. Seatbelt has no private
// loopback, so it is the host's. The forwarder listens on it before the
// command starts and exits when it cannot, so a process that takes it in
// between fails the run rather than answering it.
func (s *Sandbox) Port() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// argv is the process that runs r: this executable as the run's forwarder,
// watching the lifeline, with this executable as sandbox-shell its child,
// which sets r's limits and execs the shell.
func (s *Sandbox) argv(r Run) (name string, args []string) {
	init := slices.Insert(ForwarderArgs(r.Policy.Network.relay()), 1, s.lifelineArgs()...)
	return s.self, slices.Concat(init, []string{s.self}, shellCommand(r.Policy.Limits, s.cpuGraceSeconds()), []string{r.Shell}, r.Args)
}

// profileText is the Seatbelt profile's fixed rules, with a marker line where
// the policy's rules, the ancestors and the network go.
//
//go:embed profile_darwin.sb
var profileText string

// The marker lines profile replaces.
const (
	markerRules     = ";; RULES\n"
	markerAncestors = ";; ANCESTORS\n"
	markerNetwork   = ";; NETWORK\n"
)

// refusedServices are the Mach services no profile names: a service that
// resolves names, holds the Keychain, opens or drives an app, holds the
// pasteboard, searches the home, or fetches a URL for its caller. One ending
// in * refuses every name it begins.
var refusedServices = []string{
	"com.apple.dnssd.service",
	"com.apple.SecurityServer", "com.apple.securityd", "com.apple.securityd.xpc", "com.apple.secd",
	"com.apple.security.agent",
	"com.apple.coreservices.launchservicesd", "com.apple.lsd.*",
	"com.apple.coreservices.appleevents",
	"com.apple.pasteboard.*",
	"com.apple.metadata.*",
	"com.apple.trustd", "com.apple.trustd.agent", "com.apple.nsurlsessiond",
}

// internetRules is a run's internet: IPv4 to every address but the host's
// own, and the resolver and the trust daemon, which refusedServices names and
// a run without it never reaches. Seatbelt's localhost under ip4 matches every
// address the host holds, loopback and the unspecified address included, but
// only with the deny of all IPv6 after it: without that deny, or with it
// before, the localhost deny shuts nothing. IPv6 is denied whole because Seatbelt names
// no host but localhost and *, and an IPv4-mapped address such as
// ::ffff:0.0.0.0 reaches the host past any deny it can write. A later rule
// wins, so the relay's own loopback port, which follows these, stays open.
// The darwin tests pin each of those. Other hosts on the local network stay
// open with the internet.
const internetRules = `(allow network-outbound (remote ip4 "*:*"))
(deny network-outbound (remote ip4 "localhost:*"))
(deny network-outbound (remote ip6 "*:*"))
(allow network-outbound (literal "/private/var/run/mDNSResponder"))
(allow mach-lookup
  (global-name "com.apple.dnssd.service")
  (global-name "com.apple.trustd.agent"))
`

// refused reports whether refusedServices names service.
func refused(service string) bool {
	return slices.ContainsFunc(refusedServices, func(r string) bool {
		if prefix, ok := strings.CutSuffix(r, "*"); ok {
			return strings.HasPrefix(service, prefix)
		}
		return service == r
	})
}

// relayRules is one relay's network: its port, which is the one value written
// into the text, and its socket, by the path as given and as resolved, since
// Seatbelt's match on a socket's path is undocumented. The port is TCP on
// 127.0.0.1 alone, the one endpoint the forwarder holds: UDP or IPv6 at that
// number could be another service's.
const relayRules = `(allow network-bind network-inbound (local tcp4 "localhost:%[1]d"))
(allow network-outbound
  (remote tcp4 "localhost:%[1]d")
  (remote unix-socket (path-literal (param "SOCKET")))
  (remote unix-socket (path-literal (param "SOCKET_RESOLVED"))))
`

// profile is r's Seatbelt profile and the -D arguments it reads. Every path
// is canonical, since Seatbelt checks a file's real path, and is a parameter,
// since a path can hold what the text would read as code. A Read rule carries
// its refusal to write, so it decides a tie with a Write rule the way a
// read-only mount does.
func (s *Sandbox) profile(r Run, compiled []rule) (text string, params []string) {
	var rules strings.Builder
	var reached []string
	for i, ru := range compiled {
		name := "RULE_" + strconv.Itoa(i)
		params = append(params, "-D", name+"="+ru.at)
		switch ru.kind {
		case ruleRead:
			fmt.Fprintf(&rules, "(allow file-read* (subpath (param \"%[1]s\")))\n(deny file-write* (subpath (param \"%[1]s\")))\n", name)
			reached = append(reached, ru.at)
		case ruleWrite:
			fmt.Fprintf(&rules, "(allow file-read* file-write* (subpath (param \"%s\")))\n", name)
			// A run that could unlink or create its own path's root could put a
			// link there, and the next run's profile would open where it leads.
			if ru.own {
				fmt.Fprintf(&rules, "(deny file-write-unlink file-write-create (literal (param \"%s\")))\n", name)
			}
			reached = append(reached, ru.at)
		case ruleDeny:
			fmt.Fprintf(&rules, "(deny file-read* file-write* (subpath (param \"%s\")))\n", name)
		}
	}

	var network string
	if r.Policy.Network.Internet {
		network = internetRules
	}
	if relay := r.Policy.Network.relay(); relay.Socket != "" {
		params = append(params, "-D", "SOCKET="+relay.Socket, "-D", "SOCKET_RESOLVED="+realpath.Resolved(relay.Socket))
		network += fmt.Sprintf(relayRules, relay.Port)
	}
	var up strings.Builder
	for i, p := range ancestors(reached) {
		name := "UP_" + strconv.Itoa(i)
		params = append(params, "-D", name+"="+p)
		fmt.Fprintf(&up, "(allow file-read-metadata (literal (param \"%s\")))\n", name)
	}
	text = strings.NewReplacer(
		markerRules, rules.String(),
		markerAncestors, up.String(),
		markerNetwork, network,
	).Replace(profileText)
	return text, params
}

// ancestors is every directory above one of paths, sorted.
func ancestors(paths []string) []string {
	seen := map[string]bool{}
	for _, p := range paths {
		for d := filepath.Dir(p); !seen[d]; d = filepath.Dir(d) {
			seen[d] = true
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// overFixedMount reports whether a rule on p would replace a mount every run
// has: never, since a Seatbelt run has no mounts of its own.
func overFixedMount(string) bool { return false }

// NetworkStatus is whether a run here can be given the internet: wherever the
// sandbox is, here.
func (s *Sandbox) NetworkStatus() (bool, string) { return true, "" }

// FixedMount reports whether p is on or under /dev, a device filesystem a
// folder grant has no business opening.
func FixedMount(p string) bool { return realpath.Within(filepath.Clean(p), "/dev") }
