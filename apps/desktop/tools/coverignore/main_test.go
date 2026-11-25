package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/cover"
)

const source = `package demo

func f(err error) error {
	if err != nil { // coverage:ignore only a full disk fails here
		return err
	}
	if err == nil { // coverage:ignore
		return nil
	}
	return nil
}

func g() int { // coverage:ignore only the sandbox's child runs this
	if true {
		return 1
	}
	return 2
}

func h(err error) int {
	if err != nil {
		return 0
	}
	// coverage:ignore-start only the sandbox's child runs this
	println()
	return 3
	// coverage:ignore-stop
}
`

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunMergesTheProfilesAndDropsMarkedBlocks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "mod", "go.mod"), "module example.com/mod\n\ngo 1.26\n")
	writeFile(t, filepath.Join(dir, "mod", "demo", "demo.go"), source)
	// Two test binaries report the same blocks, and a second run adds a hit.
	writeFile(t, filepath.Join(dir, "a.out"), `mode: atomic
example.com/mod/demo/demo.go:3.28,4.16 1 1
example.com/mod/demo/demo.go:4.16,6.3 1 0
example.com/mod/demo/demo.go:7.16,9.3 1 0
example.com/mod/demo/demo.go:10.2,10.12 1 0
example.com/mod/demo/demo.go:3.28,4.16 1 0
example.com/mod/demo/demo.go:13.14,14.10 1 0
example.com/mod/demo/demo.go:14.10,16.3 1 0
example.com/mod/demo/demo.go:17.2,17.10 1 0
example.com/mod/demo/demo.go:20.24,21.16 1 1
example.com/mod/demo/demo.go:21.16,23.3 1 0
example.com/mod/demo/demo.go:25.2,26.10 1 0
`)
	writeFile(t, filepath.Join(dir, "b.out"), `mode: atomic
example.com/mod/demo/demo.go:10.2,10.12 1 2
`)

	out := filepath.Join(dir, "merged.out")
	if err := run(filepath.Join(dir, "mod"), out, []string{filepath.Join(dir, "a.out"), filepath.Join(dir, "b.out")}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// Line 4's marker takes its if; line 7's has no reason, so its block
	// stays; g's marker on the func line takes the function; h keeps what
	// precedes its range.
	want := `mode: atomic
example.com/mod/demo/demo.go:3.28,4.16 1 1
example.com/mod/demo/demo.go:7.16,9.3 1 0
example.com/mod/demo/demo.go:10.2,10.12 1 2
example.com/mod/demo/demo.go:20.24,21.16 1 1
example.com/mod/demo/demo.go:21.16,23.3 1 0
`
	if string(got) != want {
		t.Errorf("merged profile:\n%s\nwant:\n%s", got, want)
	}
}

const marked = `package demo

func F(err error) error {
	if err != nil { // coverage:ignore only a full disk fails here
		return err
	}
	return nil
}
`

// The cover tool's Go 1.26 starts the if body's block on the marked line, its
// Go 1.27 on the line below. Either way the marker drops it.
func TestRunDropsAMarkedIfBodyInEitherLayout(t *testing.T) {
	for _, tc := range []struct{ name, profile, want string }{
		{"go1.26", `mode: set
example.com/mod/demo/demo.go:3.25,4.16 1 1
example.com/mod/demo/demo.go:4.16,6.3 1 0
example.com/mod/demo/demo.go:7.2,7.12 1 1
`, `mode: set
example.com/mod/demo/demo.go:3.25,4.16 1 1
example.com/mod/demo/demo.go:7.2,7.12 1 1
`},
		{"go1.27", `mode: set
example.com/mod/demo/demo.go:4.2,4.16 1 1
example.com/mod/demo/demo.go:5.3,6.1 1 0
example.com/mod/demo/demo.go:7.2,7.12 1 1
`, `mode: set
example.com/mod/demo/demo.go:7.2,7.12 1 1
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "mod", "go.mod"), "module example.com/mod\n")
			writeFile(t, filepath.Join(dir, "mod", "demo", "demo.go"), marked)
			writeFile(t, filepath.Join(dir, "p.out"), tc.profile)

			out := filepath.Join(dir, "merged.out")
			if err := run(filepath.Join(dir, "mod"), out, []string{filepath.Join(dir, "p.out")}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Errorf("merged profile:\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

// measure runs src's tests with coverage under the toolchain running these
// tests, so a Go upgrade that moves the blocks fails here rather than in
// Codecov, and returns the profile coverignore makes of it.
func measure(t *testing.T, src, test string) []*cover.Profile {
	t.Helper()
	dir := t.TempDir()
	mod := filepath.Join(dir, "mod")
	writeFile(t, filepath.Join(mod, "go.mod"), "module example.com/mod\n")
	writeFile(t, filepath.Join(mod, "demo", "demo.go"), src)
	writeFile(t, filepath.Join(mod, "demo", "demo_test.go"), test)
	profile := filepath.Join(dir, "p.out")
	cmd := exec.Command("go", "test", "-coverprofile="+profile, "./...")
	cmd.Dir = mod
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}

	out := filepath.Join(dir, "merged.out")
	if err := run(mod, out, []string{profile}); err != nil {
		t.Fatal(err)
	}
	merged, err := cover.ParseProfiles(out)
	if err != nil {
		t.Fatal(err)
	}
	return merged
}

func TestRunDropsAMarkedIfBodyTheToolchainReports(t *testing.T) {
	merged := measure(t, marked, `package demo

import "testing"

func TestF(t *testing.T) { F(nil) }
`)
	for _, p := range merged {
		for _, b := range p.Blocks {
			// The function's entry block may end on the if line; it starts
			// above the marker, so it stays.
			if b.StartLine >= 4 && b.StartLine <= 6 {
				t.Errorf("block %d.%d,%d.%d of the marked if survived", b.StartLine, b.StartCol, b.EndLine, b.EndCol)
			}
		}
	}
}

const markedFunc = `package demo

func F(err error) error { // coverage:ignore only a child that writes no coverage runs this
	if err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		err = nil
	}
	return err
}

func G() int {
	return 1
}
`

func TestRunDropsEveryBlockOfAMarkedFuncTheToolchainReports(t *testing.T) {
	merged := measure(t, markedFunc, `package demo

import "testing"

func TestF(t *testing.T) { F(nil); G() }
`)
	kept := 0
	for _, p := range merged {
		for _, b := range p.Blocks {
			if b.StartLine >= 3 && b.StartLine <= 11 {
				t.Errorf("block %d.%d,%d.%d of the marked func survived", b.StartLine, b.StartCol, b.EndLine, b.EndCol)
			}
			kept++
		}
	}
	if kept == 0 {
		t.Error("G's block was dropped too")
	}
}

func TestRunRefusesAFileOutsideTheModule(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/mod\n")
	writeFile(t, filepath.Join(dir, "p.out"), "mode: set\nexample.com/other/x.go:1.1,2.2 1 1\n")

	err := run(dir, filepath.Join(dir, "merged.out"), []string{filepath.Join(dir, "p.out")})
	if err == nil || !strings.Contains(err.Error(), "outside module") {
		t.Fatalf("err = %v, want one naming the module", err)
	}
}

func TestModulePathNeedsAModuleLine(t *testing.T) {
	gomod := filepath.Join(t.TempDir(), "go.mod")
	writeFile(t, gomod, "go 1.26\n")

	if _, err := modulePath(gomod); err == nil {
		t.Fatal("want an error for a go.mod with no module line")
	}
}
