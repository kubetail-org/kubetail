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

// The webview's client for `host.json`, the host-persisted settings file and its
// source of truth (`src-tauri/src/host_file.rs`). Owns the whole protocol — boot
// injection (`readInjectedHostFile`), patch writes (`updateHostFile`), and the
// post-write broadcast with its catch-up read (`subscribeHostFile`) — so a setting's module only picks its
// field out. Adding a setting means adding a field here and in the host, not a new
// command or sync path. See docs/adr/2026-08-09-host-json-settings.md
import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';

// `host.json` as the host serializes it. Every setting optional; values are typed
// `string` (not narrow unions) because the file is external input — each setting's
// module validates its own field, so a newer host format can't break an older webview.
export type HostFile = {
  schemaVersion?: number;
  colorSchemePreference?: string;
};

// Only present fields are written; the host merges. Mirrors the host's `HostFilePatch`.
export type HostFilePatch = Partial<Omit<HostFile, 'schemaVersion'>>;

declare global {
  interface Window {
    __KUBETAIL_HOST__?: HostFile;
  }
}

// Hand-mirrors `host_file::UPDATED_EVENT`; carries the merged file.
const UPDATED_EVENT = 'host-file-updated';

// The host-injected file, as of the last write before this load; empty outside
// Tauri (plain-browser dev). Synchronous — safe during first paint.
export function readInjectedHostFile(): HostFile {
  return window.__KUBETAIL_HOST__ ?? {};
}

// Fire-and-forget: callers apply optimistically; a failed write costs durability
// across restart, not the current session.
export function updateHostFile(patch: HostFilePatch): void {
  invoke('update_host_file', { patch }).catch(() => {});
}

// Subscribe to the host's post-write broadcast. The returned unsubscribe is safe
// to call before `listen` resolves — a `useEffect` cleanup never awaits registration.
//
// A write that landed while this page was loading was broadcast to no listener,
// so once listening, read the file once. A broadcast heard first is newer.
export function subscribeHostFile(onUpdate: (file: HostFile) => void): () => void {
  let unlisten: (() => void) | undefined;
  let cancelled = false;
  let heard = false;

  listen<HostFile>(UPDATED_EVENT, (event) => {
    heard = true;
    onUpdate(event.payload ?? {});
  })
    .then((fn) => {
      // Unsubscribed while `listen` was still in flight.
      if (cancelled) {
        fn();
        return undefined;
      }
      unlisten = fn;
      return invoke<HostFile>('read_host_file').then((file) => {
        if (!cancelled && !heard) onUpdate(file ?? {});
      });
    })
    // Absent bridge (plain-browser dev) — no host file to track.
    .catch(() => {});

  return () => {
    cancelled = true;
    unlisten?.();
  };
}
