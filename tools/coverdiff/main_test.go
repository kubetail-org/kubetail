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
	"errors"
	"fmt"
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

// fixture writes a repository with one Go and one TypeScript change, and
// reports that measured them; goCount is how often the Go change's first line ran.
func fixture(t *testing.T, goCount int) config {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "modules", "go.mod"), "module example.com/side\n")
	writeFile(t, filepath.Join(root, "changes.diff"), `--- a/modules/a.go
+++ b/modules/a.go
@@ -4,0 +5,2 @@
+	x()
+	y()
--- a/dashboard-ui/src/b.ts
+++ b/dashboard-ui/src/b.ts
@@ -1,0 +2,3 @@
+a
+b
+// a comment no report measures
`)
	writeFile(t, filepath.Join(root, "go.out"), fmt.Sprintf("mode: atomic\n"+
		"example.com/side/a.go:5.2,5.5 1 %d\n"+
		"example.com/side/a.go:6.2,6.5 1 1\n", goCount))
	writeFile(t, filepath.Join(root, "lcov.info"), "SF:dashboard-ui/src/b.ts\nDA:2,1\nDA:3,1\nBRDA:3,0,0,0\nend_of_record\n")
	return config{
		root:       root,
		diff:       filepath.Join(root, "changes.diff"),
		goProfiles: []goProfile{{dir: "modules", path: filepath.Join(root, "go.out")}},
		lcov:       []string{filepath.Join(root, "lcov.info")},
	}
}

func TestRunListsTheUncoveredChangedLines(t *testing.T) {
	var out strings.Builder
	err := run(fixture(t, 0), &out)
	if !errors.Is(err, errUncovered) {
		t.Fatalf("run = %v, want errUncovered", err)
	}
	want := "dashboard-ui/src/b.ts:3 partially covered\nmodules/a.go:5 not covered\n"
	if out.String() != want {
		t.Errorf("run printed\n%s\nwant\n%s", out.String(), want)
	}
}

func TestRunPassesWhenEveryMeasuredChangedLineIsCovered(t *testing.T) {
	cfg := fixture(t, 3)
	cfg.lcov = nil
	var out strings.Builder
	if err := run(cfg, &out); err != nil {
		t.Fatalf("run = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("run printed %q", out.String())
	}
}

func TestRunFailsOnAnUnreadableInput(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for name, edit := range map[string]func(*config){
		"diff": func(c *config) { c.diff = missing },
		"go":   func(c *config) { c.goProfiles[0].path = missing },
		"lcov": func(c *config) { c.lcov = []string{missing} },
	} {
		cfg := fixture(t, 1)
		edit(&cfg)
		err := run(cfg, &strings.Builder{})
		if err == nil || errors.Is(err, errUncovered) {
			t.Errorf("%s: run = %v, want a read error", name, err)
		}
	}
}

func TestRunReadsAProfilePerModule(t *testing.T) {
	cfg := fixture(t, 0)
	cfg.lcov = nil
	writeFile(t, filepath.Join(cfg.root, "cli", "go.mod"), "module example.com/cli\n")
	writeFile(t, filepath.Join(cfg.root, "cli.out"), "mode: atomic\nexample.com/cli/c.go:1.1,1.5 1 0\n")
	diff := "--- a/cli/c.go\n+++ b/cli/c.go\n@@ -0,0 +1 @@\n+\tz()\n"
	writeFile(t, filepath.Join(cfg.root, "cli.diff"), diff)
	cfg.diff = filepath.Join(cfg.root, "cli.diff")
	cfg.goProfiles = append(cfg.goProfiles, goProfile{dir: "cli", path: filepath.Join(cfg.root, "cli.out")})
	var out strings.Builder
	if err := run(cfg, &out); !errors.Is(err, errUncovered) {
		t.Fatalf("run = %v, want errUncovered", err)
	}
	if want := "cli/c.go:1 not covered\n"; out.String() != want {
		t.Errorf("run printed %q, want %q", out.String(), want)
	}
}

func TestParseGoProfile(t *testing.T) {
	p, err := parseGoProfile("cli=/tmp/cli.out")
	if err != nil || p != (goProfile{dir: "cli", path: "/tmp/cli.out"}) {
		t.Errorf("parseGoProfile = %+v, %v", p, err)
	}
	for _, v := range []string{"cli.out", "=cli.out", "cli="} {
		if _, err := parseGoProfile(v); err == nil {
			t.Errorf("parseGoProfile(%q) = nil error", v)
		}
	}
}
