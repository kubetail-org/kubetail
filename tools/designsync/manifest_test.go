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
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBlobHashIsGits(t *testing.T) {
	tl := fixtureTool(t)
	out, err := exec.Command("git", "hash-object", filepath.Join(tl.root, "src/status-pill.tsx")).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := blobHash([]byte(pillSource)), strings.TrimSpace(string(out)); got != want {
		t.Errorf("blobHash = %s, git hash-object = %s", got, want)
	}
}

func TestLoadManifestRejectsAnUnknownField(t *testing.T) {
	tl := fixtureTool(t)
	editManifest(t, tl, `"version": 1,`, `"version": 1, "extra": true,`)
	_, _, err := tl.loadManifest()
	wantErr(t, err, `unknown field "extra"`)
}

func TestLoadManifestNeedsTheFile(t *testing.T) {
	_, _, err := tool{root: t.TempDir()}.loadManifest()
	wantErr(t, err, "components.json")
}

func TestReadRefusesAPathOutsideTheRoot(t *testing.T) {
	tl := fixtureTool(t)
	for _, path := range []string{"../secret", "/etc/hosts"} {
		_, err := tl.read(path)
		wantErr(t, err, "not a path inside the repository")
	}
}

func TestFormatRewritesTheManifest(t *testing.T) {
	tl := fixtureTool(t)
	editManifest(t, tl, `"version": 1,`, `"version":    1,`)
	if err := tl.formatCmd(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, tl, manifestPath); got != testManifest {
		t.Errorf("formatted manifest:\n%s", got)
	}
}

func TestFormatNeedsAManifest(t *testing.T) {
	wantErr(t, tool{root: t.TempDir()}.formatCmd(), "components.json")
}

// A name with < or & stays readable in the file.
func TestEncodeKeepsHTMLCharacters(t *testing.T) {
	m := manifest{Version: 1, Components: []component{{Notes: []string{"a <b> & c"}}}}
	if got := string(encode(m)); !strings.Contains(got, "a <b> & c") {
		t.Errorf("encode escaped the note:\n%s", got)
	}
}
