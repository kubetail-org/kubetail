// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReportsEachProblem(t *testing.T) {
	folders := subjectsDir + "/folders.md"
	long := strings.Repeat("é", cellLimit+1)
	for _, tc := range []struct {
		name, file, old, new string
		problem              string
	}{
		{"no title", folders, "# Folders\n", "", "folders.md: no title"},
		{"no opening", folders, "A subfolder must be a real folder.\n", "", "folders.md: no opening paragraph"},
		{"unknown status", folders, "**By decision** |", "**Shipped** |", `FOLD-2: status "**Shipped**" does not open with one of the statuses`},
		{"long protection", folders, "The cache is not encrypted.", long, "FOLD-2: the protection is 601 characters, over 600"},
		{"long residual", folders, "| **By decision** | — |", "| **By decision** | " + long + " |", "FOLD-2: the residual is 601 characters, over 600"},
		{"id twice", folders, "| FOLD-2 |", "| FOLD-1 |", "FOLD-1: id is also used in docs/security-model/folders.md"},
		{"row without evidence", folders, "### FOLD-3", "### FOLD-9", "FOLD-3: no entry under ## Evidence"},
		{"evidence without row", folders, "### FOLD-3", "### FOLD-9", "FOLD-9: an Evidence entry with no row"},
		{"missing path", folders, "`src-tauri/src`", "`src-tauri/gone`", "FOLD-1: Where names `src-tauri/gone`"},
		{"missing Go test", folders, "TestALinkedSubfolderFailsTheStart", "TestRenamedSince", "FOLD-1: cites the test `TestRenamedSince`"},
		{"missing Rust test", folders, "a_linked_dir_is_refused", "a_renamed_test", "FOLD-1: cites the test `a_renamed_test`"},
		{"enforced without a test", folders, "| **Not built** |", "| **Enforced** |", "FOLD-3: Enforced, and no bullet cites a test"},
		{"enforced without Where", folders, "| **Not built** |", "| **Enforced** |", "FOLD-3: Enforced, and no Where line"},
		{"held by review without Where", folders, "| **Not built** |", "| **Held by review** |", "FOLD-3: Held by review, and no Where line"},
		{"decision without its record", folders, "(../adr/2026-09-02-cache.md)", "(../TODO.md)", "FOLD-2: By decision, and no link to the ADR or record"},
		{"not built without its work", folders, "(../TODO.md#security)", "(../adr/2026-09-02-cache.md)", "FOLD-3: Not built, and no link to its work"},
		{"record nothing links", folders, "(../security/2026-11-24-resolved-once.md)", "(../adr/2026-09-02-cache.md)", "docs/security/2026-11-24-resolved-once.md: no row or section of the model links this record"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl := checkout(t, map[string]func(string) string{tc.file: replace(tc.old, tc.new)})
			var out strings.Builder
			wantErr(t, tl.checkCmd(&out), "problems in the security model")
			if !strings.Contains(out.String(), tc.problem) {
				t.Errorf("problems do not hold %q:\n%s", tc.problem, out.String())
			}
		})
	}
}

// A row pending rebuild cites tests that are gone; one with no rows at all is
// a problem of its own.
func TestCheckPassesATestARebuildOwes(t *testing.T) {
	tl := checkout(t, nil)
	var out strings.Builder
	if err := tl.checkCmd(&out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	tl = checkout(t, map[string]func(string) string{
		subjectsDir + "/folders.md": func(string) string { return "# Folders\n\nNothing yet.\n" },
	})
	out.Reset()
	wantErr(t, tl.checkCmd(&out), "problems in the security model")
	if !strings.Contains(out.String(), "no rows under ## Protections") {
		t.Errorf("problems:\n%s", out.String())
	}
}

func TestCheckFailsOnAStaleIndexOrRegister(t *testing.T) {
	for _, name := range []string{modelPath, subjectsDir + "/" + registerName} {
		tl := checkout(t, nil)
		path := filepath.Join(tl.root, filepath.FromSlash(name))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, tl.root, name, strings.Replace(string(data), "| 1 |", "| 7 |", 1)+"\nedited by hand\n")
		var out strings.Builder
		wantErr(t, tl.checkCmd(&out), "1 problems")
		if !strings.Contains(out.String(), "run `go run ./securitymodel write`") {
			t.Errorf("%s: problems:\n%s", name, out.String())
		}
	}
}

func TestCheckNeedsTheModelsFiles(t *testing.T) {
	wantErr(t, tool{root: t.TempDir()}.checkCmd(io.Discard), "no subject files")

	tl := checkout(t, nil)
	if err := os.Remove(filepath.Join(tl.root, modelPath)); err != nil {
		t.Fatal(err)
	}
	wantErr(t, tl.checkCmd(io.Discard), "security-model.md")
}
