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

// A switch in flight: a chat's sandbox and network switches, and the sandbox
// default a new chat starts with. A send says which switches its sender saw and
// the sidecar refuses one that differs, so Send and Ask again hold off from the
// press until the watch shows the value the mutation committed: the mutation's
// answer and the watch's frame are separate streams, and a send between them
// would pass the old value.
// The provider sits above the routes, like the outbox, so a pane that remounts
// meanwhile still waits.
import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

// By switch: null while the mutation is in flight, then the value it committed
// until the watch shows it.
type InFlight = Record<string, boolean | null>;

type ChatSwitchContextValue = {
  inFlight: InFlight;
  setInFlight: (next: (current: InFlight) => InFlight) => void;
};

const ChatSwitchContext = createContext<ChatSwitchContextValue | null>(null);

export function ChatSwitchProvider({ children }: { children: ReactNode }) {
  const [inFlight, setInFlight] = useState<InFlight>({});
  const value = useMemo(() => ({ inFlight, setInFlight }), [inFlight]);
  return <ChatSwitchContext.Provider value={value}>{children}</ChatSwitchContext.Provider>;
}

function without(current: InFlight, key: string): InFlight {
  const next = { ...current };
  delete next[key];
  return next;
}

export type ChatSwitching = {
  /** Whether a switch is in flight, or committed and not yet watched. */
  switching: boolean;
  switchTo: (value: boolean) => void;
};

/**
 * One switch of one chat, named by `key`, given the value the pane's list watch
 * shows. `commit` runs the mutation and answers the value it committed, or
 * undefined when it failed; errorReportExchange reports the failure.
 */
export function useChatSwitch(
  key: string,
  watched: boolean | undefined,
  commit: (value: boolean) => Promise<boolean | undefined>,
): ChatSwitching {
  const ctx = useContext(ChatSwitchContext);
  if (!ctx) throw new Error('useChatSwitch must be used within a ChatSwitchProvider');
  const { inFlight, setInFlight } = ctx;
  const committed = inFlight[key];
  const landed = committed !== undefined && committed !== null && committed === watched;

  useEffect(() => {
    if (landed) setInFlight((current) => without(current, key));
  }, [landed, key, setInFlight]);

  // A pane that mounts later opens its own list watch, whose snapshot follows a
  // commit that has answered, so the wait goes with the pane that started it. A
  // switch still in flight stays for the next pane.
  useEffect(
    () => () => setInFlight((current) => (current[key] === null ? current : without(current, key))),
    [key, setInFlight],
  );

  const switchTo = (value: boolean) => {
    setInFlight((current) => ({ ...current, [key]: null }));
    void commit(value).then((result) =>
      setInFlight((current) => (result === undefined ? without(current, key) : { ...current, [key]: result })),
    );
  };

  return { switching: committed !== undefined && !landed, switchTo };
}

/**
 * A change named by `key` held only while its mutation is in flight. Whoever
 * waits on it reads its own watch, which no one else can see land, so the
 * hold ends with the answer whether or not the pane that started it is still
 * mounted.
 *
 * So a waiter's watch can still show the old value for a moment after the
 * answer. The one change held this way is a sandbox default, made in Settings,
 * a dialog over the composer that waits on it, so that window cannot send in
 * the moment. A send that does is refused with KUBETAIL_CHAT_SANDBOX_CHANGED
 * and keeps its draft.
 */
export function usePendingSwitch(key: string): { pending: boolean; run: (mutation: () => Promise<unknown>) => void } {
  const ctx = useContext(ChatSwitchContext);
  if (!ctx) throw new Error('usePendingSwitch must be used within a ChatSwitchProvider');
  const { inFlight, setInFlight } = ctx;
  const run = (mutation: () => Promise<unknown>) => {
    setInFlight((current) => ({ ...current, [key]: null }));
    const done = () => setInFlight((current) => without(current, key));
    void mutation().then(done, done);
  };
  return { pending: key in inFlight, run };
}

/**
 * Whether any of the switches named by `keys` is in flight, or committed and
 * not yet watched, wherever its control is drawn.
 */
export function useSwitchingAny(keys: string[]): boolean {
  const ctx = useContext(ChatSwitchContext);
  if (!ctx) throw new Error('useSwitchingAny must be used within a ChatSwitchProvider');
  return keys.some((key) => key in ctx.inFlight);
}
