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

// The fold every collection watch shares. See docs/read-contracts/README.md, *Collection watch*.
import type { SubscriptionReducer } from './use-folded-subscription';

// A map keyed by item id, accumulated from a collection watch.
export type Keyed<T> = ReadonlyMap<string, T>;

// `snapshotComplete` flips on the Bookmark. Until then `items` is a partial snapshot,
// not the collection.
export type CollectionState<T> = { items: Keyed<T>; snapshotComplete: boolean };

// One frame as a collection watch delivers it: the change type and its item, null on the
// Bookmark and on a change whose nested non-null field errored.
export type CollectionChange<T> = { type: string; item: T | null | undefined };

// Added and Modified upsert, Deleted removes, and a change with no item is dropped. The
// Bookmark is detected by `type`, never by a missing item: a null item also arrives on
// ordinary changes, and reading one as the boundary would call a half-listed collection
// complete. The map is copied once per batch.
export function collectionWatchReducer<Data, T extends { id: string }>(
  select: (data: Data) => CollectionChange<T>,
): SubscriptionReducer<Data, CollectionState<T>>;
export function collectionWatchReducer<Data, T>(
  select: (data: Data) => CollectionChange<T>,
  keyOf: (item: T) => string,
): SubscriptionReducer<Data, CollectionState<T>>;
export function collectionWatchReducer<Data, T>(
  select: (data: Data) => CollectionChange<T>,
  keyOf: (item: T) => string = (item) => (item as { id: string }).id,
): SubscriptionReducer<Data, CollectionState<T>> {
  return (prev, frames) => {
    const items = new Map(prev?.items);
    let snapshotComplete = prev?.snapshotComplete ?? false;
    frames.forEach((data) => {
      const { type, item } = select(data);
      if (type === 'Bookmark') snapshotComplete = true;
      else if (item && type === 'Deleted') items.delete(keyOf(item));
      else if (item) items.set(keyOf(item), item);
    });
    return { items, snapshotComplete };
  };
}
