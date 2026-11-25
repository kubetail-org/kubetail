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
	"reflect"
	"testing"
)

// The hashes pin the snapshot: a change to what it covers changes every
// entry's hash, and needs every entry recorded again.
func TestHashesAreStable(t *testing.T) {
	want := map[string]string{
		"9:148": "85b65f12f4764dc5",
		"5:21":  "24e5aaf56cdbca46",
		"7:1":   "70146a80abbd6213",
		"42:1":  "ec305ba5806b9204",
		"44:1":  "046a00a28fc8ba0d",
		"43:2":  "474f90067d7902b8",
	}
	if got := hashes(fixturePages(t)); !reflect.DeepEqual(got, want) {
		t.Errorf("hashes = %v, want %v", got, want)
	}
}

func TestHashSeesWhatALayerLooksLike(t *testing.T) {
	for _, tc := range []struct {
		name string
		node string // the entry whose hash is checked
		edit func(t *testing.T, components, screens *page)
		same bool
	}{
		{"the component moves on the canvas", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:148")["relativeTransform"] = []any{[]any{1.0, 0.0, 500.0}, []any{0.0, 1.0, 80.0}}
		}, true},
		{"a property outside the hash", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:148")["interactions"] = []any{"click"}
		}, true},
		{"its description", "9:148", func(t *testing.T, c, _ *page) {
			c.ComponentSets["9:148"] = componentMeta{Key: "pill", Description: "Rounder."}
		}, true},
		{"float noise", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:151")["size"] = map[string]any{"x": 40.004, "y": 16.0}
		}, true},
		{"a layer resizes", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:151")["size"] = map[string]any{"x": 40.1, "y": 16.0}
		}, false},
		{"a layer moves", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:151")["relativeTransform"] = []any{[]any{1.0, 0.0, 9.0}, []any{0.0, 1.0, 4.0}}
		}, false},
		{"a variant's fill", "9:148", func(t *testing.T, c, _ *page) {
			layer(t, c, "9:149")["fills"] = []any{}
		}, false},
		{"a vector's path, inside an instance", "5:21", func(t *testing.T, c, _ *page) {
			layer(t, c, "I5:22;100:2")["strokeGeometry"] = []any{map[string]any{"path": "M0 0L18 19"}}
		}, false},
		{"which component an instance draws", "5:21", func(t *testing.T, c, _ *page) {
			c.Components["100:1"] = componentMeta{Key: "lucide-bell-ring"}
		}, false},
		{"an instance of a component the page does not list", "5:21", func(t *testing.T, c, _ *page) {
			delete(c.Components, "100:1")
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := hashes(fixturePages(t))
			components, screens := fixturePages(t)
			tc.edit(t, components, screens)
			after := hashes(components, screens)
			if same := before[tc.node] == after[tc.node]; same != tc.same {
				t.Errorf("hash unchanged = %v, want %v", same, tc.same)
			}
		})
	}
}

func TestScreenHashStopsAtEachInstance(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(t *testing.T, screens *page)
		same bool
	}{
		{"what the component draws", func(t *testing.T, s *page) {
			layer(t, s, "I42:3;9:151")["fills"] = []any{}
		}, true},
		{"its stamp", func(t *testing.T, s *page) {
			layer(t, s, "42:9")["characters"] = `{"kubetail":1,"id":"screens/dashboard","changes":["Wider."]}`
		}, true},
		{"a section nested in it", func(t *testing.T, s *page) {
			layer(t, s, "44:2")["size"] = map[string]any{"x": 900.0, "y": 200.0}
		}, true},
		{"an overridden value", func(t *testing.T, s *page) {
			layer(t, s, "I42:3;9:151")["characters"] = "Stale"
		}, false},
		{"an overridden style", func(t *testing.T, s *page) {
			layer(t, s, "I42:3;9:151")["style"].(map[string]any)["fontSize"] = 14.0
		}, false},
		{"where the instance sits", func(t *testing.T, s *page) {
			layer(t, s, "42:3")["relativeTransform"] = []any{[]any{1.0, 0.0, 24.0}, []any{0.0, 1.0, 16.0}}
		}, false},
		{"its properties", func(t *testing.T, s *page) {
			layer(t, s, "42:3")["componentProperties"] = map[string]any{"State": map[string]any{"type": "VARIANT", "value": "Hover"}}
		}, false},
		{"its modes", func(t *testing.T, s *page) {
			layer(t, s, "42:2")["explicitVariableModes"] = map[string]any{"VariableCollectionId:theme": "1:1"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := hashes(fixturePages(t))
			components, screens := fixturePages(t)
			tc.edit(t, screens)
			if same := before["42:1"] == hashes(components, screens)["42:1"]; same != tc.same {
				t.Errorf("hash unchanged = %v, want %v", same, tc.same)
			}
		})
	}
}

func TestOverridesNameASwappedInstancesComponent(t *testing.T) {
	_, screens := fixturePages(t)
	screens.Components["100:1"] = componentMeta{Key: "lucide-bell"}
	pill := layer(t, screens, "42:3")
	pill["overrides"] = []any{
		map[string]any{"id": "I42:3;9:151", "overriddenFields": []any{"width", "text", "inheritTextStyleId"}},
		map[string]any{"id": "I42:3;5:22", "overriddenFields": []any{"componentProperties"}},
	}
	label := layer(t, screens, "I42:3;9:151")
	label["styles"] = map[string]any{"text": "S:1"}
	pill["children"] = append(pill["children"].([]any), node{"id": "I42:3;5:22", "type": "INSTANCE", "componentId": "100:1"})

	got := screens.overrides(pill)
	want := []any{
		map[string]any{"id": "I42:3;5:22", "main": "lucide-bell"},
		map[string]any{"id": "I42:3;9:151", "width": label["size"], "text": "Syncing", "inheritTextStyleId": label["styles"]},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overrides =\n%v\nwant\n%v", got, want)
	}
}

func hashes(components, screens *page) map[string]string {
	out := map[string]string{}
	for _, s := range readPages(components, screens) {
		out[s.Node] = s.Hash
	}
	return out
}
