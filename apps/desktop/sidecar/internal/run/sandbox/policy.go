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
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/realpath"
)

// Policy is everything the OS sandbox enforces for a run.
type Policy struct {
	Files   FilePolicy
	Always  AlwaysPolicy
	Network NetworkPolicy
	Limits  Limits
}

// FilePolicy is what a run may do with files. A rule covers a path and
// everything under it, and decides all a run may do there. The deepest rule
// wins, the narrower rule wins a tie, and nothing sits beneath a Write rule. A
// path no rule covers is off limits.
type FilePolicy struct {
	Read  []string // readable, not writable
	Write []string // readable and writable
	Deny  []string // neither
}

// AlwaysPolicy is what no FilePolicy rule opens: the denied-always list, where
// nothing opens, and Kubetail's own directories, where only the run's own paths
// open.
type AlwaysPolicy struct {
	Deny     []string // the denied-always list
	Kubetail []string // Kubetail's folders; one inside another is denied with it
	Read     []string // the run's own paths it reads, each inside a Kubetail path
	Write    []string // and reads and writes
	// LinkParents is the folder holding each link Kubetail followed to its
	// folders. A run that could write one could repoint the link, so no
	// Write rule may be on or above one.
	LinkParents []string
}

// NetworkPolicy is how a run reaches past the machine. The zero value is no
// network at all.
type NetworkPolicy struct {
	Relays []Relay
	// Internet lets the run reach the internet. The host's loopback stays shut.
	Internet bool
	// Resolver is a resolv.conf the caller wrote for a run with Internet, bound
	// over the system's on Linux; "" on macOS and for a run without Internet.
	Resolver string
}

// Relay is a loopback port inside the run that the forwarder connects to a
// Unix socket outside it.
type Relay struct {
	Port   int
	Socket string
}

// Limits bounds what a run's processes may use. Zero is the platform's
// default for that resource. Each holds per process but Processes, which
// the kernel holds against a count (Sandbox.CountedProcesses).
type Limits struct {
	CPUSeconds  int // CPU time per process
	MemoryBytes int // address space per process; Linux alone
	OpenFiles   int // open descriptors per process
	Processes   int // the kernel's count of tasks (Linux) or processes (macOS)
}

// cpuGrace is how far a run's hard CPU limit sits above its soft one, so a
// process that traps SIGXCPU can say something before the kernel kills it.
const cpuGrace = 5

// cpuGraceSeconds is the grace s's runs take: cpuGrace, or a test's.
func (s *Sandbox) cpuGraceSeconds() int { return cmp.Or(s.testCPUGrace, cpuGrace) }

// check is why l cannot be enforced, or nil. sandbox-shell execs under memory
// and process limits without restoring the open-files limit the Go runtime
// raises, so either needs that limit set.
func (l Limits) check() error {
	if min(l.CPUSeconds, l.MemoryBytes, l.OpenFiles, l.Processes) < 0 {
		return fmt.Errorf("a negative limit: %+v", l)
	}
	if (l.MemoryBytes > 0 || l.Processes > 0) && l.OpenFiles == 0 {
		return errors.New("a memory or process limit with no open-files limit")
	}
	return nil
}

// Check is why p cannot be enforced as written, or nil.
func (p Policy) Check() error {
	_, err := p.compile()
	return err
}

// compile is p's rules, or why p cannot be enforced as written. It reads each
// path's canonical form once, so the rules it checks are the rules it answers.
func (p Policy) compile() ([]rule, error) {
	c, err := p.canonicalPaths()
	if err != nil {
		return nil, err
	}
	if err := p.check(c); err != nil {
		return nil, err
	}
	return p.rules(c), nil
}

// canonicalPaths is each path a policy's rules name and its canonical form.
type canonicalPaths map[string]string

// of is each of paths, canonical.
func (c canonicalPaths) of(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = c[p]
	}
	return out
}

// canonicalPaths is p's canonicalPaths, or why it has none: a path that is not
// absolute, one the kernel cannot name, or two that name one folder two ways.
// The rules are checked and compiled by their text, and compared under
// another name a rule could miss one it overlaps, and a Deny be dropped as out
// of reach.
func (p Policy) canonicalPaths() (canonicalPaths, error) {
	for _, path := range p.paths() {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s is not an absolute path", path)
		}
	}
	named := slices.Concat(p.rulePaths(), p.ownResolver(), p.Always.LinkParents)
	at, err := realpath.Canonical(named)
	if err == nil {
		err = aliased(at, fileOf)
	}
	if err != nil {
		return nil, err
	}
	c := canonicalPaths{}
	for i, path := range named {
		c[path] = at[i]
	}
	return c, nil
}

// aliased is why paths cannot be compared by text, or nil: two of them hold a
// file its folder names two ways, as a folder that folds names does where the
// kernel gives no stored spelling. file is the file at a path (fileOf).
func aliased(paths []string, file func(string) (fileID, bool)) error {
	type entry struct {
		folder string
		file   fileID
	}
	names := map[entry]string{}
	seen := map[string]bool{}
	for _, p := range paths {
		for at := p; !seen[at]; at = filepath.Dir(at) {
			seen[at] = true
			f, ok := file(at)
			if !ok {
				continue
			}
			e := entry{filepath.Dir(at), f}
			if other, named := names[e]; named {
				return fmt.Errorf("%s and %s name one file", other, at)
			}
			names[e] = at
		}
	}
	return nil
}

// check is why p cannot be enforced as written, or nil. Paths are compared
// canonical, since both sandboxes check a file at its real location.
func (p Policy) check(c canonicalPaths) error {
	// A run can rename what a Write rule covers, so a rule beneath one could be
	// moved out from under.
	writes := c.of(slices.Concat(p.Files.Write, p.Always.Write))
	rulePaths := p.rulePaths()
	for i, r := range c.of(rulePaths) {
		for _, w := range writes {
			if r != w && realpath.Within(r, w) {
				return fmt.Errorf("%s lies beneath the Write rule %s", rulePaths[i], w)
			}
		}
	}
	for i, parent := range c.of(p.Always.LinkParents) {
		for _, w := range writes {
			if realpath.Within(parent, w) {
				return fmt.Errorf("the Write rule %s is on or above %s, which holds a link Kubetail follows", w, p.Always.LinkParents[i])
			}
		}
	}
	kubetail, denied := c.of(p.Always.Kubetail), c.of(p.Always.Deny)
	always := slices.Concat(denied, kubetail)
	files := slices.Concat(p.Files.Read, p.Files.Write, p.Files.Deny)
	for i, r := range c.of(files) {
		if realpath.WithinAny(r, always) {
			return fmt.Errorf("%s lies on or inside a path no rule opens", files[i])
		}
	}
	mounted := slices.Concat(files, p.Always.Read, p.Always.Write)
	for i, r := range c.of(mounted) {
		if overFixedMount(r) {
			return fmt.Errorf("a rule on %s would replace a mount every run has", mounted[i])
		}
	}
	if p.Network.Resolver != "" && !p.Network.Internet {
		return errors.New("a resolver for a run without the internet")
	}
	own := slices.Concat(p.Always.Read, p.Always.Write, p.ownResolver())
	for i, r := range c.of(own) {
		if err := notLink(own[i]); err != nil {
			return err
		}
		// The run's own paths compile last, so a denial inside one would lose.
		holdsDenied := slices.ContainsFunc(denied, func(d string) bool { return realpath.Within(d, r) })
		if !realpath.WithinAny(r, kubetail) || realpath.WithinAny(r, denied) || holdsDenied {
			return fmt.Errorf("%s is not inside Kubetail's directories, clear of the denied-always list", own[i])
		}
	}
	if len(p.Network.Relays) > 1 {
		return fmt.Errorf("%d relays, and the forwarder relays one", len(p.Network.Relays))
	}
	return p.Limits.check()
}

// notLink fails when path's last component is a symbolic link, or when it
// cannot tell. A profile resolves a run's own path, so a link a run planted
// where its workspace was would open the link's target to the next. A link
// above the last component, such as macOS's /var, is the system's. A path that
// is not there opens nothing and is passed over.
func notLink(path string) error {
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return err
	case info.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("%s is a link", path)
	}
	return nil
}

// ownResolver is p's Resolver as one of the run's own paths, checked as they
// are, or none.
func (p Policy) ownResolver() []string {
	if p.Network.Resolver == "" {
		return nil
	}
	return []string{p.Network.Resolver}
}

// rulePaths is every path a file rule of p names, Files and Always.
func (p Policy) rulePaths() []string {
	return slices.Concat(p.Files.Read, p.Files.Write, p.Files.Deny, p.Always.Deny, p.Always.Kubetail, p.Always.Read, p.Always.Write)
}

// paths is every path p names.
func (p Policy) paths() []string {
	paths := slices.Concat(p.rulePaths(), p.ownResolver(), p.Always.LinkParents)
	for _, r := range p.Network.Relays {
		paths = append(paths, r.Socket)
	}
	return paths
}

// Outside is f less every Read or Write rule on or inside one of paths,
// compared as files. It reads the disk.
func (f FilePolicy) Outside(paths ...string) FilePolicy {
	dirs := realpath.ResolveDirs(paths)
	keep := func(rules []string) []string {
		return slices.DeleteFunc(slices.Clone(rules), dirs.Holds)
	}
	return FilePolicy{Read: keep(f.Read), Write: keep(f.Write), Deny: f.Deny}
}

// relay is n's one relay, or the zero Relay, which relays nothing.
func (n NetworkPolicy) relay() Relay {
	if len(n.Relays) == 0 {
		return Relay{}
	}
	return n.Relays[0]
}

// ruleKind is what a rule lets a run do at its path. Among rules on one path
// they sort in this order, so the narrower one comes last and wins.
type ruleKind int

const (
	ruleWrite ruleKind = iota
	ruleRead
	ruleDeny
)

// rule is one file rule as a platform compiles it: its path as written and
// canonical, and whether it is one of the run's own paths inside Kubetail's
// directories.
type rule struct {
	path, at string
	kind     ruleKind
	own      bool
}

// rules is p's file rules in the order both platforms compile them, where the
// last matching one wins: the Files rules, shallowest first; every Always path
// as a Deny, so no Files rule outranks it; then the run's own paths,
// shallowest first. A Read or Write rule whose path does not exist opens
// nothing and is left out. A Deny is left out where no Files Read or Write
// reaches it, since its path is off limits already.
func (p Policy) rules(c canonicalPaths) []rule {
	files := slices.Concat(
		c.rules(p.Files.Write, ruleWrite, false), c.rules(p.Files.Read, ruleRead, false), c.rules(p.Files.Deny, ruleDeny, false))
	sortRules(files)
	always := slices.Concat(c.rules(p.Always.Deny, ruleDeny, false), outermost(c.rules(p.Always.Kubetail, ruleDeny, false)))
	own := slices.Concat(c.rules(p.Always.Write, ruleWrite, true), c.rules(p.Always.Read, ruleRead, true))
	sortRules(own)

	var reach []string
	for _, r := range files {
		if r.kind != ruleDeny {
			reach = append(reach, r.at)
		}
	}
	all := slices.Concat(files, always, own)
	return slices.DeleteFunc(all, func(r rule) bool {
		if r.kind == ruleDeny {
			return !slices.ContainsFunc(reach, func(t string) bool { return realpath.Within(r.at, t) || realpath.Within(t, r.at) })
		}
		_, err := os.Stat(r.at)
		return err != nil
	})
}

// rules is a rule of kind for each of paths.
func (c canonicalPaths) rules(paths []string, kind ruleKind, own bool) []rule {
	rules := make([]rule, len(paths))
	for i, path := range paths {
		rules[i] = rule{path: path, at: c[path], kind: kind, own: own}
	}
	return rules
}

// outermost is rules less each one inside another of them, so each Kubetail
// folder is denied once. A policy's canonical paths name no folder two ways,
// so their text tells.
func outermost(rules []rule) []rule {
	var out []rule
	for _, r := range rules {
		if !slices.ContainsFunc(rules, func(o rule) bool { return o.at != r.at && realpath.Within(r.at, o.at) }) {
			out = append(out, r)
		}
	}
	return out
}

// sortRules orders rules by their canonical paths, shallowest first, and among
// rules on one path by kind. The path as written can mislead: /bin, a link to
// /usr/bin, would sort before /usr.
func sortRules(rules []rule) {
	slices.SortStableFunc(rules, func(a, b rule) int {
		return cmp.Or(
			cmp.Compare(strings.Count(a.at, string(filepath.Separator)), strings.Count(b.at, string(filepath.Separator))),
			strings.Compare(a.at, b.at),
			cmp.Compare(a.kind, b.kind))
	})
}
