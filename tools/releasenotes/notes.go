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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// notes is notes.json, the prose the skill writes. Each highlight and item
// cites the pull requests it describes; render adds their links. Every string
// is one line: a paragraph, a title or a bullet.
type notes struct {
	Tag          string      `json:"tag"`
	Summary      []string    `json:"summary"` // the opening paragraphs
	Highlights   []highlight `json:"highlights"`
	New          []item      `json:"new"`
	Fixes        []item      `json:"fixes"`
	Performance  []item      `json:"performance"`
	Security     []item      `json:"security"`
	UpgradeNotes []item      `json:"upgrade_notes"`
	Omitted      []omission  `json:"omitted"`
}

// A highlight is a change worth a heading and a few paragraphs of its own.
type highlight struct {
	Title string   `json:"title"`
	Body  []string `json:"body"` // one paragraph per string
	PRs   []int    `json:"prs"`
}

type item struct {
	Text string `json:"text"`
	PRs  []int  `json:"prs"`
}

// An omission is a feat, fix or ! pull request the notes leave out on purpose.
type omission struct {
	PR     int    `json:"pr"`
	Reason string `json:"reason"`
}

type section struct {
	key, title string
	items      []item
}

// sections lists the notes' sections in the order render writes them.
func (n notes) sections() []section {
	return []section{
		{"new", "New", n.New},
		{"fixes", "Fixes", n.Fixes},
		{"performance", "Performance", n.Performance},
		{"security", "Security", n.Security},
		{"upgrade_notes", "Upgrade notes", n.UpgradeNotes},
	}
}

func readNotes(path string) (notes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return notes{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var n notes
	if err := dec.Decode(&n); err != nil {
		return notes{}, fmt.Errorf("%s: %w", path, err)
	}
	return n, nil
}
