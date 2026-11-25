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

// A log window's page: its bar over the view the host paired it with. The page asks
// the host which chat and call it shows (`log_window_view`) and reads the rest from
// the watches, so the URL carries nothing and a reload draws the same view.
import { Button } from '@kubetail/ui/elements/button';
import { invoke } from '@tauri-apps/api/core';
import { useEffect, useState } from 'react';
import type { ReactNode } from 'react';

import { LogsViewHeader } from '@/components/logs/log-panel';
import { LogViewer } from '@/components/logs/log-viewer';
import { chatTitleOf, useChatMessages, useChats } from '@/lib/chats';
import { displayName, useClusters } from '@/lib/clusters';
import type { Cluster } from '@/lib/clusters';
import { reportHostError } from '@/lib/error-bus';
import { createLogViewportHandle } from '@/lib/log-viewport';
import type { LogViewportHandle } from '@/lib/log-viewport';
import { createViewportPublisher } from '@/lib/log-window-push';
import { viewOfCall } from '@/lib/logs-view';
import type { LogsView } from '@/lib/logs-view';

import { LogsWindowBar } from './bar';

/**
 * What the page reads of `log_window_view`'s answer. `grep` is null when the open carried
 * none; `generation` is the page load the host counts, which every push names.
 */
type Answer = { chatId: string; callId: string; grep: string | null; generation: number };

function Notice({ children }: { children: ReactNode }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 text-sm text-muted-foreground">
      {children}
    </div>
  );
}

export function LogsWindow() {
  const [answer, setAnswer] = useState<Answer | null>(null);
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);

  // The host only reads here, so a second mount's call answers the same.
  useEffect(() => {
    let cancelled = false;
    invoke<Answer>('log_window_view').then(
      (next) => {
        if (!cancelled) setAnswer(next);
      },
      (e: unknown) => {
        if (cancelled) return;
        reportHostError(e);
        setFailed(true);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [attempt]);

  if (answer) return <Paired answer={answer} />;
  return (
    <>
      <LogsWindowBar cluster={undefined} chatTitle="" />
      {failed ? (
        <Notice>
          <p>This log window could not load.</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setFailed(false);
              setAttempt((n) => n + 1);
            }}
          >
            Try again
          </Button>
        </Notice>
      ) : (
        <Notice>Loading…</Notice>
      )}
    </>
  );
}

// What the view was last drawn from. `chatTitle` is the chat's own, empty for none: the
// native title takes it as is, and the bar names it.
type Shown = { view: LogsView; chatTitle: string; cluster: Cluster | undefined };

function sameShown(a: Shown | null, b: Shown | null) {
  if (a === null || b === null) return a === b;
  return (
    a.view.action === b.view.action &&
    a.view.clusterId === b.view.clusterId &&
    a.chatTitle === b.chatTitle &&
    a.cluster === b.cluster
  );
}

function Paired({ answer }: { answer: Answer }) {
  const { chats, phase: chatsPhase } = useChats();
  const { messages, phase: messagesPhase } = useChatMessages(answer.chatId);
  const { clusters } = useClusters();

  const chat = chats.find((c) => c.id === answer.chatId);
  const cluster = clusters?.find((c) => c.id === chat?.clusterID);
  const call = messages.flatMap((m) => m.toolCalls).find((c) => c.id === answer.callId);
  const action = call && viewOfCall(call);
  const loading = chatsPhase === 'connecting' || messagesPhase === 'connecting' || clusters === null;
  const next =
    !loading && chat && action
      ? {
          view: { clusterId: chat.clusterID, chatId: chat.id, callId: answer.callId, action },
          chatTitle: chat.title,
          cluster,
        }
      : null;

  // A reconnect masks a watch's data until its new snapshot lands. The view drawn
  // before it stays up through that, so the viewer keeps its grep, scroll, pause and
  // selection; a settled snapshot that draws a notice lets it go.
  const [shown, setShown] = useState<Shown | null>(null);
  if (!loading && !sameShown(shown, next)) setShown(next);
  const drawn = loading ? shown : next;

  // The viewport and the grep applied here outlive the viewer, which a notice unmounts,
  // so the push keeps the grep and a viewer mounted again continues the revisions.
  const [viewport] = useState(createLogViewportHandle);
  const [applied, setApplied] = useState<string | null>(null);
  // The grep in effect: applied here, else the open's, else the action's.
  const chosen = applied ?? answer.grep;
  const grep = chosen ?? drawn?.view.action.grep ?? null;
  const [publisher] = useState(() =>
    createViewportPublisher({
      generation: answer.generation,
      grep,
      viewport,
      send: (push) => invoke('log_window_viewport', push),
      report: reportHostError,
    }),
  );
  useEffect(() => publisher.start(), [publisher]);
  useEffect(() => publisher.setGrep(grep), [publisher, grep]);
  // The user is leaving for the app window, where the push is read.
  useEffect(() => {
    window.addEventListener('blur', publisher.flush);
    return () => window.removeEventListener('blur', publisher.flush);
  }, [publisher]);

  let body: ReactNode;
  if (drawn) {
    body = (
      <View
        view={drawn.view}
        grep={chosen ?? drawn.view.action.grep}
        viewport={viewport}
        onGrepApplied={setApplied}
        chatTitle={drawn.chatTitle}
        clusterName={drawn.cluster ? displayName(drawn.cluster) : ''}
      />
    );
  } else if (loading) {
    body = <Notice>Loading…</Notice>;
  } else if (!chat) {
    body = <Notice>This chat was deleted.</Notice>;
  } else {
    body = <Notice>This log view is not available.</Notice>;
  }
  // Undefined until a chat has loaded: a chat with no title is named, a missing one is not.
  const chatTitle = loading ? shown?.chatTitle : chat?.title;
  return (
    <>
      <LogsWindowBar
        cluster={loading ? shown?.cluster : cluster}
        chatTitle={chatTitle === undefined ? '' : chatTitleOf(chatTitle)}
      />
      {body}
    </>
  );
}

function View({
  view,
  grep,
  viewport,
  onGrepApplied,
  chatTitle,
  clusterName,
}: {
  view: LogsView;
  /** The grep the user applied here, over the open's, over the action's. */
  grep: string;
  viewport: LogViewportHandle;
  onGrepApplied: (grep: string) => void;
  chatTitle: string;
  clusterName: string;
}) {
  // The host builds the native title from these parts; an empty one keeps its place.
  const [first] = view.action.sources;
  const source = `${first.kind} ${first.name} in ${first.namespace}`;
  useEffect(() => {
    invoke('log_window_title', { parts: [source, chatTitle, clusterName] }).catch(reportHostError);
  }, [source, chatTitle, clusterName]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <LogsViewHeader view={view} grep={grep} />
      {/* Keyed on the call, as the panel's: a new call lands at its anchor. */}
      <LogViewer
        key={view.callId}
        clusterId={view.clusterId}
        query={{ ...view.action, grep }}
        openAt={view.action.anchor}
        pinToEnd={view.action.pinToEnd}
        viewport={viewport}
        onGrepApplied={onGrepApplied}
      />
    </div>
  );
}
