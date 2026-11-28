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

package monitor

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/appdb"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/deltafold"
)

// ErrStopping is a subscription opened as the monitor stops.
var ErrStopping = errors.New("monitor: stopping")

// Stream is a subscription: its frames, and why they stopped. Frames closes on
// every exit, so Err is what tells a failure from an ordinary teardown. The same
// shape as memory.Stream.
type Stream[T any] struct {
	Frames <-chan T
	err    atomic.Pointer[error]
}

// Err returns why the stream ended, or nil if it ended cleanly. Read it once
// Frames has closed.
func (s *Stream[T]) Err() error {
	if p := s.err.Load(); p != nil {
		return *p
	}
	return nil
}

// WatchTargetCollection streams every target of a registered integration, then
// each change to what it serves. A target's delete arrives as Deleted.
func (m *Monitor) WatchTargetCollection(ctx context.Context) (*Stream[MonitorTargetCollectionWatchFrame], error) {
	return stream(ctx, m, func(ctx context.Context, out chan<- MonitorTargetCollectionWatchFrame) error {
		return pumpCollection(ctx, m, out, m.servedTargets, servedTargetID, targetFrame, appdb.KeyMonitorTargets)
	})
}

// WatchIncidentCollection streams every incident of a target the targets watch
// serves, then each change to what it serves. It re-reads on the targets key too,
// whose delete cascades to their incidents.
func (m *Monitor) WatchIncidentCollection(ctx context.Context) (*Stream[IncidentCollectionWatchFrame], error) {
	return stream(ctx, m, func(ctx context.Context, out chan<- IncidentCollectionWatchFrame) error {
		return pumpCollection(ctx, m, out, m.store.servedIncidents, servedIncidentID, incidentFrame,
			appdb.KeyIncidents, appdb.KeyMonitorTargets)
	})
}

// SubscribeIncidentOccurrenceEventFeed streams an incident's retained
// occurrences, highest number first, one Bookmark, then each occurrence that
// starts or ends. An incident the incidents watch leaves out has none, and its
// feed sends nothing once deleted.
func (m *Monitor) SubscribeIncidentOccurrenceEventFeed(ctx context.Context, id IncidentID) (*Stream[IncidentOccurrenceEventFeedFrame], error) {
	return stream(ctx, m, func(ctx context.Context, out chan<- IncidentOccurrenceEventFeedFrame) error {
		return m.pumpOccurrences(ctx, id, out)
	})
}

func servedTargetID(t MonitorTarget) MonitorTargetID { return t.ID }

func targetFrame(typ apimeta.CollectionWatchFrameType, t MonitorTarget) MonitorTargetCollectionWatchFrame {
	if typ == apimeta.CollectionWatchFrameBookmark {
		return MonitorTargetCollectionWatchFrame{Type: typ}
	}
	return MonitorTargetCollectionWatchFrame{Type: typ, Target: &t}
}

func servedIncidentID(i Incident) IncidentID { return i.ID }

func incidentFrame(typ apimeta.CollectionWatchFrameType, i Incident) IncidentCollectionWatchFrame {
	if typ == apimeta.CollectionWatchFrameBookmark {
		return IncidentCollectionWatchFrame{Type: typ}
	}
	return IncidentCollectionWatchFrame{Type: typ, Incident: &i}
}

// servedTargets is every served target, each derived against its registration.
func (m *Monitor) servedTargets(ctx context.Context) ([]MonitorTarget, error) {
	rows, err := m.store.servedTargets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]MonitorTarget, 0, len(rows))
	for _, row := range rows {
		t, err := m.servedTarget(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// stream runs pump on a goroutine stop joins, filing its error on the stream.
func stream[T any](ctx context.Context, m *Monitor, pump func(context.Context, chan<- T) error) (*Stream[T], error) {
	if err := m.enter(); err != nil {
		return nil, err
	}
	ch := make(chan T, 1)
	st := &Stream[T]{Frames: ch}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	go func() {
		defer m.wg.Done()
		defer close(ch)
		defer stop()
		defer cancel()
		if err := pump(ctx, ch); err != nil && ctx.Err() == nil {
			st.err.Store(&err)
		}
	}()
	return st, nil
}

// pumpCollection is a collection watch over read: listen on keys, then read, a
// snapshot, then a re-read and a diff on each notice.
func pumpCollection[K comparable, T comparable, F any](
	ctx context.Context, m *Monitor, out chan<- F, read func(context.Context) ([]T, error),
	key func(T) K, frame func(apimeta.CollectionWatchFrameType, T) F, keys ...string,
) error {
	// Listen, then read: a snapshot taken first would miss a commit in between.
	rx := m.store.db.ListenForChanges(keys...)
	defer rx.Close()

	f := deltafold.New(key, deltafold.Equal[T], frame)
	records, err := read(ctx)
	if err != nil {
		return err
	}
	if !f.Snapshot(ctx, out, records) {
		return nil
	}
	for {
		if _, err := rx.RecvContext(ctx); err != nil {
			return nil
		}
		records, err := read(ctx)
		if err != nil {
			return err
		}
		if !f.Diff(ctx, out, records) {
			return nil
		}
	}
}

// pumpOccurrences is the occurrence feed: listen, read, the snapshot and its
// Bookmark, then on each notice every occurrence that is new or changed. One
// trimmed or deleted is dropped unannounced.
func (m *Monitor) pumpOccurrences(ctx context.Context, id IncidentID, out chan<- IncidentOccurrenceEventFeedFrame) error {
	rx := m.store.db.ListenForChanges(appdb.KeyIncidents)
	defer rx.Close()

	sent := map[IncidentOccurrenceID]IncidentOccurrence{}
	send := func(occurrences []IncidentOccurrence) bool {
		kept := make(map[IncidentOccurrenceID]IncidentOccurrence, len(occurrences))
		for _, o := range occurrences {
			kept[o.ID] = o
			if prev, ok := sent[o.ID]; ok && prev == o {
				continue
			}
			frame := IncidentOccurrenceEventFeedFrame{Type: IncidentOccurrenceEventFeedFrameOccurrence, IncidentID: id, Occurrence: &o}
			if !deltafold.Send(ctx, out, frame) {
				return false
			}
		}
		sent = kept
		return true
	}

	occurrences, err := m.store.servedOccurrences(ctx, id)
	if err != nil {
		return err
	}
	bookmark := IncidentOccurrenceEventFeedFrame{Type: IncidentOccurrenceEventFeedFrameBookmark, IncidentID: id}
	if !send(occurrences) || !deltafold.Send(ctx, out, bookmark) {
		return nil
	}
	for {
		if _, err := rx.RecvContext(ctx); err != nil {
			return nil
		}
		occurrences, err := m.store.servedOccurrences(ctx, id)
		if err != nil {
			return err
		}
		if !send(occurrences) {
			return nil
		}
	}
}
