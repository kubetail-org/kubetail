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

func TestWriteFillsTheIndexAndTheRegister(t *testing.T) {
	tl := checkout(t, nil)

	model, err := os.ReadFile(filepath.Join(tl.root, modelPath))
	if err != nil {
		t.Fatal(err)
	}
	wantIndex := indexStart + `
| Subject | Enforced | Built | Held by review | By decision | Not built | Pending rebuild |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| [Folders](security-model/folders.md) | 1 | 0 | 0 | 1 | 1 | 1 |
| **All** | 1 | 0 | 0 | 1 | 1 | 1 |
` + indexStop
	if !strings.Contains(string(model), wantIndex) {
		t.Errorf("model:\n%s", model)
	}

	register, err := os.ReadFile(filepath.Join(tl.root, subjectsDir, registerName))
	if err != nil {
		t.Fatal(err)
	}
	wantRows := `## Folders

- [FOLD-1](folders.md#fold-1), residual. A process of the user's own can still link one.
- [FOLD-2](folders.md#fold-2), By decision. The cache is not encrypted.
- [FOLD-3](folders.md#fold-3), Not built. A retention policy.
- [FOLD-4](folders.md#fold-4), Pending rebuild, was Enforced. Discovery lists no remote model.
`
	if !strings.HasSuffix(string(register), wantRows) {
		t.Errorf("register:\n%s", register)
	}
}

// A row that is not enforced keeps its residual beside it, and a subject with
// nothing open is left out.
func TestRegisterKeepsAnOpenRowsResidual(t *testing.T) {
	open := subject{title: "Open", path: "docs/security-model/open.md", rows: []row{
		{id: "OPEN-1", protection: "A link is followed.", status: "By decision", statusCell: "**By decision**", residual: "It can be repointed."},
	}}
	shut := subject{title: "Shut", path: "docs/security-model/shut.md", rows: []row{
		{id: "SHUT-1", protection: "Nothing is open.", status: "Enforced", statusCell: "**Enforced**", residual: "—"},
	}}
	got := string(renderRegister([]subject{open, shut}))
	if !strings.Contains(got, "- [OPEN-1](open.md#open-1), By decision. A link is followed. Residual: It can be repointed.\n") {
		t.Errorf("register:\n%s", got)
	}
	if strings.Contains(got, "Shut") {
		t.Errorf("a subject with nothing open is listed:\n%s", got)
	}
}

// A model with no markers is checked as it is.
func TestWriteIndexLeavesAModelWithNoMarkers(t *testing.T) {
	model := []byte("# Security model\n")
	if got := writeIndex(model, nil); string(got) != string(model) {
		t.Errorf("writeIndex = %q", got)
	}
}

func TestWriteNeedsTheIndexMarkers(t *testing.T) {
	tl := checkout(t, nil)
	writeFile(t, tl.root, modelPath, "# Security model\n")
	wantErr(t, tl.writeCmd(), "has no index markers")

	wantErr(t, tool{root: t.TempDir()}.writeCmd(), "no subject files")

	tl = checkout(t, nil)
	if err := os.Remove(filepath.Join(tl.root, modelPath)); err != nil {
		t.Fatal(err)
	}
	wantErr(t, tl.writeCmd(), "security-model.md")
}
