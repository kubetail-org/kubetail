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

package layout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixture is testdata/layouts.json, which src-tauri/src/paths.rs reads
// too, so the host's layout and this one cannot drift.
type fixture struct {
	Name     string   `json:"name"`
	Platform Platform `json:"platform"`
	Dev      bool     `json:"dev"`
	Roots    Roots    `json:"roots"`
	Layout   Layout   `json:"layout"`
}

func readFixture(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "layouts.json"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	dec := json.NewDecoder(f)
	// A kind the fixture names and this side does not fails here.
	dec.DisallowUnknownFields()
	var cases []fixture
	require.NoError(t, dec.Decode(&cases))
	return cases
}

// slashed spells every folder with `/`, as the fixture does.
func slashed(l Layout) Layout {
	for _, dir := range l.fields() {
		*dir = filepath.ToSlash(*dir)
	}
	return l
}

func TestEachPlatformsLayoutIsTheFixtures(t *testing.T) {
	cases := readFixture(t)
	platforms := map[Platform]bool{}
	for _, c := range cases {
		platforms[c.Platform] = true
		t.Run(c.Name, func(t *testing.T) {
			assert.Equal(t, c.Layout, slashed(Of(c.Platform, c.Dev, c.Roots)))
		})
	}
	assert.Equal(t, map[Platform]bool{Linux: true, MacOS: true, Windows: true}, platforms)
}

func TestTheRunningOSTakesItsOwnLayout(t *testing.T) {
	assert.Equal(t, platformOf(runtime.GOOS), Current())
}

func TestEachGOOSMapsToItsPlatform(t *testing.T) {
	want := map[string]Platform{"linux": Linux, "darwin": MacOS, "windows": Windows}
	for goos, platform := range want {
		assert.Equal(t, platform, platformOf(goos), goos)
	}
}

// Dirs is what the sidecar fences, so the expected folders come from the
// fixture's own fields, not from the list Dirs reads: a kind left out of it
// fails here.
func TestDirsIsEveryFolderTheLayoutNames(t *testing.T) {
	f, err := os.ReadFile(filepath.Join("testdata", "layouts.json"))
	require.NoError(t, err)
	var raw []struct {
		Layout map[string]string `json:"layout"`
	}
	require.NoError(t, json.Unmarshal(f, &raw))

	for i, c := range readFixture(t) {
		var want []string
		for _, dir := range raw[i].Layout {
			if dir != "" && !slices.Contains(want, dir) {
				want = append(want, dir)
			}
		}
		l := slashed(Of(c.Platform, c.Dev, c.Roots))
		assert.ElementsMatch(t, want, l.Dirs(), c.Name)
		assert.Equal(t, []string{l.Data, l.Cache}, l.Dirs()[:2], "%s: the sidecar's own come first", c.Name)
	}
}

func TestDirsDropsAnExactDuplicate(t *testing.T) {
	l := Layout{Data: "/d", Cache: "/c", State: "/s", Logs: "/s", App: "/d"}
	assert.Equal(t, []string{"/d", "/c", "/s"}, l.Dirs())
}

// The leaf is the only thing the dev build changes.
func TestADevBuildTakesTheDevLeaf(t *testing.T) {
	for _, c := range readFixture(t) {
		for _, dir := range slashed(Of(c.Platform, c.Dev, c.Roots)).Dirs() {
			assert.Equal(t, c.Dev, strings.Contains(dir, "-dev"), "%s: %s", c.Name, dir)
		}
	}
}
