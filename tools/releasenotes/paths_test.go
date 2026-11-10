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
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The repository's own filters name each component, with the code it ships.
func TestTheRepositorysFiltersNameEachComponent(t *testing.T) {
	tl := tool{dir: ".", run: execRun}
	for component, dirs := range map[string][]string{
		"cli":           {"modules/cli", "modules/dashboard", "modules/shared", "dashboard-ui"},
		"dashboard":     {"modules/dashboard", "modules/shared", "dashboard-ui"},
		"cluster-api":   {"modules/cluster-api", "modules/shared", "proto"},
		"cluster-agent": {"crates", "proto"},
	} {
		paths, err := tl.componentPaths(context.Background(), component)
		if err != nil {
			t.Fatal(err)
		}
		for _, dir := range dirs {
			if want := ":(top,glob)" + dir + "/**"; !slices.Contains(paths, want) {
				t.Errorf("%s paths %v lack %s", component, paths, want)
			}
		}
	}
}

func TestComponentPathsFails(t *testing.T) {
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
			_, err := tl.componentPaths(ctx, "cli")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}

	outside := tool{dir: t.TempDir(), run: execRun}
	if _, err := outside.componentPaths(ctx, "cli"); err == nil {
		t.Error("componentPaths outside a checkout succeeded")
	}
}
