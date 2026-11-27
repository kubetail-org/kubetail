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

// A chat wherever it is drawn: a page in chat mode, the right sidebar's panel on the
// dashboard. Each pane is a column whose last item is the composer, so the space
// above it is what scrolls. Neither navigates — the two homes answer "a chat was
// created" and "this chat is gone" differently, so they say so and the home decides.
import type { ReactNode, Ref } from 'react';

import { ChatComposer } from '@/components/chat/chat-composer';
import { ChatHeader } from '@/components/chat/chat-header';
import { ChatTranscript } from '@/components/chat/chat-transcript';
import { useActiveCluster } from '@/lib/active-cluster';
import { useActiveKubeContext } from '@/lib/active-kube-context';
import type { AppMode } from '@/lib/app-mode';
import { useAskAgainWhenLive } from '@/lib/ask-again-when-live';
import { useChatOutbox } from '@/lib/chat-outbox';
import { approvalAnchor, chatModeOf, useChatMessages, useChats, waitingRequestsOf } from '@/lib/chats';
import type { ChatMessage } from '@/lib/chats';
import { useClusters } from '@/lib/clusters';
import { useLastSettled } from '@/lib/graphql/use-last-settled';
import { useNetworkSwitch } from '@/lib/network-switch';
import { useSandbox } from '@/lib/sandbox';
import { useSandboxDefault, useSandboxDefaultChanging } from '@/lib/sandbox-default';
import { useSandboxSwitch } from '@/lib/sandbox-switch';

type NewChatPaneProps = {
  mode: AppMode;
  /** What fills the space above the composer. */
  empty: ReactNode;
  /** Draw `empty` and the composer as one group in the middle of the pane. */
  centered?: boolean;
  /** The first send created this chat. */
  onCreated: (chatID: string) => void;
  /** The composer's message box. */
  inputRef?: Ref<HTMLTextAreaElement>;
};

/** A chat that has not started. Nothing is written until the first send creates it. */
export function NewChatPane({ mode, empty, centered = false, onCreated, inputRef }: NewChatPaneProps) {
  // The chat is filed under the window's cluster, so the composer waits on the
  // clusters watch: no snapshot yet means no answer to whether there is one.
  const { clusterID, phase } = useActiveCluster();
  // The toggle needs the machine's word on the sandbox and its network.
  const sandbox = useSandbox();
  useAskAgainWhenLive(sandbox.failed, phase === 'live', sandbox.retry);
  // The chat is created with its cluster's default, so a send names the one
  // shown and waits for a change of any default to reach the watch.
  const sandboxDefault = useSandboxDefault(clusterID);
  const defaultChanging = useSandboxDefaultChanging(clusterID);

  const composer = (
    <ChatComposer
      chatID={null}
      mode={mode}
      clusterID={clusterID}
      phase={phase}
      last={null}
      onCreated={onCreated}
      sandboxAvailable={sandbox.available}
      sandboxDisabled={sandboxDefault?.sandboxDisabled}
      switching={defaultChanging}
      networkAvailable={sandbox.networkAvailable}
      networkReason={sandbox.networkReason}
      inputRef={inputRef}
    />
  );

  if (centered) {
    // Sits a little above the middle, where the eye starts.
    return (
      <div className="flex min-h-0 flex-1 flex-col justify-center gap-6 pb-30">
        {empty}
        {composer}
      </div>
    );
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* A flex container, so an `empty` that scrolls can shrink into it. */}
      <div className="flex min-h-0 flex-1 flex-col">{empty}</div>
      {composer}
    </div>
  );
}

type ChatPaneProps = {
  chatID: string;
  mode: AppMode;
  /** The chat is gone and the window should leave it. */
  onGone: () => void;
};

/** One chat: its transcript over its composer. */
export function ChatPane({ chatID, mode, onGone }: ChatPaneProps) {
  // Keyed on the id so the watches, the scroll position and the composer start
  // fresh when one pane moves from chat to chat.
  return <OpenChat key={chatID} chatID={chatID} mode={mode} onGone={onGone} />;
}

// A chat of another cluster. It is still there, so the way out is to follow it — the
// button renders only while its cluster is a kube-context to switch to. A draft
// typed into it waits in its outbox entry until the window is back.
function OutOfScope({ clusterID }: { clusterID: string }) {
  const { clusters } = useClusters();
  const { contexts, setContext } = useActiveKubeContext();
  const name = clusters?.find((c) => c.id === clusterID)?.spec.source.kubeconfig?.context;
  // The record names its context whether or not the picker offers it — a disabled
  // cluster, or one the kubeconfig no longer declares, keeps the name it was imported
  // under, and the param it would write yields to the default, leaving this notice up.
  const context = contexts.some((c) => c.name === name) ? name : undefined;

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2">
      <p className="text-sm text-muted-foreground">This chat belongs to another cluster.</p>
      {context && (
        <button type="button" className="text-sm underline" onClick={() => setContext(context)}>
          Switch to it
        </button>
      )}
    </div>
  );
}

// The first request waiting on the user in a message before the last: the
// transcript follows the last message down by itself, but an agent can wait
// under any answer.
function firstWaitingEarlier(messages: ChatMessage[]): string | undefined {
  const message = messages.slice(0, -1).find((m) => m.awaitingApproval);
  return message && waitingRequestsOf(message.toolCalls)[0]?.approval.id;
}

function OpenChat({ chatID, mode, onGone }: ChatPaneProps) {
  // The list is opened here rather than above so a pane with nothing open costs no
  // second cold list beside the sidebar's.
  const { chats, phase: listPhase } = useChats();
  const { messages, phase: messagesPhase } = useChatMessages(chatID);
  const { moveDraft } = useChatOutbox(mode, chatID);
  const { clusterID, phase: clusterPhase } = useActiveCluster();
  const sandbox = useSandbox();
  // The clusters watch is one more thing the pane waits on: the window's cluster is
  // what says whether this chat is in scope, and an unanswered watch names none.
  const phase = listPhase === 'connecting' || clusterPhase === 'connecting' ? 'connecting' : messagesPhase;
  // Without the answer the switch stays hidden.
  useAskAgainWhenLive(sandbox.failed, phase === 'live', sandbox.retry);
  const chat = chats.find((c) => c.id === chatID);
  // Held here, since the composer's Send and the transcript's Ask again both wait
  // for either switch in flight.
  const sandboxSwitch = useSandboxSwitch(chatID, chat?.sandboxDisabled);
  const networkSwitch = useNetworkSwitch(chatID, chat?.networkEnabled);
  const switching = sandboxSwitch.switching || networkSwitch.switching;
  const waiting = firstWaitingEarlier(messages);
  // The title bar's chat: one the chat list would list, in scope and of this mode, since a
  // link can name any chat's id. `useChats` holds the chat through a reconnect, and the
  // cluster is held through one of the clusters watch, so an open rename or confirm in the
  // bar rides either out.
  const scope = useLastSettled(clusterID, clusterPhase !== 'connecting');
  const titled = chat && chat.clusterID === scope && chat.mode === chatModeOf(mode) ? chat : null;

  // Absence is gated on the list's Bookmark: before it, an id the map does not hold
  // is one the snapshot has yet to reach.
  if (listPhase !== 'connecting' && !chat) {
    const leave = () => {
      // The draft was typed as deliberately as any; it goes where the window goes.
      moveDraft(null);
      onGone();
    };
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2">
        <p className="text-sm text-muted-foreground">This chat was deleted.</p>
        <button type="button" className="text-sm underline" onClick={leave}>
          Start a new one
        </button>
      </div>
    );
  }

  // Gated on the clusters snapshot for the same reason absence is gated on the list's:
  // before it, every chat looks like another cluster's.
  if (chat && clusterPhase !== 'connecting' && chat.clusterID !== clusterID) {
    return <OutOfScope clusterID={chat.clusterID} />;
  }

  const transcript = (
    <ChatTranscript
      messages={messages}
      phase={phase}
      chatID={chatID}
      mode={mode}
      clusterID={chat?.clusterID}
      sandboxAvailable={sandbox.available}
      sandboxDisabled={chat?.sandboxDisabled}
      networkEnabled={chat?.networkEnabled}
      switching={switching}
      footer={
        // The chat's own cluster, so a send into it needs no active one.
        <ChatComposer
          chatID={chatID}
          mode={mode}
          clusterID={chat?.clusterID}
          phase={phase}
          last={messages.at(-1) ?? null}
          lastAnswer={messages.findLast((m) => m.role === 'Assistant') ?? null}
          sandboxAvailable={sandbox.available}
          sandboxDisabled={chat?.sandboxDisabled}
          networkAvailable={sandbox.networkAvailable}
          networkReason={sandbox.networkReason}
          networkEnabled={chat?.networkEnabled}
          switching={switching}
          onSwitchSandbox={sandboxSwitch.setSandboxDisabled}
          onSwitchNetwork={networkSwitch.setNetworkEnabled}
          onShowWaiting={
            waiting
              ? () => document.getElementById(approvalAnchor(waiting))?.scrollIntoView({ block: 'center' })
              : undefined
          }
        />
      }
    />
  );
  // Chat mode's page carries the chat's title bar; the dashboard's panel does not.
  if (mode !== 'chat') return transcript;
  return (
    <>
      {titled && <ChatHeader chat={titled} />}
      {transcript}
    </>
  );
}
