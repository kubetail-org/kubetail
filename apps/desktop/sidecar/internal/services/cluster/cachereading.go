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
	"strings"
)

var (
	// ErrNoCache is a cluster with no cache to read: its record gone, its identity
	// moving under both readings, or its cache gone under the reads.
	ErrNoCache = errors.New("cluster: the cluster has no cache")
	// ErrSyncing is a cache whose rows are not trusted: an object missing from it
	// may still exist.
	ErrSyncing = errors.New("cluster: the cache is syncing")
)

// CacheReading runs the sidecar's own statements over one snapshot of one cache inside
// one reading of its cluster, each answer cut at the reading's row and byte bounds.
type CacheReading struct {
	query    OwnQuery
	maxRows  int
	maxBytes int
	health   ClusterCacheHealth
}

// OwnQuery runs one statement of a reading CachedData().ReadOwn holds open.
type OwnQuery func(sql string, maxRows, maxBytes int, args ...any) (ClusterCachedDataQueryResult, error)

// ReadActiveCache is CachedData().ReadActive over svc. Exported because a fake
// CachedData outside this package answers ReadActive through it, over its own
// readings, health and query.
func ReadActiveCache(ctx context.Context, svc Service, id ClusterID, serverUID string, maxRows, maxBytes int, read func(CacheReading, ActiveCluster) error) error {
	err := svc.Clusters().ReadActive(ctx, id, func(ctx context.Context, active ActiveCluster) error {
		// First: another server's cache, its health and its rows are none of a
		// pinned reading's business.
		if !active.ReadsServer(serverUID) {
			return ErrIdentityMismatch
		}
		if active.Cache == nil {
			return ErrNoCache
		}
		health, ok, err := svc.Caches().Health(ctx, id, active.Cache.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoCache
		}
		if !health.Trusted() {
			return ErrSyncing
		}
		ok, err = svc.CachedData().ReadOwn(ctx, id, active.Cache.ID, func(query OwnQuery) error {
			return read(CacheReading{query, maxRows, maxBytes, health}, active)
		})
		if err == nil && !ok {
			return ErrNoCache
		}
		return err
	})
	err = PinnedReadErr(serverUID, err)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrIdentityMoved) {
		return ErrNoCache
	}
	return err
}

// Health is the cache's health the reading was admitted under, read just before
// its snapshot: which kinds were watching, paused or behind.
func (r CacheReading) Health() ClusterCacheHealth { return r.health }

// Query is sql's answer, with args bound to its placeholders; a cache gone under
// the read is ErrNoCache.
func (r CacheReading) Query(sql string, args ...any) (ClusterCachedDataQueryResult, error) {
	return r.query(sql, r.maxRows, r.maxBytes, args...)
}

// Placeholders is n comma-separated placeholders, for an IN list of n values.
func Placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// CellText is a cell as text, "" for any other.
func CellText(v any) string {
	s, _ := v.(string)
	return s
}

// CellInteger is a cell as an integer, 0 for any other.
func CellInteger(v any) int64 {
	n, _ := v.(int64)
	return n
}
