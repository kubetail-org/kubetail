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
// A LogsWatch call in the transcript: one block naming what the watch follows, for
// what, from when and until when, the model's description, then the task's state.
// A running watch says so; a finished one draws the lines it held as its task
// carries them, stripped, redacted and cut, with why it ended. Nothing is fetched,
// so a reload, an evicted log or a gone cluster draws the same card. View around
// this makes the watch's view at its first line the focused view, and Open in window
// moves it to a window, as a LogsRead card's buttons do, in chat mode alone.
import { Button } from '@kubetail/ui/elements/button';
import { useState } from 'react';
import type { ReactNode } from 'react';

import { ModelDescription } from '@/components/chat/model-description';
import { SourceLine } from '@/components/logs/log-source-line';
import { VisibleText } from '@/components/primitives/visible-text';
import type { AppMode } from '@/lib/app-mode';
import type { ChatToolCall } from '@/lib/chats';
import { INLINE_LINES } from '@/lib/inline-log-lines';
import type { LogsWatchAction, LogsWatchTask } from '@/lib/logs-view';
import { descriptionLine } from '@/lib/visible-text';

import { LogsViewButtons, useViewPlace } from './buttons';
import { GapRow, LineRow, readsOneStream } from './inline';
import { logLine } from './read-card';

// A watch's byte limit when the call names none.
const DEFAULT_MAX_BYTES = 16384;

// What the watch waits for, saying only what differs from a bare watch: the
// pattern, the start when it is not the call's own moment, and the limits.
function Waits({ action }: { action: LogsWatchAction }) {
  const limits = [
    action.limit > 1 && `${action.limit} lines`,
    action.maxBytes !== DEFAULT_MAX_BYTES && `${action.maxBytes} bytes`,
  ].filter(Boolean);
  const from = action.since !== action.resolvedAt ? ` from ${action.since}` : '';
  const upTo = limits.length > 0 ? `, up to ${limits.join(' or ')},` : '';
  return (
    <p>
      {'watching for '}
      {action.grep === '' ? (
        'any line'
      ) : (
        <span className="font-mono">
          /<VisibleText text={action.grep} />/
        </span>
      )}
      {`${from}${upTo} until ${action.until}`}
    </p>
  );
}

// Where a watch with no line stands, in the card's words; null for none: a
// matched watch draws its lines instead, and a call whose task has not arrived
// has nothing to say. A failure's reason can carry the cluster's words, so it is
// drawn as text.
function stateOf(task: LogsWatchTask | null, until: string): ReactNode {
  switch (task?.status) {
    case 'Running':
      return 'watching…';
    case 'Expired':
      return `nothing matched by ${until}`;
    case 'Failed':
      return (
        <>
          ended: <VisibleText text={task.endedBecause} />
        </>
      );
    case 'Stopped':
      return 'stopped';
    case 'Lost':
      return 'lost when Kubetail stopped';
    case 'Matched':
    // A command's and an agent's ends.
    case 'Exited':
    case 'Completed':
    case undefined:
      return null;
  }
}

export function LogsWatchCard({
  call,
  action,
  task,
  chatID,
  clusterID,
  mode,
}: {
  call: ChatToolCall;
  action: LogsWatchAction;
  /** The call's task; null until the call's row names one. */
  task: LogsWatchTask | null;
  chatID: string;
  /** The chat's cluster. Undefined until the list watch delivers the chat. */
  clusterID: string | undefined;
  mode: AppMode;
}) {
  const place = useViewPlace(chatID, call.id, mode);
  const [all, setAll] = useState(false);
  const [first] = action.sources;
  const more = action.sources.length - 1;
  const line = descriptionLine(call.action?.description ?? '');
  const held = task?.matched ?? [];
  const lines = all ? held : held.slice(0, INLINE_LINES);
  const viewAround = task?.viewAround ?? null;
  const state = held.length === 0 ? stateOf(task, action.until) : null;
  return (
    <div className="mt-1 rounded-md border px-3 py-2">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-foreground">
            Watch: <SourceLine source={first} />
            {more > 0 && ` and ${more} more`}
          </p>
          <Waits action={action} />
          {line !== '' && <ModelDescription line={line} />}
        </div>
        {mode === 'chat' && viewAround && (
          <LogsViewButtons
            label="View around this"
            place={place}
            chatID={chatID}
            callID={call.id}
            clusterID={clusterID}
            action={viewAround}
          />
        )}
      </div>
      {state !== null && <p className="mt-1 opacity-70">{state}</p>}
      {held.length > 0 && (
        <>
          <div
            data-testid="logs-watch"
            className="mt-2 overflow-hidden rounded border bg-background font-mono leading-6 text-foreground"
          >
            {lines.map((matched, i) => {
              const row = logLine(matched, i);
              return (
                <div key={row.cursor}>
                  {row.gap && <GapRow line={row} />}
                  <LineRow line={row} oneStream={readsOneStream(action)} />
                </div>
              );
            })}
          </div>
          {!all && held.length > INLINE_LINES && (
            <Button variant="ghost" size="sm" className="mt-1" onClick={() => setAll(true)}>
              Show all {held.length} lines
            </Button>
          )}
          {task?.endedBecause && (
            <p className="mt-1 opacity-70">
              ended because <VisibleText text={task.endedBecause} />
            </p>
          )}
        </>
      )}
    </div>
  );
}
