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

// Tiny in-process pub/sub for transient runtime errors, surfaced by
// `ConnectionStatus`. Decoupled so a future reporter (e.g. Sentry) taps the same
// bus without touching producers.

export type AppErrorSource = 'graphql' | 'subscription' | 'network' | 'render' | 'auth';

export type AppError = {
  source: AppErrorSource;
  message: string;
  // Original Error / CombinedError for reporters; not rendered.
  cause?: unknown;
  // React's component stack; `ErrorBoundary` alone fills it.
  componentStack?: string;
  // Details a producer chooses to log, by key. Never a serialized `cause`: a
  // CombinedError carries GraphQL payloads full of cluster text.
  context?: Record<string, unknown>;
};

// Coerce an unknown thrown value into an Error (pass-through preserves the stack).
// The one place `catch`-value narrowing lives.
export function toError(x: unknown): Error {
  return x instanceof Error ? x : new Error(String(x));
}

// An Error's `.message` (not its `"Error: …"` toString), or String(x).
export function errorMessage(x: unknown): string {
  return toError(x).message;
}

const target = new EventTarget();
const ERROR_EVENT = 'app-error';

export function reportError(error: AppError): void {
  target.dispatchEvent(new CustomEvent(ERROR_EVENT, { detail: error }));
}

/** Reports a host command that failed or was refused. */
export function reportHostError(e: unknown): void {
  reportError({ source: 'network', message: errorMessage(e), cause: e });
}

export function onError(handler: (error: AppError) => void): () => void {
  const listener = (e: Event) => handler((e as CustomEvent<AppError>).detail);
  target.addEventListener(ERROR_EVENT, listener);
  return () => target.removeEventListener(ERROR_EVENT, listener);
}
