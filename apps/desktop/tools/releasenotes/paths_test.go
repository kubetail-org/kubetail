package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The repository's own filter names the desktop's paths.
func TestTheRepositorysFilterNamesTheDesktopsPaths(t *testing.T) {
	tl := tool{dir: ".", run: execRun}
	desktop, err := tl.productPaths(context.Background(), "desktop")
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"src", "src-tauri", "sidecar", "proto", "src-icons", "public"} {
		if want := ":(top,glob)" + dir + "/**"; !slices.Contains(desktop, want) {
			t.Errorf("desktop paths %v lack %s", desktop, want)
		}
	}
}

func TestProductPathsFails(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, body, want string
	}{
		{"no filter", "go:\n  - cli/**\n", "no release-cli filter"},
		{"not a list", "release-cli: cli/**\n", "no release-cli filter"},
		{"not a string", "release-cli:\n  - {a: b}\n", "is not a glob"},
		{"bad yaml", "release-cli: [\n", "path-filters.yml"},
		{"missing file", "", "path-filters.yml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl, _ := fixtureTool(t)
			path := filepath.Join(tl.dir, filtersFile)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := tl.productPaths(ctx, "cli")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}

	outside := tool{dir: t.TempDir(), run: execRun}
	if _, err := outside.productPaths(ctx, "cli"); err == nil {
		t.Error("productPaths outside a checkout succeeded")
	}
}
