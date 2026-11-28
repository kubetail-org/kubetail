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

package cluster

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readingService is the cluster service as ReadActiveCache reads it: one reading, its cache's
// health, and a query answering rows.
type readingService struct {
	Service
	readErr   error
	active    ActiveCluster
	health    ClusterCacheHealth
	noHealth  bool
	healthErr error
	noQuery   bool
	// asked is the statement, bounds and args the query was handed.
	asked struct {
		sql              string
		maxRows, maxByte int
		args             []any
	}
}

func (f *readingService) Clusters() Clusters     { return readingClusters{f: f} }
func (f *readingService) Caches() Caches         { return readingCaches{f: f} }
func (f *readingService) CachedData() CachedData { return readingData{f: f} }

type readingClusters struct {
	Clusters
	f *readingService
}

func (fc readingClusters) ReadActive(ctx context.Context, _ ClusterID, read func(context.Context, ActiveCluster) error) error {
	if fc.f.readErr != nil {
		return fc.f.readErr
	}
	return read(ctx, fc.f.active)
}

type readingCaches struct {
	Caches
	f *readingService
}

func (fc readingCaches) Health(context.Context, ClusterID, ClusterCacheID) (ClusterCacheHealth, bool, error) {
	return fc.f.health, !fc.f.noHealth, fc.f.healthErr
}

type readingData struct {
	CachedData
	f *readingService
}

func (fd readingData) ReadOwn(_ context.Context, _ ClusterID, _ ClusterCacheID, read func(OwnQuery) error) (bool, error) {
	if fd.f.noQuery {
		return false, nil
	}
	return true, read(func(sql string, maxRows, maxBytes int, args ...any) (ClusterCachedDataQueryResult, error) {
		fd.f.asked.sql, fd.f.asked.maxRows, fd.f.asked.maxByte, fd.f.asked.args = sql, maxRows, maxBytes, args
		return ClusterCachedDataQueryResult{Rows: [][]any{{"a", int64(2)}}}, nil
	})
}

// watching is a service whose active cache is live.
func watchingService() *readingService {
	return &readingService{
		active: ActiveCluster{Cache: &ClusterCache{RecordMeta: RecordMeta{ID: 41}}},
		health: ClusterCacheHealth{Reason: "Watching"},
	}
}

// ReadActiveCache hands a trusted cache's reading to read, whose statements run
// within its bounds with their args.
func TestReadActiveCacheRunsOverATrustedCache(t *testing.T) {
	t.Parallel()

	svc := watchingService()
	var rows [][]any
	err := ReadActiveCache(t.Context(), svc, "7", 10, 20, func(r CacheReading, active ActiveCluster) error {
		assert.Equal(t, ClusterCacheID(41), active.Cache.ID)
		assert.Equal(t, svc.health, r.Health(), "the health the reading was admitted under")
		res, err := r.Query("SELECT ?", "x")
		rows = res.Rows
		return err
	})

	require.NoError(t, err)
	assert.Equal(t, [][]any{{"a", int64(2)}}, rows)
	assert.Equal(t, "SELECT ?", svc.asked.sql)
	assert.Equal(t, []any{"x"}, svc.asked.args)
	assert.Equal(t, 10, svc.asked.maxRows)
	assert.Equal(t, 20, svc.asked.maxByte)
}

// ReadActiveCache is ErrNoCache for a cluster with no cache, a record gone or an
// identity that moved, and a cache gone under the read; ErrSyncing for a cache
// whose rows are not trusted; and any other failure as it is.
func TestReadActiveCacheRefusesWhatHasNoTrustedRows(t *testing.T) {
	t.Parallel()

	failed := errors.New("db closed")
	for name, tc := range map[string]struct {
		change func(*readingService)
		want   error
	}{
		"no cache":       {func(f *readingService) { f.active.Cache = nil }, ErrNoCache},
		"record gone":    {func(f *readingService) { f.readErr = ErrNotFound }, ErrNoCache},
		"identity moved": {func(f *readingService) { f.readErr = ErrIdentityMoved }, ErrNoCache},
		"no health":      {func(f *readingService) { f.noHealth = true }, ErrNoCache},
		"cache gone":     {func(f *readingService) { f.noQuery = true }, ErrNoCache},
		"syncing":        {func(f *readingService) { f.health.Reason = "Connecting" }, ErrSyncing},
		"health failed":  {func(f *readingService) { f.healthErr = failed }, failed},
		"reading failed": {func(f *readingService) { f.readErr = failed }, failed},
	} {
		t.Run(name, func(t *testing.T) {
			svc := watchingService()
			tc.change(svc)

			err := ReadActiveCache(t.Context(), svc, "7", 10, 20, func(r CacheReading, _ ActiveCluster) error {
				_, err := r.Query("SELECT 1")
				return err
			})

			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestPlaceholdersIsOnePerValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "?", Placeholders(1))
	assert.Equal(t, "?, ?, ?", Placeholders(3))
}

// A cell is read as its type, and as the zero value when it is another.
func TestCells(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "x", CellText("x"))
	assert.Empty(t, CellText(int64(1)))
	assert.Equal(t, int64(3), CellInteger(int64(3)))
	assert.Zero(t, CellInteger("3"))
}
