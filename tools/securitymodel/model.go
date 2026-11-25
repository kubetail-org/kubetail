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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	modelPath    = "docs/security-model.md"
	subjectsDir  = "docs/security-model"
	registerName = "not-protected.md"
)

// The statuses a row can have, in the order the index counts them.
var statuses = []string{"Enforced", "Built", "Held by review", "By decision", "Not built", "Pending rebuild"}

// A subject is one file under docs/security-model/: an opening, a table of
// rows, and an Evidence entry per row.
type subject struct {
	path    string // from the repository's root
	title   string
	opening string // the prose between the title and the table
	rows    []row
	// evidence holds each "### <ID>" entry's text, and order the ids as
	// written, so a row with no entry and an entry with no row are both seen.
	evidence map[string]string
	order    []string
}

// A row is one line of a subject's table.
type row struct {
	id         string
	protection string
	status     string // one of statuses, or "" when the cell names none
	statusCell string
	residual   string
}

var (
	rowRE      = regexp.MustCompile(`^\| ([A-Z]+-[0-9]+) \| (.*) \| (.*) \| (.*) \|$`)
	evidenceRE = regexp.MustCompile(`^### ([A-Z]+-[0-9]+)$`)
	backtickRE = regexp.MustCompile("`([^`]+)`")
	linkRE     = regexp.MustCompile(`\]\(([^)#\s]+)(#[^)]*)?\)`)
	// A test bullet opens with the tests it cites: "- `TestA`, `TestB` — …".
	testBulletRE = regexp.MustCompile("(?m)^- (`[^`]+`(?:, `[^`]+`)*)(?: —|$)")
	whereRE      = regexp.MustCompile(`(?m)^- \*\*Where:\*\* (.*)$`)
)

// loadSubjects reads every subject file, in name order. The register is
// derived, so it is not one.
func (t tool) loadSubjects() ([]subject, error) {
	names, err := filepath.Glob(filepath.Join(t.root, subjectsDir, "*.md"))
	if err != nil { // coverage:ignore the pattern is constant
		return nil, err
	}
	sort.Strings(names)
	var subjects []subject
	for _, name := range names {
		if filepath.Base(name) == registerName {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil { // coverage:ignore only an unreadable checkout fails here
			return nil, err
		}
		subjects = append(subjects, parseSubject(subjectsDir+"/"+filepath.Base(name), string(data)))
	}
	if len(subjects) == 0 {
		return nil, fmt.Errorf("no subject files in %s", subjectsDir)
	}
	return subjects, nil
}

func parseSubject(path, text string) subject {
	s := subject{path: path, evidence: map[string]string{}}
	var opening []string
	section, entry := "", ""
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "# ") && s.title == "":
			s.title = strings.TrimPrefix(line, "# ")
			continue
		case strings.HasPrefix(line, "## "):
			section, entry = strings.TrimPrefix(line, "## "), ""
			continue
		}
		switch section {
		case "":
			// The link back to the model is navigation, not the opening.
			if !strings.HasPrefix(line, "[←") {
				opening = append(opening, line)
			}
		case "Protections":
			if m := rowRE.FindStringSubmatch(line); m != nil {
				s.rows = append(s.rows, row{id: m[1], protection: m[2], status: statusOf(m[3]), statusCell: m[3], residual: m[4]})
			}
		case "Evidence":
			if m := evidenceRE.FindStringSubmatch(line); m != nil {
				entry = m[1]
				s.order = append(s.order, entry)
				continue
			}
			if entry != "" {
				s.evidence[entry] += line + "\n"
			}
		}
	}
	s.opening = strings.TrimSpace(strings.Join(opening, "\n"))
	return s
}

// statusOf is the status a cell opens with, in bold; a qualifier may follow it.
func statusOf(cell string) string {
	for _, st := range statuses {
		if strings.HasPrefix(cell, "**"+st+"**") {
			return st
		}
	}
	return ""
}

// citedTests is the tests an Evidence entry's bullets open with.
func citedTests(entry string) []string {
	var tests []string
	for _, m := range testBulletRE.FindAllStringSubmatch(entry, -1) {
		for _, t := range backtickRE.FindAllStringSubmatch(m[1], -1) {
			tests = append(tests, t[1])
		}
	}
	return tests
}

// wherePaths is the paths on an Evidence entry's Where line, and whether it
// has the line.
func wherePaths(entry string) ([]string, bool) {
	m := whereRE.FindStringSubmatch(entry)
	if m == nil {
		return nil, false
	}
	var paths []string
	for _, p := range backtickRE.FindAllStringSubmatch(m[1], -1) {
		paths = append(paths, p[1])
	}
	return paths, true
}

// links is every local link target in text, with no anchor.
func links(text string) []string {
	var targets []string
	for _, m := range linkRE.FindAllStringSubmatch(text, -1) {
		if !strings.Contains(m[1], "://") {
			targets = append(targets, m[1])
		}
	}
	return targets
}

// anchor is the fragment GitHub gives an "### <ID>" heading.
func anchor(id string) string { return strings.ToLower(id) }
