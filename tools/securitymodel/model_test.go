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
	"reflect"
	"testing"
)

func TestParseSubjectReadsTheRowsAndTheirEvidence(t *testing.T) {
	s := parseSubject("docs/security-model/folders.md", testSubject)

	if s.title != "Folders" || s.opening != "A subfolder must be a real folder." {
		t.Errorf("title %q, opening %q", s.title, s.opening)
	}
	want := row{
		id:         "FOLD-1",
		protection: "A linked subfolder fails the start.",
		status:     "Enforced",
		statusCell: "**Enforced** on Unix",
		residual:   "A process of the user's own can still link one.",
	}
	if len(s.rows) != 4 || s.rows[0] != want {
		t.Fatalf("rows = %+v", s.rows)
	}
	if got := []string{s.rows[1].status, s.rows[2].status, s.rows[3].status}; !reflect.DeepEqual(got, []string{"By decision", "Not built", "Pending rebuild"}) {
		t.Errorf("statuses = %v", got)
	}
	if !reflect.DeepEqual(s.order, []string{"FOLD-1", "FOLD-2", "FOLD-3", "FOLD-4"}) {
		t.Errorf("order = %v", s.order)
	}

	entry := s.evidence["FOLD-1"]
	wantTests := []string{"TestALinkedSubfolderFailsTheStart", "a_linked_dir_is_refused", "refuses a linked folder", "subfolders_test.go"}
	if got := citedTests(entry); !reflect.DeepEqual(got, wantTests) {
		t.Errorf("tests = %v, want %v", got, wantTests)
	}
	paths, ok := wherePaths(entry)
	if !ok || !reflect.DeepEqual(paths, []string{"sidecar/app/{subfolders,layout}.go", "src-tauri/src"}) {
		t.Errorf("where = %v, %v", paths, ok)
	}
	if got := links(entry); !reflect.DeepEqual(got, []string{"../security/2026-11-24-resolved-once.md"}) {
		t.Errorf("links = %v", got)
	}
	if _, ok := wherePaths(s.evidence["FOLD-2"]); ok {
		t.Error("FOLD-2 has no Where line")
	}
}

func TestStatusOfNeedsABoldStatusFirst(t *testing.T) {
	for cell, want := range map[string]string{
		"**Enforced**":                      "Enforced",
		"**Pending rebuild**, was Enforced": "Pending rebuild",
		"**Built**, not yet checked":        "Built",
		"**Shipped**":                       "",
		"Enforced":                          "",
		"on Unix, **Enforced**":             "",
	} {
		if got := statusOf(cell); got != want {
			t.Errorf("statusOf(%q) = %q, want %q", cell, got, want)
		}
	}
}

func TestLinksLeavesOutTheWebAndAnchors(t *testing.T) {
	got := links("[a](../TODO.md#security), [b](https://example.com/x), [c](x.md)")
	if !reflect.DeepEqual(got, []string{"../TODO.md", "x.md"}) {
		t.Errorf("links = %v", got)
	}
}
