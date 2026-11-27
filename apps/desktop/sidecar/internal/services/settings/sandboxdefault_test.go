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

package settings

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb/appdbtest"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
)

// sandboxDefaultReader is a Store or a Service.
type sandboxDefaultReader interface {
	GetSandboxDefault(ctx context.Context, cluster apimeta.ClusterID) (SandboxDefault, error)
}

func sandboxDefaultOf(t *testing.T, s sandboxDefaultReader, cluster apimeta.ClusterID) SandboxDefault {
	t.Helper()
	d, err := s.GetSandboxDefault(t.Context(), cluster)
	require.NoError(t, err)
	return d
}

// setDefault writes cluster's default through s and answers the default it
// left.
func setDefault(t *testing.T, s *Store, cluster apimeta.ClusterID, disabled bool) SandboxDefault {
	t.Helper()
	d, err := s.SetSandboxDefault(t.Context(), cluster, disabled)
	require.NoError(t, err)
	return d
}

// resetDefault resets cluster's default through s, expecting it to land on
// disabled, and answers the default it left.
func resetDefault(t *testing.T, s *Store, cluster apimeta.ClusterID, disabled bool) SandboxDefault {
	t.Helper()
	d, err := s.ResetSandboxDefault(t.Context(), cluster, disabled)
	require.NoError(t, err)
	return d
}

// Out of the box, a new chat starts in the sandbox, on every cluster.
func TestANewChatStartsSandboxedByDefault(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	assert.Equal(t, SandboxDefault{}, sandboxDefaultOf(t, s, ""))
	assert.Equal(t, SandboxDefault{}, sandboxDefaultOf(t, s, "dev"))
}

// Every cluster's value is each cluster's until a cluster has its own.
func TestAClustersSandboxDefaultOverridesEveryClusters(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)

	setDefault(t, s, "", true)
	assert.Equal(t, SandboxDefault{Disabled: true}, sandboxDefaultOf(t, s, ""))
	assert.Equal(t, SandboxDefault{Disabled: true}, sandboxDefaultOf(t, s, "dev"))

	assert.Equal(t, SandboxDefault{Own: true}, setDefault(t, s, "dev", false), "the write answers what it left")
	assert.Equal(t, SandboxDefault{Own: true}, sandboxDefaultOf(t, s, "dev"))
	assert.Equal(t, SandboxDefault{Disabled: true}, sandboxDefaultOf(t, s, "prod"))
	assert.Equal(t, SandboxDefault{Disabled: true}, sandboxDefaultOf(t, s, ""))
}

// A reset takes a cluster back to every cluster's value.
func TestAResetClusterSandboxDefaultFollowsEveryClusters(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	setDefault(t, s, "", true)
	setDefault(t, s, "dev", false)

	assert.Equal(t, SandboxDefault{Disabled: true}, resetDefault(t, s, "dev", true))
	assert.Equal(t, SandboxDefault{Disabled: true}, sandboxDefaultOf(t, s, "dev"))
}

// A reset that would land on a default other than the one its caller saw
// writes nothing: the caller decided whether to confirm on what it saw.
func TestASandboxDefaultResetThatLandsElsewhereIsRefused(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	setDefault(t, s, "", true)
	setDefault(t, s, "dev", false)
	changes := s.ListenForSandboxDefaultChanges()
	t.Cleanup(changes.Close)

	_, err := s.ResetSandboxDefault(t.Context(), "dev", false)
	assert.ErrorIs(t, err, ErrSandboxDefaultChanged)
	assert.Equal(t, SandboxDefault{Own: true}, sandboxDefaultOf(t, s, "dev"), "the cluster keeps its own")
	// A negative assertion: a notice is sent after the commit, and a refused
	// reset commits nothing.
	testutil.NoRecv(t, changes.Chan(), 20*time.Millisecond, "a refused reset's notice")
}

// A value Kubetail cannot read starts a new chat in the sandbox, whichever
// scope holds it.
func TestASandboxDefaultKubetailCannotReadIsSandboxed(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	setDefault(t, s, "", true)
	exec(t, db, `INSERT INTO settings (key, cluster_id, value, created_at, updated_at) VALUES ('sandbox.disabled', 'dev', 'yes', 0, 0)`)

	assert.Equal(t, SandboxDefault{Own: true}, sandboxDefaultOf(t, s, "dev"))

	exec(t, db, `UPDATE settings SET value = 7 WHERE key = 'sandbox.disabled' AND cluster_id IS NULL`)
	assert.Equal(t, SandboxDefault{}, sandboxDefaultOf(t, s, ""))
}

// A cluster that is gone, or awaiting deletion, takes no value.
func TestASandboxDefaultForAGoneClusterIsRefused(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	exec(t, db, `UPDATE clusters SET delete_requested_at = 1 WHERE id = 'prod'`)

	_, err := s.SetSandboxDefault(ctx, "gone", true)
	assert.ErrorIs(t, err, ErrClusterGone)
	_, err = s.SetSandboxDefault(ctx, "prod", true)
	assert.ErrorIs(t, err, ErrClusterGone)
	_, err = s.ResetSandboxDefault(ctx, "gone", false)
	assert.ErrorIs(t, err, ErrClusterGone)
	assert.Equal(t, 0, settingsCount(t, db), "nothing was written")
}

// Each write notifies its watchers, after its commit.
func TestASandboxDefaultWriteNotifies(t *testing.T) {
	t.Parallel()
	s, _ := storeWithClusters(t)
	changes := s.ListenForSandboxDefaultChanges()
	t.Cleanup(changes.Close)

	setDefault(t, s, "", true)
	testutil.Recv(t, changes.Chan(), "the write of every cluster's")
	resetDefault(t, s, "dev", true)
	testutil.Recv(t, changes.Chan(), "the reset")
}

// A read that fails is an error, never sandboxed: a caller that starts a chat
// must not start it on a guess.
func TestASandboxDefaultReadThatFailsIsAnError(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	s := openStore(t, db)
	exec(t, db, `DROP TABLE settings`)

	_, err := s.GetSandboxDefault(t.Context(), "")
	assert.Error(t, err)
	_, err = s.SetSandboxDefault(t.Context(), "", true)
	assert.Error(t, err)
}

// A machine with no sandbox has no default to keep: every new chat starts as
// it can, a stored value is ignored and a change is refused.
func TestASandboxDefaultWithoutASandbox(t *testing.T) {
	t.Parallel()
	db := appdbtest.Open(t)
	seedCluster(t, db, "dev", "dev-eks")
	store := openStore(t, db)
	setDefault(t, store, "", true)
	s := NewService(store, nil, nil, nil, "")

	assert.Equal(t, SandboxDefault{}, sandboxDefaultOf(t, s, "dev"))
	_, err := s.SetSandboxDefault(t.Context(), "dev", true)
	assert.ErrorIs(t, err, ErrNoSandbox)
	_, err = s.ResetSandboxDefault(t.Context(), "dev", false)
	assert.ErrorIs(t, err, ErrNoSandbox)
}

// agent/chat reads the default through what this package exports, in its own
// transaction.
func TestScanSandboxDefaultReadsAClustersValue(t *testing.T) {
	t.Parallel()
	s, db := storeWithClusters(t)
	ctx := t.Context()
	setDefault(t, s, "", true)
	setDefault(t, s, "dev", false)

	for cluster, want := range map[apimeta.ClusterID]bool{"dev": false, "prod": true, "": true} {
		rows, err := db.Read.QueryContext(ctx, SelectSandboxDefault, SandboxDefaultArgs(cluster)...)
		require.NoError(t, err)
		got, err := ScanSandboxDefault(rows, cluster)
		require.NoError(t, err)
		assert.Equal(t, want, got, cluster)
	}
}
