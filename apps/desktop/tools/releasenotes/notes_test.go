package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadNotes(t *testing.T) {
	n, err := readNotes(writeNotes(t, testNotes()))
	if err != nil {
		t.Fatal(err)
	}
	if n.Tag != "cli/v0.2.0" || len(n.UpgradeNotes) != 1 || n.UpgradeNotes[0].PRs[0] != 3 {
		t.Errorf("notes = %+v", n)
	}
}

// A misspelt field would drop its section unseen, so it fails instead.
func TestReadNotesRejectsAnUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.json")
	if err := os.WriteFile(path, []byte(`{"tag":"cli/v1.0.0","fixed":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readNotes(path); err == nil || !strings.Contains(err.Error(), `unknown field "fixed"`) {
		t.Errorf("err = %v", err)
	}
}
