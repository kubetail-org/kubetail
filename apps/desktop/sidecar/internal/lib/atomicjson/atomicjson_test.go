package atomicjson_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/atomicjson"
)

type doc struct {
	A string `json:"a"`
	N int    `json:"n"`
}

// A missing file is the "nothing written yet" state: zero value, no error.
// Both store packages depend on this so a first Load before any Save works.
func TestLoadMissingFileReturnsZero(t *testing.T) {
	got, err := atomicjson.Load[doc](filepath.Join(t.TempDir(), "x.json"))
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if got != (doc{}) {
		t.Fatalf("want zero doc, got %+v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	want := doc{A: "hello", N: 7}
	if err := atomicjson.Save(p, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := atomicjson.Load[doc](p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip: want %+v, got %+v", want, got)
	}
}

// SaveIndent writes a file people edit by hand: two-space indents, a trailing newline.
func TestSaveIndentIndentsAndEndsWithANewline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	if err := atomicjson.SaveIndent(p, doc{A: "hello", N: 7}); err != nil {
		t.Fatalf("SaveIndent: %v", err)
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": \"hello\",\n  \"n\": 7\n}\n"
	if string(got) != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

// A second Save fully replaces the document (atomic rename, not append).
func TestSaveOverwrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	if err := atomicjson.Save(p, doc{A: "first"}); err != nil {
		t.Fatal(err)
	}
	if err := atomicjson.Save(p, doc{A: "second"}); err != nil {
		t.Fatal(err)
	}
	got, _ := atomicjson.Load[doc](p)
	if got.A != "second" {
		t.Fatalf("want second, got %+v", got)
	}
}

// Corrupt content surfaces as an error with the zero value — callers must
// not silently treat garbage as valid state.
func TestLoadInvalidJSONReturnsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(p, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := atomicjson.Load[doc](p)
	if err == nil {
		t.Fatal("want unmarshal error, got nil")
	}
	if got != (doc{}) {
		t.Fatalf("want zero doc on error, got %+v", got)
	}
}

// A successful Save leaves no temp file behind: the write goes to a temp
// file in the same dir and is atomically renamed into place.
func TestSaveLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	if err := atomicjson.Save(p, doc{A: "v"}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "x.json" {
			t.Fatalf("unexpected leftover file: %q", e.Name())
		}
	}
}

// A caller passes the path it wants, not a path it has prepared: the parent
// directory is created if absent. The modes are pinned in atomicjson_unix_test.go.
func TestSaveCreatesMissingDirs(t *testing.T) {
	p := filepath.Join(t.TempDir(), "nested", "deeper", "x.json")
	if err := atomicjson.Save(p, doc{A: "v"}); err != nil {
		t.Fatalf("Save into missing dirs: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailsWhenTheParentIsAFile(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicjson.Save(filepath.Join(parent, "x.json"), doc{A: "v"}); err == nil {
		t.Fatal("Save under a file returned no error")
	}
}

// A read that fails for anything other than "missing" is an error, not a zero
// value: only absence is a legitimate empty document.
func TestLoadUnreadablePathReturnsError(t *testing.T) {
	dir := t.TempDir() // a directory is readable-as-a-file nowhere
	if _, err := atomicjson.Load[doc](dir); err == nil {
		t.Fatal("Load on a directory returned no error")
	}
}

// A value JSON cannot represent fails before anything is written, so the
// document on disk is untouched.
func TestSaveRejectsUnmarshalableValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.json")
	if err := atomicjson.Save(path, make(chan int)); err == nil {
		t.Fatal("Save of a channel returned no error")
	}
	if err := atomicjson.SaveIndent(path, make(chan int)); err == nil {
		t.Fatal("SaveIndent of a channel returned no error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("Save wrote %s despite failing to marshal", path)
	}
}
