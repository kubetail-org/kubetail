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

//go:build ignore

// Reads the nightly stress run's `go test -json` output and writes the failures
// to failures.json beside it, for the workflow's report job to file as issues.
//
//	go run ../scripts/stress.go -cpu 4 -count 3 -race=true -runner ubuntu-24.04-race-cpu4 stress/stress.json
//
// It also prints them to the log and to $GITHUB_STEP_SUMMARY. It exits 0
// whatever it finds: the workflow fails on go test's own status.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const workspacePath = "github.com/kubetail-org/kubetail"

// The modules outside go.work, by their directory from the repo root. A
// command for one of their packages runs in the module's directory.
var outsideModules = []string{"apps/desktop/sidecar"}

// The bounds on a package's output excerpt. The issue body that holds it is
// limited to 65536 characters.
const (
	excerptLines = 80
	excerptBytes = 6000
)

// The test2json fields this reads. A build's events carry ImportPath and no
// Package, and the package's fail event names the build in FailedBuild.
type event struct {
	Action      string
	Package     string
	Test        string
	Output      string
	ImportPath  string
	FailedBuild string
}

// One test failing in this job.
type failure struct {
	Package  string `json:"package"`
	Test     string `json:"test"`
	CPU      int    `json:"cpu"`
	Seed     string `json:"seed"`
	TimedOut bool   `json:"timedOut"`
	Command  string `json:"command"`
}

// A package failing in this job with no test to blame: a build failure, a
// panic in TestMain, a race found after the last test, a crash of the test
// binary.
type outside struct {
	Package string `json:"package"`
	CPU     int    `json:"cpu"`
	Seed    string `json:"seed"`
	// Tests started and never finished: those a crash cut short.
	Running []string `json:"running"`
	// The head of the output no finished test holds, and how many lines it
	// leaves out.
	Output  string `json:"output"`
	Omitted int    `json:"omitted"`
	Command string `json:"command"`
}

type report struct {
	Runner   string    `json:"runner"`
	Failures []failure `json:"failures"`
	Outside  []outside `json:"outside"`
}

// One output line, numbered across the package so buffers merge in order.
type line struct {
	seq  int
	text string
}

// The first excerptLines lines of one stream of output, and a count of the
// rest. No excerpt takes more than excerptLines lines from one buffer.
type buffer struct {
	lines []line
	more  int
}

func (b *buffer) add(l line) {
	if len(b.lines) < excerptLines {
		b.lines = append(b.lines, l)
	} else {
		b.more++
	}
}

type pkgState struct {
	seed     string
	timedOut bool
	failed   bool
	running  map[string]bool // top-level tests started and not finished
	failures []failure
	// Output no running test holds, and that of each test, subtests
	// included, started and not finished. A finished test's output is
	// dropped.
	out   buffer
	open  map[string]*buffer
	build string // the ImportPath of a failed build
}

func main() {
	cpu := flag.Int("cpu", 1, "the -cpu go test ran with")
	count := flag.Int("count", 1, "the -count go test ran with")
	race := flag.Bool("race", true, "whether go test ran with -race")
	runner := flag.String("runner", "", "the job's name in the report")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: go run ../scripts/stress.go [flags] stress.json")
		os.Exit(2)
	}

	if err := run(flag.Arg(0), *runner, *cpu, *count, *race); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, runner string, cpu, count int, race bool) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	r, err := read(in, cpu, count, race)
	if err != nil {
		return err
	}
	r.Runner = runner

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "failures.json"), data, 0o644); err != nil {
		return err
	}

	summary := markdown(r)
	fmt.Print(summary)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := f.WriteString(summary); err != nil {
			return err
		}
	}
	return nil
}

// read folds the event stream into failures.
func read(in io.Reader, cpu, count int, race bool) (report, error) {
	pkgs := map[string]*pkgState{}
	builds := map[string]*buffer{}
	seq := 0
	var order []string
	dec := json.NewDecoder(bufio.NewReader(in))
	for {
		var e event
		if err := dec.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			return report{}, err
		}
		seq++
		if e.Package == "" {
			if e.Action == "build-output" && e.ImportPath != "" {
				b := builds[e.ImportPath]
				if b == nil {
					b = &buffer{}
					builds[e.ImportPath] = b
				}
				b.add(line{seq, e.Output})
			}
			continue
		}
		p := pkgs[e.Package]
		if p == nil {
			p = &pkgState{running: map[string]bool{}, open: map[string]*buffer{}}
			pkgs[e.Package] = p
			order = append(order, e.Package)
		}

		if e.Test == "" {
			switch e.Action {
			case "output":
				if seed, ok := strings.CutPrefix(e.Output, "-test.shuffle "); ok {
					p.seed = strings.TrimSpace(seed)
				} else if !isSummary(e.Output, e.Package) {
					p.out.add(line{seq, e.Output})
				}
			case "fail":
				p.failed = true
				p.build = e.FailedBuild
			}
			continue
		}
		switch e.Action {
		case "run":
			p.open[e.Test] = &buffer{}
		case "output":
			// test2json names the test that printed last, so a crash after a
			// test finished is printed under its name.
			if b := p.open[e.Test]; b == nil {
				p.out.add(line{seq, e.Output})
			} else if isResult(e.Output, e.Test) {
				delete(p.open, e.Test)
			} else if !strings.HasPrefix(e.Output, "=== ") {
				b.add(line{seq, e.Output})
			}
		case "pass", "fail", "skip":
			delete(p.open, e.Test)
		}
		if e.Action == "output" && strings.HasPrefix(e.Output, "panic: test timed out after ") {
			p.timedOut = true
		}
		// A subtest's failure fails its parent too, and the issue is the parent's.
		if strings.Contains(e.Test, "/") {
			continue
		}
		switch e.Action {
		case "run":
			p.running[e.Test] = true
		case "fail":
			p.failures = addFailure(p.failures, failure{Package: e.Package, Test: e.Test, CPU: cpu})
			delete(p.running, e.Test)
		case "pass", "skip":
			delete(p.running, e.Test)
		}
	}

	r := report{Failures: []failure{}, Outside: []outside{}}
	for _, name := range order {
		p := pkgs[name]
		if !p.failed {
			continue
		}
		// The alarm names the tests running when it fired, and test2json
		// closes none of them.
		if p.timedOut {
			for test := range p.running {
				p.failures = addFailure(p.failures, failure{Package: name, Test: test, CPU: cpu, TimedOut: true})
			}
		}
		if len(p.failures) == 0 {
			o := outside{Package: name, CPU: cpu, Seed: p.seed, Running: slices.AppendSeq([]string{}, maps.Keys(p.open))}
			slices.Sort(o.Running)
			bufs := []*buffer{&p.out}
			if b := builds[p.build]; b != nil {
				bufs = append(bufs, b)
			}
			for _, b := range p.open {
				bufs = append(bufs, b)
			}
			o.Output, o.Omitted = excerpt(bufs)
			o.Command = command(name, cpu, p.seed, count, race)
			r.Outside = append(r.Outside, o)
			continue
		}
		for _, f := range p.failures {
			f.Seed = p.seed
			f.Command = command(f.Package, f.CPU, f.Seed, count, race)
			r.Failures = append(r.Failures, f)
		}
	}
	return r, nil
}

// addFailure keeps one entry per test, however many rounds failed it.
func addFailure(fs []failure, f failure) []failure {
	for i, g := range fs {
		if g.Test == f.Test {
			fs[i].TimedOut = g.TimedOut || f.TimedOut
			return fs
		}
	}
	return append(fs, f)
}

// isSummary reports whether s is the line go test ends a failed package
// with, which says nothing the issue does not.
func isSummary(s, pkg string) bool {
	return strings.HasPrefix(s, "FAIL\t"+pkg+"\t") || strings.HasPrefix(s, "FAIL\t"+pkg+" [")
}

// isResult reports whether s is the line that ends test, which test2json
// follows with the test's pass, fail or skip only when the next test starts.
func isResult(s, test string) bool {
	s = strings.TrimLeft(s, " ")
	for _, r := range []string{"--- PASS: ", "--- FAIL: ", "--- SKIP: "} {
		if strings.HasPrefix(s, r+test+" ") {
			return true
		}
	}
	return false
}

// excerpt merges the buffers in output order and returns the head of the
// result, within excerptLines and excerptBytes, and how many lines it leaves
// out.
func excerpt(bufs []*buffer) (string, int) {
	var all []line
	omitted := 0
	for _, b := range bufs {
		all = append(all, b.lines...)
		omitted += b.more
	}
	slices.SortFunc(all, func(a, b line) int { return a.seq - b.seq })

	var sb strings.Builder
	n := 0
	for _, l := range all {
		if n == excerptLines || sb.Len()+len(l.text) > excerptBytes {
			break
		}
		sb.WriteString(l.text)
		n++
	}
	// A first line too long for the excerpt is cut to fit.
	if n == 0 && len(all) > 0 {
		sb.WriteString(strings.ToValidUTF8(all[0].text[:excerptBytes], ""))
		n = 1
	}
	// The last line test2json gives may have no newline, and a fence after it
	// must start a line.
	if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
		sb.WriteString("\n")
	}
	return sb.String(), omitted + len(all) - n
}

// command reproduces a failure from the repo root. The seed orders the whole
// package, so it runs the whole package. A package that failed before its
// tests started printed no seed.
func command(pkg string, cpu int, seed string, count int, race bool) string {
	rel := strings.TrimPrefix(pkg, workspacePath+"/")
	var b strings.Builder
	b.WriteString("go")
	for _, m := range outsideModules {
		if inModule, ok := strings.CutPrefix(rel, m); ok && (inModule == "" || inModule[0] == '/') {
			fmt.Fprintf(&b, " -C %s", m)
			rel = strings.TrimPrefix(inModule, "/")
			break
		}
	}
	dir := "./" + rel
	if seed == "" {
		seed = "on"
	}
	b.WriteString(" test")
	if race {
		b.WriteString(" -race")
	}
	fmt.Fprintf(&b, " -cpu %d -shuffle=%s -count=%d %s", cpu, seed, count, dir)
	return b.String()
}

// fence returns a code fence longer than any run of backticks in s, so the
// text cannot close it.
func fence(s string) string {
	longest, run := 0, 0
	for _, c := range s {
		if c == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(3, longest+1))
}

func markdown(r report) string {
	if len(r.Failures) == 0 && len(r.Outside) == 0 {
		return "No test failed.\n"
	}
	var b strings.Builder
	if len(r.Failures) > 0 {
		b.WriteString("| Package | Test | -cpu | Seed | Reproduce |\n|---|---|---|---|---|\n")
		for _, f := range r.Failures {
			test := f.Test
			if f.TimedOut {
				test += " (timed out)"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %s | `%s` |\n", f.Package, test, f.CPU, f.Seed, f.Command)
		}
	}
	for _, o := range r.Outside {
		fmt.Fprintf(&b, "\n%s failed outside a test", o.Package)
		if len(o.Running) > 0 {
			fmt.Fprintf(&b, " while %s ran", strings.Join(o.Running, ", "))
		}
		fmt.Fprintf(&b, ". Reproduce with `%s`.\n\n", o.Command)
		f := fence(o.Output)
		fmt.Fprintf(&b, "%stext\n%s%s\n", f, o.Output, f)
		if o.Omitted > 0 {
			fmt.Fprintf(&b, "\n%d more lines in the job's stress.json.\n", o.Omitted)
		}
	}
	return b.String()
}
