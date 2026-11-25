package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rustSource = `pub fn tested() -> u32 {
    1
}

// coverage:ignore-start needs a real window system
#[tauri::command]
pub fn wiring(flag: bool) -> u32 {
    if flag { 2 } else { 3 }
}
// coverage:ignore-stop

// coverage:ignore-start
pub fn no_reason() -> u32 {
    4
}
// coverage:ignore-stop

pub fn untested() -> u32 {
    // coverage:ignore-start only a macOS runner in dark mode runs this
    5
`

func TestRunLcovDropsMarkedLinesAndRecountsTotals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "lib.rs")
	writeFile(t, src, rustSource)
	in := filepath.Join(dir, "rust.lcov")
	// Line 7's function and its branch sit inside the first range; line 13's
	// start has no reason, so its function stays; line 19's start never stops,
	// so it runs to the end of the file.
	writeFile(t, in, strings.ReplaceAll(`SF:SRC
FN:1,_RNvCs_3lib6tested
FN:7,_RNvCs_3lib6wiring
FN:13,_RNvCs_3lib9no_reason
FN:18,_RNvCs_3lib8untested
FNDA:4,_RNvCs_3lib6tested
FNDA:0,_RNvCs_3lib6wiring
FNDA:0,_RNvCs_3lib9no_reason
FNDA:0,_RNvCs_3lib8untested
FNF:4
FNH:1
BRDA:8,0,0,-
BRDA:8,0,1,-
BRF:2
BRH:0
DA:1,4
DA:2,4
DA:3,4
DA:6,0
DA:7,0
DA:8,0
DA:9,0
DA:13,0
DA:14,0
DA:15,0
DA:18,0
DA:20,0
DA:21,0
LF:13
LH:3
end_of_record
`, "SRC", src))

	out := filepath.Join(dir, "filtered.lcov")
	if err := runLcov(in, out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(`SF:SRC
FN:1,_RNvCs_3lib6tested
FN:13,_RNvCs_3lib9no_reason
FN:18,_RNvCs_3lib8untested
FNDA:4,_RNvCs_3lib6tested
FNDA:0,_RNvCs_3lib9no_reason
FNDA:0,_RNvCs_3lib8untested
FNF:3
FNH:1
BRF:0
BRH:0
DA:1,4
DA:2,4
DA:3,4
DA:13,0
DA:14,0
DA:15,0
DA:18,0
LF:7
LH:3
end_of_record
`, "SRC", src)
	if string(got) != want {
		t.Errorf("filtered report:\n%s\nwant:\n%s", got, want)
	}
}

func TestFilterLcovRecountsEachFileOnItsOwn(t *testing.T) {
	report := `SF:a.rs
FN:1,a
FNDA:1,a
FNF:1
FNH:1
BRDA:2,0,0,3
BRDA:2,0,1,0
BRF:2
BRH:1
DA:1,1
DA:2,1
LF:2
LH:2
end_of_record
SF:b.rs
FN:1,a
FNDA:0,a
FNF:1
FNH:0
DA:1,0
DA:2,0
LF:2
LH:0
end_of_record
`
	// b.rs exempts its first line, which drops its function; a.rs exempts
	// nothing, and its function of the same name stays.
	exempt := func(path string) (map[int]bool, error) {
		if path == "b.rs" {
			return map[int]bool{1: true}, nil
		}
		return nil, nil
	}
	var got strings.Builder
	if err := filterLcov(strings.NewReader(report), &got, exempt); err != nil {
		t.Fatal(err)
	}
	want := `SF:a.rs
FN:1,a
FNDA:1,a
FNF:1
FNH:1
BRDA:2,0,0,3
BRDA:2,0,1,0
BRF:2
BRH:1
DA:1,1
DA:2,1
LF:2
LH:2
end_of_record
SF:b.rs
FNF:0
FNH:0
DA:2,0
LF:1
LH:0
end_of_record
`
	if got.String() != want {
		t.Errorf("filtered report:\n%s\nwant:\n%s", got.String(), want)
	}
}

func TestRunLcovFailsOnAMissingSource(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "rust.lcov")
	writeFile(t, in, "SF:"+filepath.Join(dir, "gone.rs")+"\nend_of_record\n")
	if err := runLcov(in, filepath.Join(dir, "out.lcov")); err == nil {
		t.Error("runLcov succeeded on a report naming a missing file")
	}
}

func TestFilterLcovFailsOnAMalformedRecord(t *testing.T) {
	none := func(string) (map[int]bool, error) { return nil, nil }
	for _, record := range []string{"FN:x,a", "FNDA:x,a", "BRDA:x,0,0,1", "DA:x,1"} {
		var out strings.Builder
		if err := filterLcov(strings.NewReader("SF:a.rs\n"+record+"\n"), &out, none); err == nil {
			t.Errorf("%s: filterLcov succeeded", record)
		}
	}
}
