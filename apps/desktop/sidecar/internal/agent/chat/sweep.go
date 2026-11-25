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

// The chat sweeper: what deletes the chats of a cluster marked for deletion, and
// ends the monitor of a cluster that is marked or gone, on the clusters signal
// and on its own retry.
package chat

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"time"

	"github.com/amorey/gobus/conflate"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/rootdir"
)

// startSweeper launches the chat sweeper on a goroutine the stop func joins. It
// subscribes before the first sweep, so a cluster marked under that sweep is
// taken on the signal rather than missed.
func (s *service) startSweeper() error {
	clusters := s.db.ListenForChanges(appdb.KeyClusters)
	if err := s.enter(); err != nil {
		clusters.Close()
		return err
	}
	go s.sweepLoop(clusters)
	return nil
}

// sweepLoop deletes the chats of every marked cluster: once at startup, on every
// clusters signal, and again after its own delay when a sweep failed — no signal
// follows a failure, since the mirror cannot remove a row while chats remain and
// notifies only on removal. After the first, it sweeps the chats' directory
// once: here rather than in Start, since the first request waits on Start, and
// after that sweep, since a gone chat's workspace can be large. It owes the
// wg.Done startSweeper's enter took.
func (s *service) sweepLoop(clusters *conflate.Receiver[string, struct{}]) {
	defer s.wg.Done()
	defer clusters.Close()
	retry := time.NewTimer(s.sweepRetry)
	retry.Stop()
	for first := true; ; first = false {
		if err := s.sweep(); err != nil && s.ctx.Err() == nil {
			slog.Warn("chat sweep failed; will retry", "err", err)
			retry.Reset(s.sweepRetry)
		}
		if first {
			s.sweepChatDirs()
		}
		if s.onSwept != nil {
			select {
			case s.onSwept <- struct{}{}:
			case <-s.ctx.Done():
				return
			}
		}
		select {
		case <-s.ctx.Done():
			return
		case _, ok := <-clusters.Chan():
			if !ok {
				return
			}
		case <-retry.C:
		}
	}
}

// sweep runs one pass: the monitors of every cluster that is not a live row,
// then the chats of every marked cluster. The monitors go first, so a chat
// delete that fails does not leave one running until the retry. It notifies
// nothing itself: each delete that removes a row pings the clusters key, and a
// no-op scan notifying would wake the mirror, whose own pass would wake this,
// forever.
func (s *service) sweep() error {
	errs := []error{s.sweepMonitors()}
	ids, err := markedClusterIDs(s.ctx, s.store.Stmts())
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, id := range ids {
		if _, err := s.deleteByCluster(s.ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sweepMonitors ends every monitor run, and removes every monitor directory,
// whose cluster is not a live row. It keys on the live rows rather than the
// marked ones: the mirror can delete a marked row with no chats before any pass
// sees the mark, and a monitor run holds no row back. The directory is listed
// and the slots copied before the rows are read, so neither an entry nor a run
// made meanwhile is taken by mistake: a run is reserved for a cluster whose row
// already exists, so the read sees it unless it has gone since. A run still in
// its slot once the others are joined keeps its directory: a run uses it only
// past its insert, which a mark is serialized with, and the mark's signal
// brings the pass that ends it.
func (s *service) sweepMonitors() error {
	entries, err := fs.ReadDir(s.monitorRoot.FS(), ".")
	if err != nil {
		slog.Warn("could not list the monitor's directory", "err", err)
	}
	s.turnsMu.Lock()
	slots := maps.Clone(s.monitors)
	s.turnsMu.Unlock()
	if s.onSlotsCopied != nil {
		s.onSlotsCopied()
	}
	ids, err := liveClusterIDs(s.ctx, s.store.Stmts())
	if err != nil {
		return err
	}
	if s.onLiveRead != nil {
		s.onLiveRead()
	}
	live := make(map[apimeta.ClusterID]bool, len(ids))
	for _, id := range ids {
		live[id] = true
	}
	gone := map[apimeta.ClusterID]*monitor{}
	for id, m := range slots {
		if !live[id] {
			gone[id] = m
		}
	}
	for _, m := range gone {
		m.cancel()
	}
	// Each run has settled before its directory goes.
	for id, m := range gone {
		<-m.done
		s.monitorDir(id).remove()
	}
	s.turnsMu.Lock()
	held := maps.Clone(s.monitors)
	s.turnsMu.Unlock()
	rootdir.Sweep(s.monitorRoot, entries, func(e fs.DirEntry) bool {
		id := apimeta.ClusterID(e.Name())
		return live[id] || held[id] != nil
	})
	return nil
}

// deleteByCluster deletes every chat filed under the cluster, each as Delete would,
// and reports how many went. The first error stops it: the chats already deleted
// stay deleted, and the sweeper's retry takes the rest.
func (s *service) deleteByCluster(ctx context.Context, clusterID apimeta.ClusterID) (int, error) {
	ids, err := chatIDsByCluster(ctx, s.store.Stmts(), clusterID)
	if err != nil {
		return 0, err
	}
	for n, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return n, err
		}
	}
	return len(ids), nil
}

// sweepChatDirs removes every entry of the chats' directory that is not a
// chat's. It lists the directory before reading the chats: a send can run
// before Start, and it commits its chat's row before its turn writes anything,
// so a directory the listing saw belongs to a row the read sees. A failure is
// logged, and the next start tries again.
func (s *service) sweepChatDirs() {
	entries, err := fs.ReadDir(s.chatsRoot.FS(), ".")
	if err != nil {
		slog.Warn("could not list the chats' directory", "err", err)
		return
	}
	chats, err := listChats(s.ctx, s.store.Stmts())
	if err != nil {
		if s.ctx.Err() == nil {
			slog.Warn("could not read the chats to sweep their directories", "err", err)
		}
		return
	}
	live := make(map[ChatID]bool, len(chats))
	for _, c := range chats {
		live[c.ID] = true
	}
	rootdir.Sweep(s.chatsRoot, entries, func(e fs.DirEntry) bool { return live[ChatID(e.Name())] })
}
