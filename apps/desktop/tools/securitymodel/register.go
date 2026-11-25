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
	"os"
	"path"
	"path/filepath"
	"strings"
)

// The model's index sits between these two lines.
const (
	indexStart = "<!-- index:start, written by tools/securitymodel -->"
	indexStop  = "<!-- index:stop -->"
)

func (t tool) writeCmd() error {
	subjects, err := t.loadSubjects()
	if err != nil {
		return err
	}
	name := filepath.Join(t.root, modelPath)
	model, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if !bytes.Contains(model, []byte(indexStart)) || !bytes.Contains(model, []byte(indexStop)) {
		return fmt.Errorf("%s has no index markers", modelPath)
	}
	if err := os.WriteFile(name, writeIndex(model, subjects), 0o644); err != nil { // coverage:ignore only an unwritable checkout fails here
		return err
	}
	return os.WriteFile(filepath.Join(t.root, subjectsDir, registerName), renderRegister(subjects), 0o644)
}

// writeIndex is the model with its index written from the subjects. A model
// with no markers is returned as it is.
func writeIndex(model []byte, subjects []subject) []byte {
	start := bytes.Index(model, []byte(indexStart))
	stop := bytes.Index(model, []byte(indexStop))
	if start < 0 || stop < start {
		return model
	}
	var out bytes.Buffer
	out.Write(model[:start+len(indexStart)])
	out.WriteString("\n" + renderIndex(subjects))
	out.Write(model[stop:])
	return out.Bytes()
}

// renderIndex is a table of the subjects, each with its rows counted by
// status, and their total.
func renderIndex(subjects []subject) string {
	var b strings.Builder
	b.WriteString("| Subject | " + strings.Join(statuses, " | ") + " |\n")
	b.WriteString("| ---" + strings.Repeat(" | ---:", len(statuses)) + " |\n")
	total := map[string]int{}
	for _, s := range subjects {
		counts := map[string]int{}
		for _, r := range s.rows {
			counts[r.status]++
			total[r.status]++
		}
		fmt.Fprintf(&b, "| [%s](security-model/%s) |%s\n", s.title, path.Base(s.path), countCells(counts))
	}
	fmt.Fprintf(&b, "| **All** |%s\n", countCells(total))
	return b.String()
}

func countCells(counts map[string]int) string {
	var b strings.Builder
	for _, st := range statuses {
		fmt.Fprintf(&b, " %d |", counts[st])
	}
	return b.String()
}

// renderRegister is not-protected.md: every row that is not Enforced and
// every residual, by subject, each linking its Evidence entry.
func renderRegister(subjects []subject) []byte {
	var b strings.Builder
	b.WriteString("# What is not protected\n\n")
	b.WriteString("[← Security model](../security-model.md)\n\n")
	b.WriteString("Every row of the model that no test enforces, and every residual an enforced row states.\n")
	b.WriteString("`tools/securitymodel` writes this file from the subject files: edit a row there, then run\n")
	b.WriteString("`go run ./securitymodel write` from `tools/`.\n")
	for _, s := range subjects {
		var lines []string
		for _, r := range s.rows {
			link := fmt.Sprintf("[%s](%s#%s)", r.id, path.Base(s.path), anchor(r.id))
			switch {
			case r.status != "Enforced":
				line := fmt.Sprintf("- %s, %s. %s", link, strings.ReplaceAll(r.statusCell, "**", ""), r.protection)
				if hasResidual(r) {
					line += " Residual: " + r.residual
				}
				lines = append(lines, line)
			case hasResidual(r):
				lines = append(lines, fmt.Sprintf("- %s, residual. %s", link, r.residual))
			}
		}
		if len(lines) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s\n", s.title, strings.Join(lines, "\n"))
	}
	return []byte(b.String())
}

func hasResidual(r row) bool { return r.residual != "" && r.residual != "—" }
