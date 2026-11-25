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

//go:build debug

package cluster

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubeconn"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubestore"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster/internal/kubesync"
)

// store=fail fails the open kubesync arms a cache with, which the cache reads as StoreFailed.
// Every other reader keeps the real manager.
func TestAStoreFaultFailsEveryCachesStore(t *testing.T) {
	svc, err := New(openTestDB(t), testPaths(t), newTestKubeconfig(t), nil, WithFaults("store=fail"))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, svc.Close()) })
	s := svc.(*service)

	s.kubesyncSvc.TrackDiscovery(1, kubesync.Params{ContextName: "prod", ServerUID: "uid-1"})

	state, ok := s.kubesyncSvc.GetDiscoveryState(1)
	require.True(t, ok)
	assert.Equal(t, kubesync.ReasonStoreFailed, state.Reason)
	assert.Contains(t, state.Message, "injected by KUBETAIL_FAULTS")

	store, err := s.kubestoreMgr.(*kubestore.Manager).OpenOrCreate(1)
	require.NoError(t, err, "the readers' manager opens")
	store.Release()
}

func TestNoStoreFaultHandsKubesyncTheManager(t *testing.T) {
	mgr := kubestore.NewManager(t.TempDir(), kubestore.Retention{})
	t.Cleanup(func() { _ = mgr.Close() })

	assert.Same(t, mgr, syncStores(mgr, kubeconn.Faults{}))
}
