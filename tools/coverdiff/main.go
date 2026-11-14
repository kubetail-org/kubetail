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

// Command coverdiff lists the changed lines a local coverage run measured but
// no test reached, as coverage / Gate would flag them. scripts/cover-changed.sh
// runs it:
//
//	go run ./coverdiff -root .. -diff changes.diff -go modules=merged.out -lcov lcov.info
//
// -diff is a unified diff (`git diff -U0`) whose added lines are the changed
// lines. Each -go is a workspace's or module's directory, relative to -root,
// and a Go cover profile of it; its go.work or go.mod maps the profile's import
// paths back to files.
// Each -lcov is a report whose SF paths are relative to -root or absolute.
// Every path printed is relative to -root.
//
// It prints `file:line` and the line's state for each one, and exits 1 when it
// lists any. A line no report measured is not listed: the gate skips it too.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/kubetail-org/kubetail/tools/internal/unidiff"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.root, "root", ".", "the repository the paths are relative to")
	flag.StringVar(&cfg.diff, "diff", "", "unified diff of the changes")
	flag.Func("go", "<workspace dir>=<Go cover profile> (repeatable)", func(v string) error {
		p, err := parseGoProfile(v)
		cfg.goProfiles = append(cfg.goProfiles, p)
		return err
	})
	flag.Func("lcov", "an lcov report (repeatable)", func(path string) error {
		cfg.lcov = append(cfg.lcov, path)
		return nil
	})
	flag.Parse()
	if cfg.diff == "" || flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: coverdiff -root <repo> -diff <diff> [-go <dir>=<profile>]... [-lcov <report>]...")
		os.Exit(2)
	}
	err := run(cfg, os.Stdout)
	if errors.Is(err, errUncovered) {
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "coverdiff:", err)
		os.Exit(1)
	}
}

type config struct {
	root       string
	diff       string
	goProfiles []goProfile
	lcov       []string
}

// goProfile is a Go cover profile and the workspace or module it measured.
type goProfile struct {
	dir, path string
}

func parseGoProfile(v string) (goProfile, error) {
	dir, path, ok := strings.Cut(v, "=")
	if !ok || dir == "" || path == "" {
		return goProfile{}, fmt.Errorf("want <dir>=<profile>, got %q", v)
	}
	return goProfile{dir: dir, path: path}, nil
}

// errUncovered is the verdict; the lines that earned it are already printed.
var errUncovered = errors.New("changed lines are not covered")

// state sums every measure of a line: each Go block spanning it, each lcov
// line or branch record on it. One that both ran and missed is partial.
type state struct {
	hit, missed bool
}

// verdict is how the gate lists the line, or empty when it is covered.
func (s state) verdict() string {
	switch {
	case !s.hit:
		return "not covered"
	case s.missed:
		return "partially covered"
	}
	return ""
}

// coverage holds each measured line's state, by file relative to the root.
type coverage map[string]map[int]state

// add records one measure of the line.
func (c coverage) add(file string, line int, ran bool) {
	lines := c[file]
	if lines == nil {
		lines = map[int]state{}
		c[file] = lines
	}
	s := lines[line]
	s.hit = s.hit || ran
	s.missed = s.missed || !ran
	lines[line] = s
}

func run(cfg config, out io.Writer) error {
	diff, err := os.Open(cfg.diff)
	if err != nil {
		return err
	}
	defer diff.Close()
	changed, err := unidiff.Added(diff)
	if err != nil {
		return err
	}
	cov := coverage{}
	for _, p := range cfg.goProfiles {
		if err := readGo(cov, cfg.root, p.dir, p.path); err != nil {
			return err
		}
	}
	for _, path := range cfg.lcov {
		if err := readLcov(cov, cfg.root, path); err != nil {
			return err
		}
	}

	files := make([]string, 0, len(changed))
	for file := range changed {
		files = append(files, file)
	}
	sort.Strings(files)
	listed := 0
	for _, file := range files {
		for _, line := range changed[file] {
			s, measured := cov[file][line]
			if v := s.verdict(); measured && v != "" {
				fmt.Fprintf(out, "%s:%d %s\n", file, line, v)
				listed++
			}
		}
	}
	if listed > 0 {
		return errUncovered
	}
	return nil
}
