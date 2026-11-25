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
	"regexp"
)

// A stamped is one Figma component or screen.
type stamped struct {
	Node    string
	Name    string
	Hash    string // of what it looks like
	Stamp   *stamp
	Invalid bool // a stamp is there and does not parse
}

// A stamp is the JSON block a Figma component's description ends with, or a
// screen's Stamp layer holds.
type stamp struct {
	Kubetail int      `json:"kubetail"`
	ID       string   `json:"id"`
	Changes  []string `json:"changes"`
}

// A node is one layer as Figma's REST API returns it. The API leaves out a
// property at its default.
type node = map[string]any

// A page is one page of the Desktop file, as the nodes endpoint returns it.
type page struct {
	Document      node                     `json:"document"`
	Components    map[string]componentMeta `json:"components"`
	ComponentSets map[string]componentMeta `json:"componentSets"`
}

// componentMeta is what the API says of a component beside its layers.
type componentMeta struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

// readPages returns every component on the Components page and every screen
// on the Screens page, each with its hash and stamp.
func readPages(components, screens *page) []stamped {
	var out []stamped
	for _, n := range findComponents(components.Document) {
		meta := components.Components[id(n)]
		if n["type"] == "COMPONENT_SET" {
			meta = components.ComponentSets[id(n)]
		}
		out = append(out, components.read(n, meta.Description, false))
	}
	// A screen is a section that holds something drawn, not only other sections.
	for _, n := range descendants(screens.Document) {
		if n["type"] != "SECTION" {
			continue
		}
		var text string
		drawn := false
		for _, c := range children(n) {
			switch {
			case isStamp(c):
				text, _ = c["characters"].(string)
			case c["type"] != "SECTION":
				drawn = true
			}
		}
		if drawn {
			out = append(out, screens.read(n, text, true))
		}
	}
	return out
}

// read returns the component or screen at n, whose stamp text holds.
func (p *page) read(n node, text string, screen bool) stamped {
	snap := p.snapshot(n, screen)
	// Where it sits on the canvas is not part of its design.
	delete(snap, "relativeTransform")

	name, _ := n["name"].(string)
	s := stamped{Node: id(n), Name: name, Hash: hash(snap)}
	s.Stamp, s.Invalid = parseStamp(text)
	return s
}

// The stamp runs from its opening brace to the text's last one, however a
// person spaced it.
var stampRE = regexp.MustCompile(`(?s)\{\s*"kubetail".*\}`)

// parseStamp returns the stamp text ends with, or none. invalid reports one
// that is there and does not parse.
func parseStamp(text string) (s *stamp, invalid bool) {
	match := stampRE.FindString(text)
	if match == "" {
		return nil, false
	}
	if err := json.Unmarshal([]byte(match), &s); err != nil {
		return nil, true
	}
	return s, false
}

// A screen's stamp is a text layer, since a section has no description.
func isStamp(n node) bool { return n["type"] == "TEXT" && n["name"] == "Stamp" }

func id(n node) string {
	s, _ := n["id"].(string)
	return s
}

func children(n node) []node {
	kids, _ := n["children"].([]any)
	out := make([]node, len(kids))
	for i, k := range kids {
		out[i] = k.(node)
	}
	return out
}

// descendants lists n and every layer under it, parents first.
func descendants(n node) []node {
	out := []node{n}
	for _, c := range children(n) {
		out = append(out, descendants(c)...)
	}
	return out
}

// findComponents lists the components and component sets under n. It does
// not look inside one: a variant is its component set's, which carries the
// description, and Figma nests no component in another.
func findComponents(n node) []node {
	if n["type"] == "COMPONENT" || n["type"] == "COMPONENT_SET" {
		return []node{n}
	}
	var out []node
	for _, c := range children(n) {
		out = append(out, findComponents(c)...)
	}
	return out
}
