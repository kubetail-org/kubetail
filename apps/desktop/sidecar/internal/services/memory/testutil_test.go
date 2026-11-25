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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// The clusters the tests file memories under. Raw SQL, since this package owns no
// cluster statement.
const (
	clusterA apimeta.ClusterID = "a"
	clusterB apimeta.ClusterID = "b"
)

// fakeUIDs is each cluster's last-probed UID, settable mid-test.
type fakeUIDs struct {
	mu   sync.Mutex
	uids map[apimeta.ClusterID]string
}

func (f *fakeUIDs) ServerUID(_ context.Context, id apimeta.ClusterID) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uids[id]
}

func (f *fakeUIDs) set(id apimeta.ClusterID, uid string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.uids == nil {
		f.uids = map[apimeta.ClusterID]string{}
	}
	f.uids[id] = uid
}

type harness struct {
	svc  *service
	db   *appdb.DB
	uids *fakeUIDs
}

// newHarness is a started service over an app.db of the test's own, with two
// clusters seeded.
func newHarness(t *testing.T) *harness {
	t.Helper()
	db := appdbtest.Open(t)
	for _, id := range []apimeta.ClusterID{clusterA, clusterB} {
		exec(t, db, `INSERT INTO clusters (id, source, source_key, created_at, updated_at) VALUES (?, 'kubeconfig', ?, 0, 0)`, string(id), "ctx-"+string(id))
	}
	uids := &fakeUIDs{}
	svc, err := newService(db, uids)
	require.NoError(t, err)
	svc.now = func() time.Time { return time.Date(2026, 9, 23, 10, 0, 0, 0, time.Local) }
	stop, err := svc.Start(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, stop(context.Background()))
		require.NoError(t, svc.Close())
	})
	return &harness{svc: svc, db: db, uids: uids}
}

func exec(t *testing.T, db *appdb.DB, q string, args ...any) {
	t.Helper()
	_, err := db.Write.Exec(q, args...)
	require.NoError(t, err)
}

// save is a model save of name from no chat that must succeed; the body names it.
func (h *harness) save(t *testing.T, cluster apimeta.ClusterID, name string) {
	t.Helper()
	require.NoError(t, h.svc.Save(t.Context(), cluster, name, "the body of "+name, ""))
}

// global is the user's note for every cluster named name, from the dialog.
func (h *harness) global(t *testing.T, name string) Memory {
	t.Helper()
	m, err := h.svc.Create(t.Context(), Input{Name: name, Body: "the body of " + name})
	require.NoError(t, err)
	return m
}

// names is what cluster sees, by name, in Visible's order.
func (h *harness) names(t *testing.T, cluster apimeta.ClusterID) []string {
	t.Helper()
	ms, err := h.svc.Visible(t.Context(), cluster)
	require.NoError(t, err)
	out := []string{}
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

// get is the memory cluster sees under name.
func (h *harness) get(t *testing.T, cluster apimeta.ClusterID, name string) Memory {
	t.Helper()
	ms, err := h.svc.Visible(t.Context(), cluster)
	require.NoError(t, err)
	for _, m := range ms {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("%s sees no memory %q", cluster, name)
	return Memory{}
}

// unscannable puts a view in place of the memories table whose every row fails
// to scan, its creation time being text.
func unscannable(t *testing.T, db *appdb.DB) {
	t.Helper()
	exec(t, db, `ALTER TABLE memories RENAME TO memories_held`)
	exec(t, db, `CREATE VIEW memories AS SELECT id, cluster_id, server_uid, name, body,
	written_by, chat_id, 'never' AS created_at, updated_at FROM memories_held`)
}
