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
