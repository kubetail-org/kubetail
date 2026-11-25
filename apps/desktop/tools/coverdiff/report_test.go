package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadGoMapsImportPathsToFilesAndMarksSharedLinesPartial(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sidecar", "go.mod"), "module example.com/side\n\ngo 1.26\n")
	profile := filepath.Join(root, "cover.out")
	writeFile(t, profile, `mode: atomic
example.com/side/demo/demo.go:3.28,4.16 1 1
example.com/side/demo/demo.go:4.16,6.3 1 0
`)
	cov := coverage{}
	if err := readGo(cov, root, "sidecar", profile); err != nil {
		t.Fatal(err)
	}
	want := coverage{"sidecar/demo/demo.go": {
		3: {hit: true},
		4: {hit: true, missed: true},
		5: {missed: true},
		6: {missed: true},
	}}
	if !reflect.DeepEqual(cov, want) {
		t.Errorf("readGo = %v, want %v", cov, want)
	}
}

func TestReadGoRefusesAFileOutsideTheModule(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "sidecar", "go.mod"), "module example.com/side\n")
	profile := filepath.Join(root, "cover.out")
	writeFile(t, profile, "mode: set\nexample.com/other/a.go:1.1,2.2 1 1\n")
	if err := readGo(coverage{}, root, "sidecar", profile); err == nil {
		t.Error("readGo succeeded")
	}
}

func TestReadGoNeedsAModuleAndAProfile(t *testing.T) {
	root := t.TempDir()
	profile := filepath.Join(root, "cover.out")
	writeFile(t, profile, "mode: set\n")
	if err := readGo(coverage{}, root, "sidecar", profile); err == nil {
		t.Error("readGo succeeded with no go.mod")
	}
	writeFile(t, filepath.Join(root, "sidecar", "go.mod"), "go 1.26\n")
	if err := readGo(coverage{}, root, "sidecar", profile); err == nil {
		t.Error("readGo succeeded with no module line")
	}
	writeFile(t, filepath.Join(root, "sidecar", "go.mod"), "module example.com/side\n")
	if err := readGo(coverage{}, root, "sidecar", filepath.Join(root, "missing.out")); err == nil {
		t.Error("readGo succeeded with no profile")
	}
}

func TestReadLcovReadsLinesAndUntakenBranches(t *testing.T) {
	root := t.TempDir()
	report := filepath.Join(root, "lcov.info")
	abs := filepath.Join(root, "src-tauri", "src", "lib.rs")
	writeFile(t, report, `TN:
SF:src/a.ts
FN:1,f
FNDA:1,f
DA:1,1
DA:2,0
DA:3,4
BRDA:3,0,0,2
BRDA:3,0,1,0
DA:4,1
BRDA:4,0,0,1
BRDA:5,0,0,-
LF:4
LH:3
end_of_record
SF:`+abs+`
DA:7,0,abc
end_of_record
`)
	cov := coverage{}
	if err := readLcov(cov, root, report); err != nil {
		t.Fatal(err)
	}
	want := coverage{
		"src/a.ts": {
			1: {hit: true},
			2: {missed: true},
			3: {hit: true, missed: true},
			4: {hit: true},
			5: {missed: true},
		},
		"src-tauri/src/lib.rs": {7: {missed: true}},
	}
	if !reflect.DeepEqual(cov, want) {
		t.Errorf("readLcov = %v, want %v", cov, want)
	}
}

func TestReadLcovRefusesABadRecord(t *testing.T) {
	for _, body := range []string{
		"SF:a.ts\nDA:x,1\n",
		"SF:a.ts\nBRDA:x,0,0,1\n",
		"SF:/elsewhere/a.ts\n",
	} {
		report := filepath.Join(t.TempDir(), "lcov.info")
		writeFile(t, report, body)
		if err := readLcov(coverage{}, filepath.Join(t.TempDir(), "root"), report); err == nil {
			t.Errorf("%q: readLcov succeeded", body)
		}
	}
}

func TestReadLcovNeedsTheReport(t *testing.T) {
	if err := readLcov(coverage{}, t.TempDir(), filepath.Join(t.TempDir(), "missing.info")); err == nil {
		t.Error("readLcov succeeded")
	}
}
