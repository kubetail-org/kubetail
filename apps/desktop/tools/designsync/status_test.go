package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// The fixture's entries as Figma holds them in sync.
var (
	figmaPill      = stamped{Node: "9:148", Name: "StatusPill", Hash: "00000000000000aa", Stamp: &stamp{ID: "dashboard/status-pill"}}
	figmaBell      = stamped{Node: "5:21", Name: "BellButton", Stamp: &stamp{ID: "shell/bell-button"}}
	figmaBadge     = stamped{Node: "20:1", Name: "Badge", Stamp: &stamp{ID: "chat/badge", Changes: []string{"A badge."}}}
	figmaDashboard = stamped{Node: "42:1", Name: "Dashboard", Hash: "00000000000000ee", Stamp: &stamp{ID: "screens/dashboard"}}
)

func statusOf(t *testing.T, tl tool) string {
	t.Helper()
	var out strings.Builder
	if err := tl.statusCmd(&out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func wantLines(t *testing.T, got string, lines ...string) {
	t.Helper()
	for _, want := range lines {
		if !strings.Contains(got, want) {
			t.Errorf("status lacks %q:\n%s", want, got)
		}
	}
}

func TestStatusOfTheCodeSideAlone(t *testing.T) {
	got := statusOf(t, fixtureTool(t))
	wantLines(t, got, "not built  chat/badge", "1 not built, 2 in sync", "Code side only")
	if strings.Contains(got, "status-pill") {
		t.Errorf("status lists a component in sync:\n%s", got)
	}
}

func TestStatusNamesTheSideThatMoved(t *testing.T) {
	changed := figmaPill
	changed.Hash = "00000000000000bb"
	changed.Stamp = &stamp{ID: "dashboard/status-pill", Changes: []string{"Rounder.", "Taller."}}
	// The hash differs from the manifest's and the stamp declares nothing.
	edited := figmaPill
	edited.Hash = "00000000000000bb"

	t.Run("in sync", func(t *testing.T) {
		got := statusOf(t, withDesign(fixtureTool(t), figmaPill, figmaBell, figmaBadge))
		if want := "not built  chat/badge  1 design change\n1 not built, 2 in sync\n"; got != want {
			t.Errorf("status = %q, want %q", got, want)
		}
	})
	t.Run("code ahead", func(t *testing.T) {
		tl := fixtureTool(t)
		writeFile(t, tl, "src/status-pill.tsx", pillSource+"// rounder\n")
		writeFile(t, tl, "src/bell-button.tsx", bellSource+"// louder\n")
		wantLines(t, statusOf(t, tl),
			"code ahead  dashboard/status-pill  src/status-pill.tsx",
			"code ahead  shell/bell-button      src/bell-button.tsx",
			"2 code ahead, 1 not built")
	})
	t.Run("source gone", func(t *testing.T) {
		tl := fixtureTool(t)
		editManifest(t, tl, "src/status-pill.tsx", "src/gone.tsx")
		wantLines(t, statusOf(t, tl), "code ahead  dashboard/status-pill  src/gone.tsx is gone")
	})
	t.Run("design ahead", func(t *testing.T) {
		wantLines(t, statusOf(t, withDesign(fixtureTool(t), changed, figmaBell, figmaBadge)),
			"design ahead  dashboard/status-pill  2 design changes")
	})
	t.Run("edited by hand", func(t *testing.T) {
		wantLines(t, statusOf(t, withDesign(fixtureTool(t), edited, figmaBell, figmaBadge)),
			"design ahead  dashboard/status-pill  edited in Figma, with no changes written on its stamp")
	})
	t.Run("edited by hand and code ahead", func(t *testing.T) {
		tl := fixtureTool(t)
		writeFile(t, tl, "src/status-pill.tsx", pillSource+"// rounder\n")
		wantLines(t, statusOf(t, withDesign(tl, edited, figmaBell, figmaBadge)),
			"diverged   dashboard/status-pill  src/status-pill.tsx, edited in Figma")
	})
	t.Run("diverged", func(t *testing.T) {
		tl := fixtureTool(t)
		writeFile(t, tl, "src/status-pill.tsx", pillSource+"// rounder\n")
		wantLines(t, statusOf(t, withDesign(tl, changed, figmaBell, figmaBadge)),
			"diverged   dashboard/status-pill  src/status-pill.tsx, 2 design changes")
	})
}

func TestStatusListsWhatTheStampsAndTheManifestDisagreeOn(t *testing.T) {
	got := statusOf(t, withDesign(fixtureTool(t),
		stamped{Node: "9:999", Name: "StatusPill", Stamp: &stamp{ID: "dashboard/status-pill"}},
		stamped{Node: "5:21", Name: "AlarmButton", Stamp: &stamp{ID: "shell/bell-button"}},
		stamped{Node: "5:22", Name: "CopyButton", Stamp: &stamp{ID: "shell/bell-button"}},
		stamped{Node: "7:1", Name: "New"},
		stamped{Node: "7:2", Name: "Broken", Invalid: true},
		stamped{Node: "7:3", Name: "Stray", Stamp: &stamp{ID: "chat/stray"}},
	))
	wantLines(t, got,
		"dashboard/status-pill  its stamp is on node 9:999, not 9:148",
		"shell/bell-button      renamed in Figma: BellButton is now AlarmButton",
		"CopyButton (5:22)      carries AlarmButton's stamp too: shell/bell-button",
		"New (7:1)              has no stamp",
		"Broken (7:2)           its stamp is not valid JSON",
		"Stray (7:3)            its stamp's id is not in the manifest: chat/stray",
		"chat/badge             nothing in Figma carries its stamp",
		"7 unmatched")
}

func TestStatusReportsWhatItCannotRead(t *testing.T) {
	tl := fixtureTool(t)
	tl.design = func() ([]stamped, error) { return nil, errors.New("reading Figma: 403 Forbidden") }
	wantErr(t, tl.statusCmd(io.Discard), "403 Forbidden")

	writeFile(t, tl, manifestPath, "{")
	wantErr(t, tl.statusCmd(io.Discard), "design/components.json:")
}

func TestStatusListsAScreenAsAComponent(t *testing.T) {
	// screenTool's manifest holds the screen, and Figma holds it as screen.
	screenTool := func(t *testing.T, screen stamped) tool {
		t.Helper()
		tl := fixtureTool(t)
		withScreen(t, tl)
		return withDesign(tl, figmaPill, figmaBell, figmaBadge, screen)
	}

	t.Run("in sync", func(t *testing.T) {
		wantLines(t, statusOf(t, screenTool(t, figmaDashboard)), "1 not built, 3 in sync")
	})
	t.Run("code ahead", func(t *testing.T) {
		tl := screenTool(t, figmaDashboard)
		writeFile(t, tl, "src/dashboard.tsx", screenSource+"// wider\n")
		wantLines(t, statusOf(t, tl), "code ahead  screens/dashboard  src/dashboard.tsx")
	})
	t.Run("edited by hand", func(t *testing.T) {
		edited := figmaDashboard
		edited.Hash = "00000000000000ef"
		wantLines(t, statusOf(t, screenTool(t, edited)), "design ahead  screens/dashboard  edited in Figma")
	})
	t.Run("no stamp", func(t *testing.T) {
		unstamped := figmaDashboard
		unstamped.Stamp = nil
		wantLines(t, statusOf(t, screenTool(t, unstamped)),
			"unmatched  Dashboard (42:1)   has no stamp",
			"unmatched  screens/dashboard  nothing in Figma carries its stamp")
	})
}
