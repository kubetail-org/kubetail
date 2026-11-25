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

// The event log every record kind shares: the point read, the event feed, and the
// projection between them. Not a kind's file and not a family's, because an event
// carries no kind — beehive reads a timeline by id alone, so one code path serves
// Cluster, ClusterCache and ClusterCachedKind alike.
package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/amorey/beehive"
)

// toEvent projects one stored run into the served record.
func toEvent(ev beehive.Event) Event {
	return Event{
		ID:       ObjectID(ev.ID),
		Category: ev.Category,
		Type:     ev.Type,
		Reason:   ev.Reason,
		Message:  ev.Message,
		Count:    ev.Count,
		FirstAt:  ev.FirstAt,
		LastAt:   ev.LastAt,
	}
}

// eventOptions converts the two optional arguments the schema declares.
//
// A nil category adds NO option: WithEventCategory("") selects beehive's default
// timeline, which is a timeline of its own and not "unfiltered" — and every write in
// this package carries connection, discovery or sync, so passing the empty string
// would answer nothing rather than everything.
//
// A nil limit is deliberately unbounded. beehive is built WithEventRetention(
// maxEventRuns, 0), so each (object, category) timeline is already capped; an
// unfiltered read is maxEventRuns times the categories that object writes, at most
// three.
func eventOptions(category *string, limit *int) []beehive.EventOption {
	var opts []beehive.EventOption
	if category != nil {
		opts = append(opts, beehive.WithEventCategory(*category))
	}
	if limit != nil {
		opts = append(opts, beehive.WithEventLimit(*limit))
	}
	return opts
}

func (s *service) ListEvents(ctx context.Context, id ObjectID, category *string, limit *int) ([]Event, error) {
	runs, err := s.clusterClient.ListEvents(ctx, beehive.ObjectID(id), eventOptions(category, limit)...)
	if err != nil {
		return nil, fmt.Errorf("list object %d events: %w", id, err)
	}
	events := make([]Event, 0, len(runs))
	for _, run := range runs {
		events = append(events, toEvent(run))
	}
	return events, nil
}

func (s *service) SubscribeObjectEventFeed(ctx context.Context, id ObjectID, category *string) (*Stream[EventFeedFrame], error) {
	// Any registered kind's client serves any id's timeline: beehive checks
	// registration against the CALLER's kind, not the target's, and the read itself is
	// kind-agnostic. The cluster client is picked because every id reaches this method
	// and one of the three has to be named.
	//
	// Synchronous, ahead of NewStream: a subscribe that fails is an error the resolver
	// can answer with, rather than a terminal frame on a stream the client already holds.
	src, err := s.clusterClient.WatchEvents(ctx, beehive.ObjectID(id), eventOptions(category, nil)...)
	if err != nil {
		return nil, fmt.Errorf("watch object %d events: %w", id, err)
	}
	return NewStream(ctx, func(ctx context.Context, out chan<- EventFeedFrame) error {
		// Forwarded in beehive's order — newest-first — because the client upserts by
		// Event.ID rather than appending.
		for _, ev := range src.Runs {
			if !sendFrame(ctx, out, runFrame(ev)) {
				return nil
			}
		}
		if !sendFrame(ctx, out, EventFeedFrame{Type: EventFeedFrameBookmark}) {
			return nil
		}
		for ev := range src.Events {
			if !sendFrame(ctx, out, runFrame(ev)) { // coverage:ignore only the consumer leaving while a run is in flight, which the store's own watch races
				return nil
			}
		}
		return terminalErr(src.Err())
	}), nil
}

// terminalErr keeps the reasons a consumer can act on. A collected record is not one:
// its log cascades away with it, so the watch ending IS the deletion arriving, and
// reporting it puts an error in front of a user who pressed the button. ErrWatchTooOld
// stays reported — runs were lost, and resubscribing is what makes the client correct.
func terminalErr(err error) error {
	if errors.Is(err, beehive.ErrNotFound) {
		return nil
	}
	return err
}

func runFrame(ev beehive.Event) EventFeedFrame {
	served := toEvent(ev)
	return EventFeedFrame{Type: EventFeedFrameRun, Event: &served}
}

// ListEvents is the cluster's timeline, read off its runtime object. A cluster with
// no object has no events.
func (a clustersAPI) ListEvents(ctx context.Context, id ClusterID, category *string, limit *int) ([]Event, error) {
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil || obj == nil {
		return nil, err
	}
	return a.s.ListEvents(ctx, ObjectID(obj.ID), category, limit)
}

// SubscribeEventFeed is the cluster's event feed. A cluster with no object is the
// Bookmark alone, held open until the caller is done: an object created later has an
// id of its own, so this stream would never carry its runs anyway, and a stream that
// ended would be a drop the client reconnects on for as long as the object is absent.
func (a clustersAPI) SubscribeEventFeed(ctx context.Context, id ClusterID, category *string) (*Stream[EventFeedFrame], error) {
	obj, err := a.s.runtimeObject(ctx, id)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return NewStream(ctx, func(ctx context.Context, out chan<- EventFeedFrame) error {
			if sendFrame(ctx, out, EventFeedFrame{Type: EventFeedFrameBookmark}) {
				<-ctx.Done()
			}
			return nil
		}), nil
	}
	return a.s.SubscribeObjectEventFeed(ctx, ObjectID(obj.ID), category)
}
