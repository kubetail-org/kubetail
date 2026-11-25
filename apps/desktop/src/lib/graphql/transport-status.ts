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

// Per-operation transport-status registry (keyed by urql `operation.key`), from
// subscribe-exchange to useFoldedSubscription. `generation` is a process-wide
// monotonic serial stamped per successful open — global, not per-key, because
// urql retains accumulators across variable changes and pause/re-execute, where
// a per-key counter could alias a carried-over tag.
// See docs/adr/2026-08-09-transport-status-generation.md
export type TransportStatus = { connected: boolean; generation: number };

// Shared frozen default: useSyncExternalStore's getSnapshot must return a
// referentially stable object or it re-renders forever.
const DEFAULT: TransportStatus = Object.freeze({ connected: false, generation: 0 });

type Entry = {
  // Replaced, not mutated — identity tracks change for useSyncExternalStore.
  snapshot: TransportStatus;
  listeners: Set<() => void>;
};

const registry = new Map<number, Entry>();

function entryFor(key: number): Entry {
  let entry = registry.get(key);
  if (!entry) {
    entry = { snapshot: DEFAULT, listeners: new Set() };
    registry.set(key, entry);
  }
  return entry;
}

function set(key: number, next: TransportStatus) {
  const entry = entryFor(key);
  // No-op on no change: repeated markDisconnected (each failed dial) mustn't
  // churn snapshots and re-render.
  if (entry.snapshot.connected === next.connected && entry.snapshot.generation === next.generation) return;
  entry.snapshot = next;
  entry.listeners.forEach((cb) => cb());
}

export function getStatus(key: number): TransportStatus {
  return registry.get(key)?.snapshot ?? DEFAULT;
}

// Change listener for useSyncExternalStore; safe before the key is ever marked.
export function subscribeStatus(key: number, cb: () => void): () => void {
  const entry = entryFor(key);
  entry.listeners.add(cb);
  return () => {
    entry.listeners.delete(cb);
    // Whichever teardown removes the final listener owns reclamation —
    // `clearStatus` can run before the store listener detaches, so without
    // this the registry grows one entry per distinct op key.
    if (entry.listeners.size === 0) registry.delete(key);
  };
}

let connectionSerial = 0;

// Fresh globally-unique generation — the hook's reset trigger.
export function markConnected(key: number) {
  connectionSerial += 1;
  set(key, { connected: true, generation: connectionSerial });
}

// Keep the generation: a drop isn't a new connection, so the hook keeps
// last-known data through the outage.
export function markDisconnected(key: number) {
  set(key, { connected: false, generation: getStatus(key).generation });
}

// Consumer teardown: drop the entry, or if a straggler consumer is still
// mounted, reset it to disconnected (via `set`, reusing notify + change-guard).
export function clearStatus(key: number) {
  const entry = registry.get(key);
  if (!entry) return;
  if (entry.listeners.size === 0) registry.delete(key);
  else set(key, DEFAULT);
}
