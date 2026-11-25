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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A store that fails is an error from every read and write, never a panic or a
// silent success. Closing the DB is how a test fails it.
func TestAFailedStoreIsAnError(t *testing.T) {
	h := newHarness(t)
	h.save(t, clusterA, "n")
	m := h.get(t, clusterA, "n")
	require.NoError(t, h.db.Close())
	ctx := t.Context()
	st := h.svc.store.Stmts()

	_, err := h.svc.Visible(ctx, clusterA)
	assert.Error(t, err)
	assert.Error(t, h.svc.Save(ctx, clusterA, "n", "b", ""))
	assert.Error(t, h.svc.SaveEverywhere(ctx, "n", "b", ""))
	assert.Error(t, h.svc.Forget(ctx, clusterA, "n"))
	assert.Error(t, h.svc.ForgetEverywhere(ctx, "n"))
	_, err = h.svc.Create(ctx, input(new(clusterA), "c"))
	assert.Error(t, err)
	_, err = h.svc.Update(ctx, m.ID, input(new(clusterA), "c"))
	assert.Error(t, err)
	assert.Error(t, h.svc.Delete(ctx, m.ID))

	// The helpers, each on its own, since a transaction that cannot begin never
	// reaches the second.
	_, err = scopeByName(ctx, st, new(clusterA), "n")
	assert.Error(t, err)
	assert.Error(t, checkCluster(ctx, st, clusterA))
	assert.Error(t, checkName(ctx, st, Memory{Name: "n"}))
	assert.Error(t, checkName(ctx, st, Memory{Name: "n", ClusterID: new(clusterA)}))
	_, err = memoryByID(ctx, st, m.ID)
	assert.Error(t, err)
	_, err = scopeNotes(ctx, st, nil)
	assert.Error(t, err)
	assert.Error(t, checkRoom(ctx, st, Memory{}))
	_, err = deleteMemory(ctx, st, m.ID)
	assert.Error(t, err)
	assert.Error(t, insertMemory(ctx, st, m))
	assert.Error(t, updateMemory(ctx, st, m))
}

func TestAWatchOverAFailedStoreEndsWithItsReason(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.db.Close())

	st, err := h.svc.WatchCollectionByCluster(t.Context(), clusterA)
	require.NoError(t, err)
	for range st.Frames {
	}
	assert.Error(t, st.Err())
}

// A row that does not scan is an error from every read that finds it.
func TestARowThatDoesNotScanIsAnError(t *testing.T) {
	h := newHarness(t)
	h.save(t, clusterA, "n")
	id := h.get(t, clusterA, "n").ID
	unscannable(t, h.db)
	ctx := t.Context()

	_, err := h.svc.Visible(ctx, clusterA)
	assert.ErrorContains(t, err, "scan memory")
	assert.ErrorContains(t, h.svc.Save(ctx, clusterA, "n", "b", ""), "select memory")
	assert.ErrorContains(t, h.svc.Forget(ctx, clusterA, "n"), "select memory")
	_, err = h.svc.Update(ctx, id, input(new(clusterA), "c"))
	assert.ErrorContains(t, err, "select memory")
}
