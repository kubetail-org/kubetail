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

// The fold every event feed shares. See docs/read-contracts/README.md, *Event feed*.
import type { SubscriptionReducer } from './use-folded-subscription';

// `snapshotComplete` flips on the Bookmark. Until then `events` is a still-arriving
// history, not the whole of it.
export type EventFeedHistory<E> = { events: E[]; snapshotComplete: boolean };

// One frame as an event feed delivers it: an event, or the Bookmark, which carries none.
export type EventFeedChange<E> = { type: string; event: E | null | undefined };

// Upserts by id, orders by `compare`, and keeps the first `cap`: the publisher prunes
// without announcing it, so a long-lived subscriber caps its own history.
export function eventFeedReducer<Data, E extends { id: string }>(
  select: (data: Data) => EventFeedChange<E>,
  { compare, cap }: { compare: (a: E, b: E) => number; cap: number },
): SubscriptionReducer<Data, EventFeedHistory<E>> {
  return (prev, frames) => {
    const byID = new Map((prev?.events ?? []).map((event) => [event.id, event]));
    let snapshotComplete = prev?.snapshotComplete ?? false;
    frames.forEach((data) => {
      const { type, event } = select(data);
      if (type === 'Bookmark') snapshotComplete = true;
      else if (event) byID.set(event.id, event);
    });
    return { events: [...byID.values()].toSorted(compare).slice(0, cap), snapshotComplete };
  };
}
