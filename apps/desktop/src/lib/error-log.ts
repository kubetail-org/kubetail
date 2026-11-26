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

// Forwards every error on the bus to the host's `webview_record_error` command,
// so it lands in `main.log`. Started from `main.tsx` before React mounts: a
// startup failure or an `ErrorBoundary` crash happens above anything a
// component could reach. The host re-applies its own caps and rate limits;
// these are the frontend's share of bounding what crosses IPC.

import { invoke } from '@tauri-apps/api/core';

import { errorMessage, onError, reportError, type AppError, type AppErrorSource } from '@/lib/error-bus';

export const MAX_MESSAGE_CHARS = 2000;
export const MAX_STACK_CHARS = 8000;
export const MAX_COMPONENT_STACK_CHARS = 8000;
export const MAX_CONTEXT_CHARS = 4000;

// Sends in flight before records are dropped: a tight error loop must not queue
// against a host that is already rate limiting it.
export const MAX_IN_FLIGHT = 8;
// Slots only `render` and `auth` may take, so a flood of subscription retries
// cannot starve the crash behind them.
export const RESERVED_IN_FLIGHT = 2;

const RESERVED_SOURCES: ReadonlySet<AppErrorSource> = new Set(['render', 'auth']);

// Mirrors the host's `WebviewRecord`.
type WebviewLogRecord = {
  source: AppErrorSource;
  message: string;
  stack?: string;
  componentStack?: string;
  context?: Record<string, unknown>;
};

function cap(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text;
}

// Each optional field is read under its own guard: a getter on an arbitrary
// `cause` can throw, and the record must still go with what is in hand.
function stackOf(cause: unknown): string | undefined {
  try {
    const stack = (cause as { stack?: unknown } | null | undefined)?.stack;
    return typeof stack === 'string' ? cap(stack, MAX_STACK_CHARS) : undefined;
  } catch {
    return undefined;
  }
}

function componentStackOf(componentStack: unknown): string | undefined {
  try {
    return typeof componentStack === 'string' ? cap(componentStack, MAX_COMPONENT_STACK_CHARS) : undefined;
  } catch {
    /* v8 ignore next -- reading a primitive string's length and slice cannot throw */
    return undefined;
  }
}

// Truncation replaces the whole value: a JSON string cut mid-way is not JSON.
function contextOf(context: unknown, dropped: number): Record<string, unknown> | undefined {
  try {
    const base = context && typeof context === 'object' ? (context as Record<string, unknown>) : {};
    const value = dropped > 0 ? { ...base, dropped } : base;
    if (Object.keys(value).length === 0) return undefined;
    return JSON.stringify(value).length > MAX_CONTEXT_CHARS ? { truncated: true } : value;
  } catch {
    return dropped > 0 ? { dropped } : undefined;
  }
}

function toRecord(error: AppError, dropped: number): WebviewLogRecord {
  const record: WebviewLogRecord = { source: error.source, message: cap(error.message, MAX_MESSAGE_CHARS) };
  const stack = stackOf(error.cause);
  if (stack !== undefined) record.stack = stack;
  const componentStack = componentStackOf(error.componentStack);
  if (componentStack !== undefined) record.componentStack = componentStack;
  const context = contextOf(error.context, dropped);
  if (context !== undefined) record.context = context;
  return record;
}

// Subscribes to the bus and forwards until the returned function is called.
// Never reports to the bus on failure — that would feed it back into itself.
export function startErrorLogForwarder(): () => void {
  let inFlight = 0;
  // Records dropped at the cap; rides as `context.dropped` on the next one sent,
  // since the host cannot count what it never received.
  let dropped = 0;

  return onError((error) => {
    const limit = RESERVED_SOURCES.has(error.source) ? MAX_IN_FLIGHT : MAX_IN_FLIGHT - RESERVED_IN_FLIGHT;
    if (inFlight >= limit) {
      dropped += 1;
      return;
    }
    const record = toRecord(error, dropped);
    dropped = 0;
    inFlight += 1;
    invoke('webview_record_error', { record })
      .catch(() => {})
      .finally(() => {
        inFlight -= 1;
      });
  });
}

function onUncaught(event: ErrorEvent) {
  // A ResizeObserver whose callback changes layout (floating-ui's, as a popup
  // resizes) gets its remaining notifications next frame; the browser says so
  // with an error event carrying no error. Nothing failed.
  if (event.message.startsWith('ResizeObserver loop')) return;
  const cause: unknown = event.error;
  reportError({ source: 'render', message: cause == null ? event.message : errorMessage(cause), cause });
}

function onRejection(event: Event) {
  const { reason } = event as { reason?: unknown };
  reportError({ source: 'render', message: errorMessage(reason), cause: reason });
}

// Catches what never reaches React — a throw outside a render or an unhandled
// rejection — and reports it as a `render` error.
export function installGlobalErrorHandlers(): () => void {
  window.addEventListener('error', onUncaught);
  window.addEventListener('unhandledrejection', onRejection);
  return () => {
    window.removeEventListener('error', onUncaught);
    window.removeEventListener('unhandledrejection', onRejection);
  };
}
