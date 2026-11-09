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

package gomodules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadReadsEveryModuleAWorkspaceUses(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.work"), "go 1.26\n\nuse (\n\t./shared\n\t./shared/nested\n\t./cli\n)\n")
	writeFile(t, filepath.Join(dir, "shared", "go.mod"), "module example.com/m/shared\n")
	writeFile(t, filepath.Join(dir, "shared", "nested", "go.mod"), "module example.com/m/shared/nested\n")
	writeFile(t, filepath.Join(dir, "cli", "go.mod"), "module example.com/m/cli\n")

	mods, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"example.com/m/shared/a/a.go":      filepath.Join(dir, "shared", "a", "a.go"),
		"example.com/m/shared/nested/b.go": filepath.Join(dir, "shared", "nested", "b.go"),
		"example.com/m/cli/main.go":        filepath.Join(dir, "cli", "main.go"),
	} {
		got, err := mods.File(name)
		if err != nil || got != want {
			t.Errorf("File(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

func TestLoadReadsAModuleWithoutAWorkspace(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/mod\n")

	mods, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := mods.File("example.com/mod/x.go")
	if want := filepath.Join(dir, "x.go"); err != nil || got != want {
		t.Errorf("File = %q, %v; want %q", got, err, want)
	}
}

func TestFileRefusesAFileOutsideEveryModule(t *testing.T) {
	mods := Modules{"example.com/mod": "mod"}
	for _, name := range []string{"example.com/other/x.go", "example.com/modx/x.go"} {
		if _, err := mods.File(name); err == nil || !strings.Contains(err.Error(), "outside every module") {
			t.Errorf("File(%q) = %v, want an error naming it outside", name, err)
		}
	}
}

func TestLoadFails(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"no go.work or go.mod":       {},
		"a go.mod with no module":    {"go.mod": "go 1.26\n"},
		"a go.work that won't parse": {"go.work": "use (\n"},
		"a used module missing":      {"go.work": "go 1.26\n\nuse ./gone\n"},
	} {
		dir := t.TempDir()
		for file, body := range files {
			writeFile(t, filepath.Join(dir, file), body)
		}
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: Load succeeded", name)
		}
	}
}
