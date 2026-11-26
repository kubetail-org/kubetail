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

// Transient error-bus banner; auto-dismisses so a brief reconnect doesn't leave it
// stale. Deliberately no toast lib for one surface — revisit if more transient UI appears.

import { useEffect, useState } from 'react';

import { onError, type AppError } from '@/lib/error-bus';

const DISMISS_AFTER_MS = 5_000;

export function ConnectionStatus() {
  const [latest, setLatest] = useState<AppError | null>(null);

  useEffect(() => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    const unsubscribe = onError((err) => {
      setLatest(err);
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => setLatest(null), DISMISS_AFTER_MS);
    });
    return () => {
      unsubscribe();
      if (timer) clearTimeout(timer);
    };
  }, []);

  if (!latest) return null;

  // Softer message for subscriptions — they auto-reconnect, so "reconnecting" is honest.
  const text = latest.source === 'subscription' ? 'Subscription dropped — reconnecting…' : `Error: ${latest.message}`;

  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed inset-x-0 top-0 z-50 mx-auto mt-2 w-fit rounded bg-destructive px-3 py-1.5 text-xs text-destructive-foreground shadow"
    >
      {text}
    </div>
  );
}
