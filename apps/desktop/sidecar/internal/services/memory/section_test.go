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

package memory

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sectionJSON struct {
	Today    string `json:"today"`
	Memories []struct {
		Name    string `json:"name"`
		Scope   string `json:"scope"`
		By      string `json:"by"`
		Updated string `json:"updated"`
		Body    string `json:"body"`
	} `json:"memories"`
	Unavailable bool `json:"unavailable"`
}

func (h *harness) section(t *testing.T) (json.RawMessage, sectionJSON) {
	t.Helper()
	raw := h.svc.Section(t.Context(), clusterA)
	var s sectionJSON
	require.NoError(t, json.Unmarshal(raw, &s))
	return raw, s
}

// Every note the cluster sees goes to the model whole, with who wrote it, so the
// model can tell the user's requests from its own notes.
func TestTheSectionCarriesEveryNote(t *testing.T) {
	h := newHarness(t)
	_, s := h.section(t)
	assert.Equal(t, "2026-09-23", s.Today)
	assert.NotNil(t, s.Memories, "an empty section is [], not null")
	assert.Empty(t, s.Memories)

	h.global(t, "prefs")
	h.save(t, clusterA, "pages")
	raw, s := h.section(t)
	require.Len(t, s.Memories, 2)
	assert.Equal(t, "pages", s.Memories[0].Name)
	assert.Equal(t, "cluster", s.Memories[0].Scope)
	assert.Equal(t, "model", s.Memories[0].By)
	assert.Equal(t, "2026-09-23", s.Memories[0].Updated)
	assert.Equal(t, "the body of pages", s.Memories[0].Body)
	assert.Equal(t, "prefs", s.Memories[1].Name)
	assert.Equal(t, "everywhere", s.Memories[1].Scope)
	assert.Equal(t, "user", s.Memories[1].By)
	assert.Equal(t, "the body of prefs", s.Memories[1].Body)
	for _, gone := range []string{`"more"`, `"description"`, `"type"`, "rebuilt"} {
		assert.NotContains(t, string(raw), gone)
	}

	_, err := h.svc.Update(t.Context(), h.get(t, clusterA, "pages").ID, Input{ClusterID: new(clusterA), Name: "pages", Body: "adopted"})
	require.NoError(t, err)
	_, s = h.section(t)
	assert.Equal(t, "user", s.Memories[0].By, "the section's author follows the row")
}

// Cluster first, then by name, so the section only changes when a note does.
func TestTheSectionIsSortedByName(t *testing.T) {
	h := newHarness(t)
	h.global(t, "a-global")
	h.save(t, clusterA, "z-note")
	h.save(t, clusterA, "b-note")

	_, s := h.section(t)
	var names []string
	for _, m := range s.Memories {
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"b-note", "z-note", "a-global"}, names)
}

// A UID stamped on a write is not read back: a rebuilt cluster flags nothing.
func TestTheSectionNeverFlagsARebuild(t *testing.T) {
	h := newHarness(t)
	h.uids.set(clusterA, "uid-1")
	h.save(t, clusterA, "stamped")
	h.uids.set(clusterA, "uid-2")

	raw, _ := h.section(t)
	assert.NotContains(t, string(raw), "rebuilt")
}

func TestAnUnreadableSectionSaysSo(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.db.Close())

	_, s := h.section(t)
	assert.True(t, s.Unavailable)
}
