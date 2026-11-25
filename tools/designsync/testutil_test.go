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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pillSource = "export function StatusPill() {}\nconst TONE = {};\n"
	bellSource = "export function BellButton() {}\n"
)

// testManifest is in the tool's format: an in-sync component with its design's
// hash recorded, one without, and a component no code draws yet.
var testManifest = `{
  "$schema": "./components.schema.json",
  "version": 1,
  "components": [
    {
      "id": "dashboard/status-pill",
      "figma": {
        "node": "9:148",
        "name": "StatusPill",
        "hash": "00000000000000aa"
      },
      "code": [
        {
          "path": "src/status-pill.tsx",
          "symbol": "TONE",
          "blob": "` + blobHash([]byte(pillSource)) + `"
        }
      ]
    },
    {
      "id": "shell/bell-button",
      "figma": {
        "node": "5:21",
        "name": "BellButton"
      },
      "code": [
        {
          "path": "src/bell-button.tsx",
          "blob": "` + blobHash([]byte(bellSource)) + `"
        }
      ]
    },
    {
      "id": "chat/badge",
      "figma": {
        "node": "20:1",
        "name": "Badge"
      }
    }
  ]
}
`

// screenSource is the one file the test screen's entry names.
const screenSource = "export function Dashboard() {}\n"

// withScreen adds a screen in sync to the fixture's manifest.
func withScreen(t *testing.T, tl tool) {
	t.Helper()
	writeFile(t, tl, "src/dashboard.tsx", screenSource)
	editManifest(t, tl, "\n  ]\n}\n", `
  ],
  "screens": [
    {
      "id": "screens/dashboard",
      "figma": {
        "node": "42:1",
        "name": "Dashboard",
        "hash": "00000000000000ee"
      },
      "code": [
        {
          "path": "src/dashboard.tsx",
          "blob": "`+blobHash([]byte(screenSource))+`"
        }
      ]
    }
  ]
}
`)
}

// fixtureTool returns a tool over a checkout holding the test manifest, its
// sources.
func fixtureTool(t *testing.T) tool {
	t.Helper()
	tl := tool{root: t.TempDir()}
	writeFile(t, tl, manifestPath, testManifest)
	writeFile(t, tl, "src/status-pill.tsx", pillSource)
	writeFile(t, tl, "src/bell-button.tsx", bellSource)
	return tl
}

func writeFile(t *testing.T, tl tool, path, content string) {
	t.Helper()
	full := filepath.Join(tl.root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, tl tool, path string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tl.root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// withDesign returns tl with Figma holding the components and screens.
func withDesign(tl tool, stamps ...stamped) tool {
	tl.design = func() ([]stamped, error) { return stamps, nil }
	return tl
}

// fixtureNodes returns testdata/nodes.json, the nodes endpoint's response for
// a small Components page and Screens page.
func fixtureNodes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// fixturePages returns the fixture's two pages, decoded afresh so a test can
// edit them.
func fixturePages(t *testing.T) (components, screens *page) {
	t.Helper()
	var r struct {
		Nodes map[string]*page
	}
	if err := json.Unmarshal(fixtureNodes(t), &r); err != nil {
		t.Fatal(err)
	}
	return r.Nodes["3:3"], r.Nodes["3:4"]
}

// layer finds the layer with the id under p.
func layer(t *testing.T, p *page, layerID string) node {
	t.Helper()
	for _, n := range descendants(p.Document) {
		if id(n) == layerID {
			return n
		}
	}
	t.Fatalf("no layer %s", layerID)
	return nil
}

// editManifest replaces old with new, which must be there once.
func editManifest(t *testing.T, tl tool, old, new string) {
	t.Helper()
	if strings.Count(testManifest, old) != 1 {
		t.Fatalf("the test manifest holds %q %d times", old, strings.Count(testManifest, old))
	}
	writeFile(t, tl, manifestPath, strings.Replace(testManifest, old, new, 1))
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Errorf("err = %v, want one holding %q", err, substr)
	}
}
