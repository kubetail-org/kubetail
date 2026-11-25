// Copyright 2024 The Kubetail Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build !windows

package chat

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/llm"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/clustercard"
)

// A question's context block, pinned as the model reads it in testdata/context.md:
// the card, the notes, the workspace, the view and the sandbox, in that order, in
// its tags, then the question. The sample is one cluster with a cache, two notes,
// a tail with one selected line and two granted folders. `make generate-prompts` rewrites it, beside the system prompt's goldens
// in internal/app. Unix alone, since the paths it names are Unix paths.
func TestTheContextBlockMatchesItsGolden(t *testing.T) {
	t.Parallel()

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = root.Close() })
	s := &service{
		sandboxState: sandbox.State{Available: true, NetworkAvailable: true},
		memories:     sampleMemories{},
		chatsRoot:    root,
	}
	chatID := ChatID("0195f1a0-5b6e-7c3d-8e4f-0a1b2c3d4e5f")

	block := s.withMemory(t.Context(), clustercard.Render(sampleFacts()), "cluster-1")
	from := time.Date(2026, 3, 15, 9, 14, 2, 118_000_000, time.UTC)
	to := time.Date(2026, 3, 15, 9, 14, 31, 902_000_000, time.UTC)
	block = withView(s.withWorkspace(block, chatID), "toolu_01AbC", tailWebapp(), Snapshot{
		CallID: "c", From: &from, To: &to, Lines: 38, AtEnd: true, PinnedToEnd: true,
		Selected: []SelectedLine{{
			Timestamp: time.Date(2026, 3, 15, 9, 14, 20, 4_000_000, time.UTC), Namespace: "payments",
			Pod: "webapp-7d9f-x2k", Container: "app", Message: "panic: assignment to entry in nil map",
		}},
		SelectedCount: 1,
	})
	block = s.withSandbox(block, sandboxState{
		networkThisTurn: true,
		folders:         []session.Folder{{Path: "/home/ana/src/payments", Write: true}, {Path: "/home/ana/notes"}},
	})
	// The chats' root is wherever the test runs; the golden names a home.
	block = strings.ReplaceAll(block, root.Name(), "/home/ana/.local/share/kubetail/chats")
	got := llm.Prompt([]llm.Block{llm.ContextBlock(block), llm.TextBlock("Why is the payments deployment paused?")}) + "\n"

	path := filepath.Join("testdata", "context.md")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
		return
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run `make generate-prompts` to write it")
	assert.Equal(t, string(want), got)
}

// sampleMemories is two notes as memory.Section writes them: the user's for the
// cluster, and one Kubetail kept for every cluster.
type sampleMemories struct{}

func (sampleMemories) Section(context.Context, apimeta.ClusterID) json.RawMessage {
	return json.RawMessage(`{"today":"2026-03-15","memories":[` +
		`{"name":"deploy-window","scope":"cluster","by":"user","updated":"2026-02-02","body":"Never restart payments pods between 09:00 and 11:00 UTC."},` +
		`{"name":"istio-apis","scope":"everywhere","by":"model","updated":"2026-01-20","body":"Istio 1.24 serves both the v1 and v1beta1 VirtualService APIs."}]}`)
}

// sampleFacts is one reachable cluster with a cache: three namespaces, four
// built-in kinds, one of them paused, and Istio's.
func sampleFacts() clustercard.Facts {
	return clustercard.Facts{
		Name:       "prod",
		Context:    "gke_acme_us-east1_prod",
		Version:    "v1.31.2",
		Connection: "Connected",
		TLS:        "verified",
		Cache: &clustercard.CacheFacts{
			Health:    cluster.ClusterCacheHealth{Reason: "Watching", TotalKinds: 5, PausedKinds: 1},
			Discovery: "Discovered",
			Kinds: []clustercard.Kind{
				{APIVersion: "v1", Kind: "Pod", Resource: "pods", Scope: "Namespaced", Reason: "Watching"},
				{APIVersion: "v1", Kind: "Namespace", Resource: "namespaces", Scope: "Cluster", Reason: "Watching"},
				{APIVersion: "v1", Kind: "Node", Resource: "nodes", Scope: "Cluster", Reason: "Watching"},
				{APIVersion: "apps/v1", Kind: "Deployment", Resource: "deployments", Scope: "Namespaced", Reason: "Paused"},
				{APIVersion: "networking.istio.io/v1", Kind: "VirtualService", Resource: "virtualservices", Scope: "Namespaced", Reason: "Watching"},
			},
			Namespaces: []string{"kube-system", "default", "payments"},
		},
	}
}
