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
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// cellLimit is the most characters a row's protection or residual may hold:
// a row states one property, and its explanation goes under Evidence.
const cellLimit = 600

func (t tool) checkCmd(out io.Writer) error {
	subjects, err := t.loadSubjects()
	if err != nil {
		return err
	}
	code, err := readTree(t.root)
	if err != nil { // coverage:ignore loadSubjects has read the root
		return err
	}
	model, err := os.ReadFile(filepath.Join(t.root, modelPath))
	if err != nil {
		return err
	}
	register, err := os.ReadFile(filepath.Join(t.root, subjectsDir, registerName))
	if err != nil && !os.IsNotExist(err) { // coverage:ignore only an unreadable checkout fails here
		return err
	}

	problems := check(subjects, code)
	problems = append(problems, checkRecords(string(model), subjects, code)...)
	if !bytes.Equal(model, writeIndex(model, subjects)) || !bytes.Equal(register, renderRegister(subjects)) {
		problems = append(problems, "the index or the register is out of date: run `go run ./securitymodel write` from tools/")
	}
	for _, p := range problems {
		fmt.Fprintln(out, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%d problems in the security model", len(problems))
	}
	rows := 0
	for _, s := range subjects {
		rows += len(s.rows)
	}
	fmt.Fprintf(out, "%s: %d rows in %d subjects pass\n", modelPath, rows, len(subjects))
	return nil
}

// check lists what is wrong with the subjects' rows: a malformed row, a row
// without what its status needs, and a test or file that is not in the tree.
func check(subjects []subject, code tree) []string {
	var problems []string
	ids := map[string]string{}
	for _, s := range subjects {
		add := func(format string, a ...any) {
			problems = append(problems, s.path+": "+fmt.Sprintf(format, a...))
		}
		if s.title == "" {
			add("no title")
		}
		if s.opening == "" {
			add("no opening paragraph before the table")
		}
		if len(s.rows) == 0 {
			add("no rows under ## Protections")
		}
		rows := map[string]bool{}
		for _, r := range s.rows {
			if other, ok := ids[r.id]; ok {
				add("%s: id is also used in %s", r.id, other)
			}
			ids[r.id] = s.path
			rows[r.id] = true

			if r.status == "" {
				add("%s: status %q does not open with one of the statuses, in bold", r.id, r.statusCell)
			}
			if n := len([]rune(r.protection)); n > cellLimit {
				add("%s: the protection is %d characters, over %d", r.id, n, cellLimit)
			}
			if n := len([]rune(r.residual)); n > cellLimit {
				add("%s: the residual is %d characters, over %d", r.id, n, cellLimit)
			}
			entry, ok := s.evidence[r.id]
			if !ok {
				add("%s: no entry under ## Evidence", r.id)
				continue
			}
			for _, p := range checkEvidence(r, entry, code) {
				add("%s: %s", r.id, p)
			}
		}
		for _, id := range s.order {
			if !rows[id] {
				add("%s: an Evidence entry with no row", id)
			}
		}
	}
	return problems
}

// checkEvidence lists what a row's Evidence entry lacks for its status, and
// what it cites that is not in the tree.
func checkEvidence(r row, entry string, code tree) []string {
	var problems []string
	add := func(format string, a ...any) { problems = append(problems, fmt.Sprintf(format, a...)) }

	paths, hasWhere := wherePaths(entry)
	for _, p := range paths {
		if !code.hasPath(p) {
			add("Where names `%s`, which is no file or folder from the root", p)
		}
	}
	tests := citedTests(entry)
	// A row pending rebuild cites the tests the rebuild must bring back.
	if r.status != "Pending rebuild" {
		for _, name := range tests {
			if !code.hasTest(name) {
				add("cites the test `%s`, which is not in the tree", name)
			}
		}
	}

	targets := links(entry)
	linksTo := func(prefixes ...string) bool {
		for _, target := range targets {
			for _, prefix := range prefixes {
				if strings.HasPrefix(target, prefix) {
					return true
				}
			}
		}
		return false
	}
	switch r.status {
	case "Enforced":
		if len(tests) == 0 {
			add("Enforced, and no bullet cites a test")
		}
		if !hasWhere {
			add("Enforced, and no Where line")
		}
	case "Built", "Held by review":
		if !hasWhere {
			add("%s, and no Where line", r.status)
		}
	case "By decision":
		// The repo's own ADRs hold the decisions the desktop shares with the
		// other components.
		if !linksTo("../adr/", "../../../../docs/adr/", "../security/") {
			add("By decision, and no link to the ADR or record that decided it")
		}
	case "Not built":
		if !linksTo("../TODO.md") {
			add("Not built, and no link to its work in ../TODO.md")
		}
	}
	return problems
}

// checkRecords lists the dated records under docs/security/ that neither the
// model nor a subject links.
func checkRecords(model string, subjects []subject, code tree) []string {
	linked := map[string]bool{}
	note := func(from, text string) {
		for _, target := range links(text) {
			linked[path.Join(path.Dir(from), target)] = true
		}
	}
	note(modelPath, model)
	for _, s := range subjects {
		note(s.path, s.opening)
		for _, entry := range s.evidence {
			note(s.path, entry)
		}
	}
	var problems []string
	for _, f := range code.files {
		if strings.HasPrefix(f, "docs/security/") && strings.HasSuffix(f, ".md") && !linked[f] {
			problems = append(problems, f+": no row or section of the model links this record")
		}
	}
	return problems
}
