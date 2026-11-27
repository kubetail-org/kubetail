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

// What a window has typed into a chat but not yet delivered: the draft in the box
// and the send in flight, one entry per chat. The entries live above the routes
// because the composer unmounts under them — the first send moves chat mode to the
// new chat's route, and a chat deleted elsewhere takes its pane down — and a send
// must settle into its entry whatever became of the box that started it.
import type { ReactNode } from 'react';
import { createContext, useContext, useMemo, useState } from 'react';
import { useMutation } from 'urql';

import { graphql } from '@/gql';
import type { LogsViewSnapshotInput } from '@/gql/graphql';
import type { AppMode } from '@/lib/app-mode';
import { chatModeOf } from '@/lib/chats';
import type { ModelPick, ModelRef } from '@/lib/models';

const ChatSendMutation = graphql(`
  mutation ChatSend(
    $chatID: ChatID
    $mode: ChatMode!
    $clusterID: ClusterID!
    $sandboxDisabled: Boolean!
    $networkEnabled: Boolean!
    $networkThisTurn: Boolean!
    $sandboxDisabledThisTurn: Boolean!
    $providerID: String!
    $modelID: String!
    $effort: String!
    $requestID: String!
    $content: String!
    $snapshot: LogsViewSnapshotInput
  ) {
    chatSend(
      chatID: $chatID
      mode: $mode
      clusterID: $clusterID
      sandboxDisabled: $sandboxDisabled
      networkEnabled: $networkEnabled
      networkThisTurn: $networkThisTurn
      sandboxDisabledThisTurn: $sandboxDisabledThisTurn
      providerID: $providerID
      modelID: $modelID
      effort: $effort
      requestID: $requestID
      content: $content
      snapshot: $snapshot
    ) {
      id
      chatID
      seq
      status
    }
  }
`);

/** A chat's two switches as the sender sees them, which the sidecar checks a send against. */
export type Switches = { sandboxDisabled: boolean; networkEnabled: boolean };

/**
 * What changes one message's turn alone: the internet for its sandboxed
 * commands, or running them outside the sandbox. One at most, since network
 * is the sandbox's.
 */
export type ThisTurn = 'network' | 'outside' | null;

/** How far a send has got. One value, so a held send cannot also be settled. */
export type Send =
  | { status: 'idle' }
  | { status: 'sending'; requestID: string; content: string }
  /**
   * No answer came. It may have committed, so Retry resends the same id, cluster,
   * pick, switches, turn's toggle and snapshot — and under the same ownership, since a
   * held Ask again is still not the draft's send.
   */
  | {
      status: 'held';
      requestID: string;
      content: string;
      clusterID: string;
      pick: ModelPick;
      switches: Switches;
      thisTurn: ThisTurn;
      snapshot: LogsViewSnapshotInput | null;
      own: boolean;
    }
  /** Accepted; over once the messages watch delivers the row with this `seq`. */
  | { status: 'awaiting'; seq: number };

const IDLE: Send = { status: 'idle' };

/**
 * Why the last send was refused, when the composer has something to say about it.
 * A full chat carries the model the send named: the composer's pick can move on —
 * or an Ask again can have run on another — and the refusal is still that model's.
 * A changed switch means the chat's sandbox or network switch was not what the
 * sender saw.
 */
export type Refusal =
  | { kind: 'context-full'; model: ModelRef }
  | { kind: 'sandbox-changed' }
  | { kind: 'network-changed' };

/**
 * `thisTurn` changes the next send's turn: the entry's, so it survives the
 * composer unmounting, and cleared once a send is accepted.
 */
type Entry = { draft: string; send: Send; pick: ModelPick | null; refusal: Refusal | null; thisTurn: ThisTurn };

const EMPTY: Entry = { draft: '', send: IDLE, pick: null, refusal: null, thisTurn: null };

type Entries = Record<string, Entry>;

type ChatOutboxContextValue = {
  entries: Entries;
  setEntries: (next: (current: Entries) => Entries) => void;
};

const ChatOutboxContext = createContext<ChatOutboxContextValue | null>(null);

/** A chat's key is its id. The chat that has not started gets one per mode, since the two modes start different chats. */
export const outboxKey = (mode: AppMode, chatID: string | null) => chatID ?? `new:${mode}`;

export function ChatOutboxProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<Entries>({});
  const value = useMemo(() => ({ entries, setEntries }), [entries]);
  return <ChatOutboxContext.Provider value={value}>{children}</ChatOutboxContext.Provider>;
}

/** The chat a send created, with the cluster it was filed under. */
export type Created = { chatID: string; clusterID: string };

/**
 * One chat's outbox. `submit` and `retry` resolve to the chat the send created, or
 * null; everything else they decide lands in the entry, which outlives the caller.
 * `clusterID` is what a send is filed under and `seed` what an entry not yet picked
 * for runs on; without either `submit` sends nothing, and a caller after `moveDraft`
 * alone leaves both out.
 */
export function useChatOutbox(mode: AppMode, chatID: string | null, clusterID?: string, seed?: ModelPick) {
  const ctx = useContext(ChatOutboxContext);
  if (!ctx) throw new Error('useChatOutbox must be used within a ChatOutboxProvider');
  const { entries, setEntries } = ctx;
  const [, chatSend] = useMutation(ChatSendMutation);

  const key = outboxKey(mode, chatID);
  const entry = entries[key] ?? EMPTY;

  const patch = (next: Partial<Entry>) =>
    setEntries((current) => ({ ...current, [key]: { ...(current[key] ?? EMPTY), ...next } }));

  // `own` is whether the draft is what is being sent: askAgain sends a question off
  // a failed row instead, and clearing the draft or its toggle then would take what
  // the user set up for a follow-up.
  const run = async (
    requestID: string,
    content: string,
    cluster: string,
    pick: ModelPick,
    switches: Switches,
    thisTurn: ThisTurn,
    snapshot: LogsViewSnapshotInput | null,
    own = true,
  ): Promise<Created | null> => {
    patch({ send: { status: 'sending', requestID, content }, refusal: null });
    const result = await chatSend({
      chatID,
      mode: chatModeOf(mode),
      clusterID: cluster,
      sandboxDisabled: switches.sandboxDisabled,
      networkEnabled: switches.networkEnabled,
      networkThisTurn: thisTurn === 'network',
      sandboxDisabledThisTurn: thisTurn === 'outside',
      providerID: pick.model.providerID,
      modelID: pick.model.id,
      effort: pick.effort,
      requestID,
      content,
      snapshot,
    });

    if (result.error?.networkError) {
      // Committed-but-lost looks exactly like never-arrived. Keeping the id lets
      // Retry ask again under it, and the sidecar answers a repeat by its key.
      patch({
        send: {
          status: 'held',
          requestID,
          content,
          clusterID: cluster,
          pick,
          switches,
          thisTurn,
          snapshot,
          own,
        },
      });
      return null;
    }
    const message = result.data?.chatSend;
    if (!message) {
      // Refused, so nothing committed: the draft stays and the next submit mints a
      // fresh id. A full chat and a changed switch are the refusals the composer draws.
      const code = result.error?.graphQLErrors[0]?.extensions?.code;
      let refusal: Refusal | null = null;
      if (code === 'KUBETAIL_CHAT_CONTEXT_FULL') refusal = { kind: 'context-full', model: pick.model };
      else if (code === 'KUBETAIL_CHAT_SANDBOX_CHANGED') refusal = { kind: 'sandbox-changed' };
      else if (code === 'KUBETAIL_CHAT_NETWORK_CHANGED') refusal = { kind: 'network-changed' };
      patch({ send: IDLE, refusal });
      return null;
    }
    if (chatID) {
      // Send stays closed until the row reaches the watch; before that the last
      // message on screen is still the one from before this send.
      patch({ ...(own ? { draft: '', thisTurn: null } : {}), send: { status: 'awaiting', seq: message.seq } });
      return null;
    }
    // A created chat's row arrives with the snapshot its route opens, and the
    // unstarted entry watches no messages that could settle an `awaiting`.
    patch({ ...(own ? { draft: '', thisTurn: null } : {}), send: IDLE });
    // The cluster rides back with the id: a retry goes under the cluster it was held
    // with, which by then need not be the one the window is on.
    return { chatID: message.chatID, clusterID: cluster };
  };

  // A held send's toggle is the one Retry resends, so it is the one shown. The
  // draft's own goes on underneath it, for after a Discard.
  const thisTurn = entry.send.status === 'held' ? entry.send.thisTurn : entry.thisTurn;
  const setThisTurn = (toggle: 'network' | 'outside', on: boolean) => {
    if (on) patch({ thisTurn: toggle });
    else if (entry.thisTurn === toggle) patch({ thisTurn: null });
  };

  // The pick is the entry's own once the composer has set one; until then it is
  // whatever the composer seeds, which is the chat's last answer or the catalog's
  // first model.
  const pick = entry.pick ?? seed ?? null;

  return {
    draft: entry.draft,
    send: entry.send,
    refusal: entry.refusal,
    pick,
    networkThisTurn: thisTurn === 'network',
    sandboxDisabledThisTurn: thisTurn === 'outside',
    setDraft: (draft: string) => patch({ draft }),
    setPick: (next: ModelPick) => patch({ pick: next }),
    // Turning one on takes the other's place; turning one off leaves the other.
    setNetworkThisTurn: (on: boolean) => setThisTurn('network', on),
    setSandboxDisabledThisTurn: (on: boolean) => setThisTurn('outside', on),
    /**
     * Sends the draft under `switches`, the chat's switches as the composer shows
     * them, with the turn's toggle and `snapshot`, what the focused log view shows.
     */
    submit: (switches: Switches, snapshot: LogsViewSnapshotInput | null = null) =>
      clusterID && pick
        ? run(crypto.randomUUID(), entry.draft, clusterID, pick, switches, entry.thisTurn, snapshot)
        : Promise.resolve(null),
    retry: () =>
      entry.send.status === 'held'
        ? run(
            entry.send.requestID,
            entry.send.content,
            entry.send.clusterID,
            entry.send.pick,
            entry.send.switches,
            entry.send.thisTurn,
            entry.send.snapshot,
            entry.send.own,
          )
        : Promise.resolve(null),
    /**
     * Asks a failed answer's question again: a fresh send under a new id, carrying
     * what the failed row ran on. It is the user asking twice, which is theirs to
     * do — the sidecar never retries a turn — and it rides the entry's one send, so
     * a press while anything is in flight sends nothing. It never sends the toggle:
     * it is the draft's, and the user gave it for the message they are typing.
     * Nor a snapshot: the screen now does not describe an earlier question.
     */
    askAgain: (content: string, ran: ModelPick, switches: Switches) =>
      clusterID && entry.send.status === 'idle'
        ? run(crypto.randomUUID(), content, clusterID, ran, switches, null, null, false)
        : Promise.resolve(null),
    /**
     * Closes an accepted send once the row it is awaiting has reached the watch:
     * whoever watches the messages is the only one who can see that. Until then the
     * last message on screen is the one from before the send, and the entry's one
     * send is what everything else waits on.
     */
    settle: (seq: number) => {
      if (entry.send.status === 'awaiting' && seq >= entry.send.seq) patch({ send: IDLE });
    },
    discard: () => patch({ send: IDLE }),
    /** Hands this chat's draft to another, joined onto whatever is waiting there, and drops the entry. */
    moveDraft: (toChatID: string | null) =>
      setEntries((current) => {
        const from = current[key];
        if (from === undefined) return current;
        // Copied and deleted: the React compiler skips a hook that destructures a computed key.
        const rest = { ...current };
        delete rest[key];
        if (!from.draft) return rest;
        const to = outboxKey(mode, toChatID);
        const waiting = rest[to]?.draft ?? '';
        const draft = waiting ? `${waiting}\n\n${from.draft}` : from.draft;
        return { ...rest, [to]: { ...(rest[to] ?? EMPTY), draft } };
      }),
  };
}
