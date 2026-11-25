package main

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRecordSetsTheWorkingTreesFingerprints(t *testing.T) {
	tl := fixtureTool(t)
	changed := pillSource + "// rounder\n"
	writeFile(t, tl, "src/status-pill.tsx", changed)
	louder := bellSource + "// louder\n"
	writeFile(t, tl, "src/bell-button.tsx", louder)

	if err := tl.recordCmd([]string{"dashboard/status-pill", "shell/bell-button"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(blobHash([]byte(pillSource)), blobHash([]byte(changed)), blobHash([]byte(bellSource)), blobHash([]byte(louder))).Replace(testManifest)
	if got := readFile(t, tl, manifestPath); got != want {
		t.Errorf("manifest after record:\n%s", got)
	}
}

func TestRecordRefusesWhatItCannotFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		setup    func(t *testing.T, tl tool)
		err      string
	}{
		{"unknown id", "chat/nope", nil, "chat/nope is not in design/components.json"},
		{"no code", "chat/badge", nil, "chat/badge: no code draws it yet"},
		{"missing source", "dashboard/status-pill", func(t *testing.T, tl tool) {
			editManifest(t, tl, "src/status-pill.tsx", "src/gone.tsx")
		}, "dashboard/status-pill: open"},
		{"no manifest", "shell/bell-button", func(t *testing.T, tl tool) {
			writeFile(t, tl, manifestPath, "{")
		}, "design/components.json:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl := fixtureTool(t)
			if tc.setup != nil {
				tc.setup(t, tl)
			}
			before := readFile(t, tl, manifestPath)
			wantErr(t, tl.recordCmd([]string{tc.id}, io.Discard), tc.err)
			if got := readFile(t, tl, manifestPath); got != before {
				t.Error("a failed record changed the manifest")
			}
		})
	}
}

func TestRecordSetsTheDesignFromFigma(t *testing.T) {
	tl := withDesign(fixtureTool(t),
		stamped{Node: "9:200", Name: "Pill", Hash: "00000000000000bb", Stamp: &stamp{ID: "dashboard/status-pill"}},
		stamped{Node: "20:1", Name: "Badge", Hash: "00000000000000cc", Stamp: &stamp{ID: "chat/badge"}},
		stamped{Node: "7:1", Name: "New", Hash: "00000000000000dd"},
	)

	if err := tl.recordCmd([]string{"dashboard/status-pill", "chat/badge"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(
		`"node": "9:148",
        "name": "StatusPill",
        "hash": "00000000000000aa"`, `"node": "9:200",
        "name": "Pill",
        "hash": "00000000000000bb"`,
		`"name": "Badge"`, `"name": "Badge",
        "hash": "00000000000000cc"`,
	).Replace(testManifest)
	if got := readFile(t, tl, manifestPath); got != want {
		t.Errorf("manifest after record:\n%s", got)
	}

	// An entry nothing in Figma carries is not recorded.
	wantErr(t, tl.recordCmd([]string{"shell/bell-button"}, io.Discard), "shell/bell-button: nothing in Figma carries its stamp")

	tl.design = func() ([]stamped, error) { return nil, errors.New("reading Figma: 403 Forbidden") }
	wantErr(t, tl.recordCmd([]string{"shell/bell-button"}, io.Discard), "403 Forbidden")
}

func TestRecordSetsAScreensFingerprints(t *testing.T) {
	tl := fixtureTool(t)
	withScreen(t, tl)
	before := readFile(t, tl, manifestPath)
	changed := screenSource + "// wider\n"
	writeFile(t, tl, "src/dashboard.tsx", changed)
	edited := figmaDashboard
	edited.Hash = "00000000000000ef"
	tl = withDesign(tl, edited)

	if err := tl.recordCmd([]string{"screens/dashboard"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer(blobHash([]byte(screenSource)), blobHash([]byte(changed)), "00000000000000ee", "00000000000000ef").Replace(before)
	if got := readFile(t, tl, manifestPath); got != want {
		t.Errorf("manifest after record:\n%s", got)
	}
}
