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

package logs

import (
	"errors"
	"testing"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cache the cluster service removed has no log file to answer from, ever again.
func TestAPageOfARemovedCacheFails(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	require.NoError(t, svc.Remove(int64(f.active.Cache.ID)))

	_, err := svc.Fetch(t.Context(), "7", FetchRequest{Scope: webScope, PageFrom: PageFromFirst, PageSize: 10})
	assert.ErrorIs(t, err, logstore.ErrRemoved)
}

// A page's walks read up to 64 MiB of the file, split evenly across its streams: about
// 640 KiB each at a hundred.
func TestAPagesWalksReadSixtyFourMiB(t *testing.T) {
	t.Parallel()
	svc, _ := serviceOver(t, webMirror(t), newLogServer())

	assert.Equal(t, int64(64<<20), svc.walkBudget)
}

// A pod is gone when the cache's own mirror, read active and trusted, lacks it.
// Anything less sure — another cache active, one still syncing, one that holds no
// pods, a cache the service no longer knows — answers nothing gone, since a stream
// removed is gone for good.
func TestPodsGoneAsksTheCachesMirror(t *testing.T) {
	t.Parallel()
	uids := []string{"uid-web-1", "uid-gone"}
	for name, tc := range map[string]struct {
		change  func(f *fakeService)
		cache   int64
		want    []string
		wantErr string
	}{
		"a cache that cannot be read": {cache: unreadableCache, wantErr: "db closed"},
		"pods that cannot be read": {
			change:  func(f *fakeService) { f.queryErr, f.queryErrOn = errors.New("db closed"), "json_each" },
			wantErr: "db closed",
		},
		"the active cache":     {want: []string{"uid-gone"}},
		"a cache not active":   {cache: 99},
		"a cache that is gone": {cache: goneCache},
		"a cache that syncs":   {change: func(f *fakeService) { f.health = cluster.ClusterCacheHealth{Reason: "Connecting"} }},
		"a cache with no pods": {change: func(f *fakeService) { f.store = newMirror(t).store }},
		"a cache it cannot read": {
			change:  func(f *fakeService) { f.healthErr = errors.New("unreadable") },
			wantErr: "unreadable",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, f := serviceOver(t, webMirror(t), newLogServer())
			if tc.change != nil {
				tc.change(f)
			}
			cache := int64(f.active.Cache.ID)
			if tc.cache != 0 {
				cache = tc.cache
			}

			gone, err := svc.podsGone(t.Context(), cache, uids)

			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, gone)
		})
	}
}

// The service tells its log files which pods are gone, so a sweep removes a gone pod's
// streams that hold nothing, and leaves a live pod's.
func TestASweepRemovesTheStreamsOfGonePods(t *testing.T) {
	t.Parallel()
	svc, f := serviceOver(t, webMirror(t), newLogServer())
	gone := rowOf("web-9", "app")
	seed(t, svc, f, gone)
	seed(t, svc, f, rowOf("web-1", "app"))

	sweepFile(t, svc, f)

	assert.Empty(t, rowsOf(t, svc, f, "web-9"))
	assert.Len(t, rowsOf(t, svc, f, "web-1"), 1)
}
