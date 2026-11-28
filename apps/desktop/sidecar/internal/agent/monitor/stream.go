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
	"slices"
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
	read := func(ctx context.Context) ([]IncidentOccurrence, error) { return m.store.servedOccurrences(ctx, id) }
	frame := func(o IncidentOccurrence) IncidentOccurrenceEventFeedFrame {
		return IncidentOccurrenceEventFeedFrame{Type: IncidentOccurrenceEventFeedFrameOccurrence, IncidentID: id, Occurrence: &o}
	}
	bookmark := IncidentOccurrenceEventFeedFrame{Type: IncidentOccurrenceEventFeedFrameBookmark, IncidentID: id}
	return stream(ctx, m, func(ctx context.Context, out chan<- IncidentOccurrenceEventFeedFrame) error {
		return pumpEventFeed(ctx, m, out, read, servedOccurrenceID, frame, bookmark, true, appdb.KeyIncidents)
	})
}

// SubscribeAlertEventFeed streams the retained alerts, the newest keepAlerts, in
// the order they committed, one Bookmark, then each alert as it is raised, once.
// Only an incidents commit raises one, so it re-reads on that key alone.
func (m *Monitor) SubscribeAlertEventFeed(ctx context.Context) (*Stream[AlertEventFeedFrame], error) {
	// An alert at or below the newest one read committed before it, so it is not
	// new however it reaches the read: one returns into the newest keepAlerts
	// when a newer incident is deleted. The pump reads on one goroutine.
	var newest int64
	read := func(ctx context.Context) ([]Alert, error) {
		alerts, err := m.store.servedAlerts(ctx)
		if err != nil {
			return nil, err
		}
		since := newest
		alerts = slices.DeleteFunc(alerts, func(a Alert) bool { return a.Seq <= since })
		if len(alerts) > 0 {
			newest = alerts[len(alerts)-1].Seq
		}
		return alerts, nil
	}
	frame := func(a Alert) AlertEventFeedFrame {
		return AlertEventFeedFrame{Type: AlertEventFeedFrameAlert, Alert: &a}
	}
	bookmark := AlertEventFeedFrame{Type: AlertEventFeedFrameBookmark}
	return stream(ctx, m, func(ctx context.Context, out chan<- AlertEventFeedFrame) error {
		return pumpEventFeed(ctx, m, out, read, servedAlertID, frame, bookmark, false, appdb.KeyIncidents)
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

func servedOccurrenceID(o IncidentOccurrence) IncidentOccurrenceID { return o.ID }

func servedAlertID(a Alert) IncidentID { return a.IncidentID }

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

// pumpEventFeed is an event feed over read: listen on keys, then read, each
// record and one Bookmark, then on each notice each record not sent yet, and,
// when resendChanged, each one that changed since it was sent. One that leaves
// the read is dropped unannounced.
func pumpEventFeed[K comparable, T comparable, F any](
	ctx context.Context, m *Monitor, out chan<- F, read func(context.Context) ([]T, error),
	key func(T) K, frame func(T) F, bookmark F, resendChanged bool, keys ...string,
) error {
	rx := m.store.db.ListenForChanges(keys...)
	defer rx.Close()

	sent := map[K]T{}
	send := func(records []T) bool {
		kept := make(map[K]T, len(records))
		for _, r := range records {
			k := key(r)
			kept[k] = r
			if prev, ok := sent[k]; ok && (prev == r || !resendChanged) {
				continue
			}
			if !deltafold.Send(ctx, out, frame(r)) {
				return false
			}
		}
		sent = kept
		return true
	}

	records, err := read(ctx)
	if err != nil {
		return err
	}
	if !send(records) || !deltafold.Send(ctx, out, bookmark) {
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
		if !send(records) {
			return nil
		}
	}
}
