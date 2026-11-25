package main

import (
	"io"
	"strings"
	"testing"
)

func TestCheckPassesTheFixture(t *testing.T) {
	tl := fixtureTool(t)
	var out strings.Builder
	if err := tl.checkCmd(&out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
}

func TestCheckNeedsAManifest(t *testing.T) {
	wantErr(t, tool{root: t.TempDir()}.checkCmd(io.Discard), "components.json")
}

func TestCheckReportsEachProblem(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		problem        string
	}{
		{"version", `"version": 1`, `"version": 2`, "version is 2, want 1"},
		{"id shape", `"id": "shell/bell-button"`, `"id": "Bell"`, `components[1]: id "Bell" is not kebab-case`},
		{"id twice", `"id": "shell/bell-button"`, `"id": "chat/badge"`, "chat/badge: id is used twice"},
		{"node shape", `"node": "5:21"`, `"node": "5-21"`, `shell/bell-button: figma.node "5-21" is not a node id`},
		{"node twice", `"node": "5:21"`, `"node": "9:148"`, "shell/bell-button: figma.node 9:148 is also dashboard/status-pill's"},
		{"name", `"name": "Badge"`, `"name": ""`, "chat/badge: figma.name is empty"},
		{"hash", `"name": "Badge"`, `"name": "Badge", "hash": "abc"`, `chat/badge: figma.hash "abc" is not a designsync hash`},
		{"blob", `"blob": "` + blobHash([]byte(pillSource)), `"blob": "x`, "is not a git blob hash"},
		{"missing file", `"path": "src/status-pill.tsx"`, `"path": "src/gone.tsx"`, "dashboard/status-pill: open"},
		{"missing symbol", `"symbol": "TONE"`, `"symbol": "TONES"`, "dashboard/status-pill: src/status-pill.tsx has no TONES"},
		{"format", `"version": 1,`, `"version":  1,`, "is not in the tool's format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl := fixtureTool(t)
			editManifest(t, tl, tc.old, tc.new)
			var out strings.Builder
			err := tl.checkCmd(&out)
			wantErr(t, err, "problems in design/components.json")
			if !strings.Contains(out.String(), tc.problem) {
				t.Errorf("output lacks %q:\n%s", tc.problem, out.String())
			}
		})
	}
}

// These need a manifest the file's format cannot hold.
func TestCheckReportsWhatOnlyAStructHolds(t *testing.T) {
	tl := fixtureTool(t)
	m, _, err := tl.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	m.Components[0].Code = append(m.Components[0].Code, m.Components[0].Code[0])
	got := strings.Join(tl.check(m), "\n")
	for _, want := range []string{
		"dashboard/status-pill: src/status-pill.tsx is listed twice",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
}

func TestCheckTellsAScreenFromAComponent(t *testing.T) {
	tl := fixtureTool(t)
	withScreen(t, tl)
	var out strings.Builder
	if err := tl.checkCmd(&out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "3 components and 1 screens pass") {
		t.Errorf("output:\n%s", out.String())
	}

	m, _, err := tl.loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	m.Screens = append(m.Screens, m.Components[1], component{ID: "Dashboard"})
	m.Screens[1].Figma.Node = "5:99"
	m.Components[2].ID = "screens/badge"
	got := strings.Join(tl.check(m), "\n")
	for _, want := range []string{
		"shell/bell-button: a screen's id starts with screens/, and no component's does",
		"shell/bell-button: id is used twice",
		"screens/badge: a screen's id starts with screens/, and no component's does",
		`screens[2]: id "Dashboard" is not kebab-case`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
}
