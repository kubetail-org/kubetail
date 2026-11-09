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
	"os"
	"path/filepath"
	"strings"
)

const workspacePath = "github.com/kubetail-org/kubetail/modules"

// The test2json fields this reads.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
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

type report struct {
	Runner   string    `json:"runner"`
	Failures []failure `json:"failures"`
	// Packages that failed with no test to blame: a build failure, a panic in
	// TestMain, a race found after the last test.
	Outside []string `json:"outside"`
}

type pkgState struct {
	seed     string
	timedOut bool
	failed   bool
	running  map[string]bool // top-level tests started and not finished
	failures []failure
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
	var order []string
	dec := json.NewDecoder(bufio.NewReader(in))
	for {
		var e event
		if err := dec.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			return report{}, err
		}
		if e.Package == "" {
			continue
		}
		p := pkgs[e.Package]
		if p == nil {
			p = &pkgState{running: map[string]bool{}}
			pkgs[e.Package] = p
			order = append(order, e.Package)
		}

		if e.Test == "" {
			switch e.Action {
			case "output":
				if seed, ok := strings.CutPrefix(e.Output, "-test.shuffle "); ok {
					p.seed = strings.TrimSpace(seed)
				}
			case "fail":
				p.failed = true
			}
			continue
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

	r := report{Failures: []failure{}, Outside: []string{}}
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
			r.Outside = append(r.Outside, name)
			continue
		}
		for _, f := range p.failures {
			f.Seed = p.seed
			f.Command = command(f, count, race)
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

// command reproduces a failure from modules/. The seed orders the whole
// package, so it runs the whole package.
func command(f failure, count int, race bool) string {
	dir := "./" + strings.TrimPrefix(f.Package, workspacePath+"/")
	var b strings.Builder
	b.WriteString("go test")
	if race {
		b.WriteString(" -race")
	}
	fmt.Fprintf(&b, " -cpu %d -shuffle=%s -count=%d %s", f.CPU, f.Seed, count, dir)
	return b.String()
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
	if len(r.Outside) > 0 {
		b.WriteString("\nFailed outside a test:\n\n")
		for _, p := range r.Outside {
			fmt.Fprintf(&b, "- %s\n", p)
		}
	}
	return b.String()
}
