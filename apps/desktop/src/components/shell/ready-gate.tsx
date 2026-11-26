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

// Blocks the app tree until the host's `ready` command resolves (sidecar IPC
// accepting connections) — otherwise the first GraphQL call silently absorbs the
// bind-wait and failures become stuck spinners instead of an explicit retry.
// Every state draws the mark where index.html's static copy stood, so the mount
// replaces one with the other and nothing moves.
//
// The app bar, the window's only title bar, mounts behind the gate, so the gate
// draws its own drag strip. It overlays the top of the screen rather than taking
// space from it, keeping the mark centred in the window.

import { Button } from '@kubetail/ui/elements/button';
import { invoke } from '@tauri-apps/api/core';
import { useCallback, useEffect, useState } from 'react';

import { AppMark } from '@/components/primitives/app-mark';
import { VisibleText } from '@/components/primitives/visible-text';
import { errorMessage, reportError } from '@/lib/error-bus';

// Keeps the gate on screen in a dev build: `pending` never settles, `error` fails
// as a start that timed out would. See docs/development-switches.md.
async function devHold(hold: string | undefined) {
  if (hold === 'pending') await new Promise(() => {});
  if (hold === 'error') throw new Error('held by VITE_READY_GATE_HOLD');
}

type State = { status: 'pending' } | { status: 'ready' } | { status: 'error'; message: string };

export function ReadyGate({ children }: { children: React.ReactNode }) {
  const [state, setState] = useState<State>({ status: 'pending' });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        if (import.meta.env.DEV) await devHold(import.meta.env.VITE_READY_GATE_HOLD);
        await invoke('ready');
        if (!cancelled) setState({ status: 'ready' });
      } catch (e) {
        if (cancelled) return;
        const message = errorMessage(e);
        reportError({ source: 'network', message, cause: e });
        setState({ status: 'error', message });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  const retry = useCallback(() => {
    setState({ status: 'pending' });
    setAttempt((n) => n + 1);
  }, []);

  if (state.status === 'ready') return <>{children}</>;

  return (
    <div
      role="status"
      aria-live="polite"
      aria-busy={state.status === 'pending'}
      className="relative flex min-h-(--app-min-h) flex-col items-center justify-center gap-3 bg-background p-6 text-sm"
    >
      {/* The app bar's 56px, so the macOS traffic lights sit in it */}
      <div data-testid="drag-strip" data-tauri-drag-region="deep" className="absolute inset-x-0 top-0 h-14" />
      <AppMark size={48} />
      {state.status === 'pending' ? (
        <span className="sr-only">Starting…</span>
      ) : (
        <>
          <p className="text-destructive">
            Failed to start: <VisibleText text={state.message} />
          </p>
          <Button size="sm" variant="outline" onClick={retry}>
            Retry
          </Button>
        </>
      )}
    </div>
  );
}
