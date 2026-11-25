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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testSubject is a subject file with one row of each kind the checks tell
// apart: enforced with a residual, accepted, not built, and pending rebuild.
const testSubject = `# Folders

[← Security model](../security-model.md)

A subfolder must be a real folder.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| FOLD-1 | A linked subfolder fails the start. | **Enforced** on Unix | A process of the user's own can still link one. |
| FOLD-2 | The cache is not encrypted. | **By decision** | — |
| FOLD-3 | A retention policy. | **Not built** | — |
| FOLD-4 | Discovery lists no remote model. | **Pending rebuild**, was Enforced | — |

## Evidence

### FOLD-1

- **Where:** ` + "`sidecar/app/{subfolders,layout}.go`, `src-tauri/src`" + `
- **Records:** [resolved once](../security/2026-11-24-resolved-once.md)
- ` + "`TestALinkedSubfolderFailsTheStart`, `a_linked_dir_is_refused`" + ` — each subfolder.
- ` + "`refuses a linked folder`" + ` — the form.
- ` + "`subfolders_test.go`" + ` — the table of cases.
- Note: made through ` + "`os.Root`" + `.

### FOLD-2

- **Records:** [ADR](../adr/2026-09-02-cache.md)

### FOLD-3

- **Records:** [TODO](../TODO.md#security)

### FOLD-4

- ` + "`TestGoneWithTheRewrite`" + ` — what the rebuild must pin.
`

const testModel = `# Security model

[Folders](security-model/folders.md) and [what is not protected](security-model/not-protected.md).

` + indexStart + `
` + indexStop + `
`

// testFiles is a checkout the test subject describes truthfully.
var testFiles = map[string]string{
	modelPath:                   testModel,
	subjectsDir + "/folders.md": testSubject,
	"docs/security/2026-11-24-resolved-once.md": "# Record\n",
	"docs/adr/2026-09-02-cache.md":              "# ADR\n",
	"docs/TODO.md":                              "# TODO\n",
	"sidecar/app/subfolders.go":                 "package app\n",
	"sidecar/app/layout.go":                     "package app\n",
	"sidecar/app/subfolders_test.go":            "package app\n\nfunc TestALinkedSubfolderFailsTheStart(t *testing.T) {}\n",
	"src-tauri/src/paths.rs":                    "#[test]\nfn a_linked_dir_is_refused() {}\n",
	"src/form.test.tsx":                         "it('refuses a linked folder', () => {})\n",
	// A dependency's files are not the repository's.
	"node_modules/dep/dep_test.go": "package dep\n\nfunc TestInADependency(t *testing.T) {}\n",
}

// checkout writes testFiles, with each edit applied to the file it names, and
// brings the index and register up to date.
func checkout(t *testing.T, edits map[string]func(string) string) tool {
	t.Helper()
	tl := tool{root: t.TempDir()}
	for name, text := range testFiles {
		if edit := edits[name]; edit != nil {
			text = edit(text)
		}
		writeFile(t, tl.root, name, text)
	}
	if err := tl.writeCmd(); err != nil {
		t.Fatal(err)
	}
	return tl
}

func writeFile(t *testing.T, root, name, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func replace(old, new string) func(string) string {
	return func(s string) string { return strings.Replace(s, old, new, 1) }
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("err = %v, want one holding %q", err, substr)
	}
}
