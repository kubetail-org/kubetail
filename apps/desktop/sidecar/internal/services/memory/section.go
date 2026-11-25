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
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/fencejson"
)

// ScopeBudget is the most one scope's notes may take in the section, as it
// encodes them, so a question carries at most two of it and today. Every write
// is held to it, so the section never cuts a note.
const ScopeBudget = 4 << 10

// sectionEntry is one note as the model reads it. By is who wrote it last: the
// system prompt makes a user's note a request and the model's a note of its own.
type sectionEntry struct {
	Name    string `json:"name"`
	Scope   string `json:"scope"`
	By      string `json:"by"`
	Updated string `json:"updated"`
	Body    string `json:"body"`
}

type section struct {
	Today    string         `json:"today"`
	Memories []sectionEntry `json:"memories"`
}

// UnavailableSection is the section when the notes could not be read.
var UnavailableSection = json.RawMessage(`{"unavailable":true}`)

// sectionSize is what v costs in the context block, measured as the card
// marshals it.
func sectionSize(v any) int {
	raw, _ := fencejson.Marshal(v) // strings always marshal
	return len(raw)
}

// Section is every note cluster sees, whole, as the model reads it in the context
// block: the cluster's own, then the ones for every cluster, each by name.
func (s *service) Section(ctx context.Context, cluster apimeta.ClusterID) json.RawMessage {
	ms, err := visible(ctx, s.store.Stmts(), cluster)
	if err != nil {
		slog.Warn("memory section unavailable", "err", err)
		return UnavailableSection
	}
	raw, _ := json.Marshal(section{Today: s.now().Local().Format(time.DateOnly), Memories: entries(ms)}) // strings always marshal
	return raw
}

// entries is ms as the section lists them, never nil.
func entries(ms []Memory) []sectionEntry {
	out := make([]sectionEntry, 0, len(ms))
	for _, m := range ms {
		out = append(out, sectionEntry{
			Name:    m.Name,
			Scope:   scopeOf(m),
			By:      string(m.WrittenBy),
			Updated: m.UpdatedAt.Local().Format(time.DateOnly),
			Body:    m.Body,
		})
	}
	return out
}

// scopeOf is who sees m.
func scopeOf(m Memory) string {
	if m.ClusterID == nil {
		return "everywhere"
	}
	return "cluster"
}
