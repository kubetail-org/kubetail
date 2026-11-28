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
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/kubelet"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logstore"
)

// Service reads views' lines off the clusters the cluster service connects to, and
// keeps them in each cluster cache's log file.
type Service struct {
	clusters cluster.Service
	files    *logstore.Manager
	// backends are the cluster's backends in the order a gap is offered to them,
	// each built over a request's claim: the kubelet, or a test's fakes. A tail
	// tails through the first.
	backends []namedBackend
	now      func() time.Time
	// retry is how long a tail waits to resolve its view again after the mirror
	// refused it, to open its watches again or to tail a dropped stream again,
	// retryCap the longest a stream that keeps dropping waits, settle how long it
	// waits after a pod frame.
	retry    time.Duration
	retryCap time.Duration
	settle   time.Duration
	// caps is the limits a page's and a tail's fills keep to: DefaultQuota, or a
	// test's.
	caps kubelet.Quota
	// walkBudget is the bytes one page's walks of the file may read, apart from what
	// its fills downloaded.
	walkBudget int64
	// grace is how long a tailer runs on after its last tail lets go: the files'
	// TailGrace, or a test's.
	grace time.Duration

	// afterFill runs once a page's or a tail's fills have committed and before its
	// walk: a test's, so a sweep can land between them; nil in production.
	afterFill func()

	// tails is each log file's tailers, by cache id.
	tailsMu sync.Mutex
	tails   map[cluster.ClusterCacheID]*registry
}

// namedBackend is one of the cluster's backends: its name, which a chunk records as
// its backend, and how to build it over a claim's get.
type namedBackend struct {
	name  string
	build func(get kubelet.Getter) kubelet.Backend
}

// New is the service over the cluster service, keeping its log files under dir to
// ret, whose sweeps it tells which pods are gone. It owes a Close.
func New(clusters cluster.Service, dir string, ret logstore.Retention) *Service {
	files := logstore.NewManager(dir, ret)
	s := &Service{
		clusters: clusters, files: files, backends: []namedBackend{{"kubelet", kubelet.New}}, now: time.Now,
		retry: tailRetry, retryCap: tailRetryCap, settle: tailSettle,
		caps: kubelet.DefaultQuota, walkBudget: maxWalkBudget, grace: ret.TailGrace,
		tails: map[cluster.ClusterCacheID]*registry{},
	}
	files.SetPodsGone(s.podsGone)
	return s
}

// Close closes the log files. The cluster service's cache pass may still clear or
// remove one after it.
func (s *Service) Close() error { return s.files.Close() }

// Clear empties the cache cacheID's log file, and Remove deletes it for good: the
// cluster service calls them with the cache's clear and removal.
func (s *Service) Clear(cacheID int64) error  { return s.files.Clear(cacheID) }
func (s *Service) Remove(cacheID int64) error { return s.files.Remove(cacheID) }

var _ cluster.CacheFileOwner = (*Service)(nil)

// podsGone answers which of uids the mirror of the cache cacheID no longer holds,
// for the log file's sweep and for a page's gone pods. It answers none unless that
// cache is its cluster's active one, trusted and mirroring pods: a stream the sweep
// removes is gone for good, so a pod is gone only when the mirror is sure.
func (s *Service) podsGone(ctx context.Context, cacheID int64, uids []string) ([]string, error) {
	cache, err := s.clusters.Caches().Get(ctx, cluster.ClusterCacheID(cacheID))
	if errors.Is(err, cluster.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	arg, _ := json.Marshal(uids) // a list of strings always marshals
	var gone []string
	err = s.clusters.CachedData().ReadActive(ctx, cache.Spec.ClusterID, "", len(uids)+1, maxBytes, func(q cluster.CacheReading, active cluster.ActiveCluster) error {
		// Set whole, never appended to: ReadActive may run this twice.
		gone = nil
		r := reading{q}
		if active.Cache.ID != cache.ID {
			return nil
		}
		mirrored, err := r.mirrors(mirrorKinds[SourcePod])
		if err != nil || !mirrored {
			return err
		}
		rows, err := r.rows(`SELECT uid FROM objects WHERE api_version = 'v1' AND kind = 'Pod' AND uid IN (SELECT value FROM json_each(?))`, string(arg))
		if err != nil {
			return err
		}
		held := map[string]bool{}
		for _, row := range rows {
			held[cluster.CellText(row[0])] = true
		}
		for _, uid := range uids {
			if !held[uid] {
				gone = append(gone, uid)
			}
		}
		return nil
	})
	if errors.Is(err, cluster.ErrNoCache) || errors.Is(err, cluster.ErrSyncing) {
		return nil, nil
	}
	return gone, err
}
