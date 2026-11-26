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
// A LogsRead call in the transcript: one block naming what the model read and over
// which range, the model's description, and the lines it read as the action holds
// them, stripped, redacted and cut, a line outside the range tagged with its side.
// Nothing is fetched, so a reload, an evicted log or a gone cluster draws the same
// lines. A footer says what the model saw beyond them. View around this makes the
// read's view the focused view, and Open in window moves it to a window, as a LogsView
// card's buttons do, in chat mode alone.
import { Button } from '@kubetail/ui/elements/button';
import { useState } from 'react';

import { ModelDescription } from '@/components/chat/model-description';
import { SourceLine } from '@/components/logs/log-source-line';
import { VisibleText } from '@/components/primitives/visible-text';
import type { AppMode } from '@/lib/app-mode';
import type { ChatToolCall } from '@/lib/chats';
import { INLINE_LINES } from '@/lib/inline-log-lines';
import { compareTimestamps, previousNanosecond } from '@/lib/log-viewer';
import type { LogLine } from '@/lib/log-viewer';
import type { LogsReadAction } from '@/lib/logs-view';
import { descriptionLine } from '@/lib/visible-text';

import { LogsViewButtons, useViewPlace } from './buttons';
import { GapRow, LineRow, type LineTag, readsOneStream } from './inline';

type Line = LogsReadAction['lines'][number];

const NO_PLACE = { node: '', region: '', zone: '', os: '', arch: '' };

// A sample's line as the viewer's rows draw one, a watch's too. Lines are keyed by
// index: a sample holds no cursor.
export function logLine(line: Line, i: number): LogLine {
  const { timestamp, namespace, pod, container, previous, message, truncatedBytes, gap } = line;
  return {
    timestamp,
    cursor: String(i),
    message,
    truncatedBytes,
    stream: { namespace, pod, container, previous },
    place: NO_PLACE,
    gap,
  };
}

type Padding = LogsReadAction['before'];

const padded = ({ lines, span }: Padding) => lines > 0 || span !== '';

function paddingWords({ lines, span }: Padding, side: 'before' | 'after'): string {
  if (lines > 0) return `${lines} ${lines === 1 ? 'line' : 'lines'} ${side}`;
  return span === '' ? '' : `${span} ${side}`;
}

const PAGE_FROM = { First: 'oldest first', Last: 'the newest' };

// The range as the model's header names it, its padding, and which end of it was read.
function rangeLine({ since, until, before, after, pageFrom }: LogsReadAction): string {
  return [
    since && `from ${since}`,
    until && `up to ${until}`,
    paddingWords(before, 'before'),
    paddingWords(after, 'after'),
    pageFrom && PAGE_FROM[pageFrom],
  ]
    .filter(Boolean)
    .join(', ');
}

// Where the range ends, null with none: `until`, else, with padding after an open end, the
// nanosecond before `resolvedAt`, where the sidecar ended it.
function rangeEnd({ until, after, resolvedAt }: LogsReadAction): string | null {
  if (until !== null) return until;
  return padded(after) ? previousNanosecond(resolvedAt) : null;
}

function tagOf({ timestamp }: Line, since: string | null, end: string | null): LineTag {
  if (since !== null && compareTimestamps(timestamp, since) < 0) return 'before';
  if (end !== null && compareTimestamps(timestamp, end) > 0) return 'after';
  return null;
}

// What the model saw beyond the lines, in the header's words, then what the page said.
function Beyond({ olderMore, newerMore, notices }: LogsReadAction) {
  const words = [olderMore && 'Older lines exist.', newerMore && 'Newer lines exist.'].filter(Boolean);
  if (words.length === 0 && notices.length === 0) return null;
  return (
    <p data-testid="logs-read-beyond" className="mt-1 opacity-70">
      {words.join(' ')}
      {notices.map(({ code, message }, i) => (
        <span key={`${code}:${message}`} title={code}>
          {(words.length > 0 || i > 0) && ' '}
          <VisibleText text={message} />
        </span>
      ))}
    </p>
  );
}

export function LogsReadCard({
  call,
  action,
  chatID,
  clusterID,
  mode,
}: {
  call: ChatToolCall;
  action: LogsReadAction;
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
  const oneStream = readsOneStream(action);
  const lines = all ? action.lines : action.lines.slice(0, INLINE_LINES);
  const { viewAround, since } = action;
  const end = rangeEnd(action);
  return (
    <div className="mt-1 rounded-md border px-3 py-2">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-foreground">
            Read: <SourceLine source={first} />
            {more > 0 && ` and ${more} more`}
          </p>
          <p>
            {rangeLine(action)}
            {action.grep !== '' && (
              <>
                {', matching '}
                <span className="font-mono">
                  /<VisibleText text={action.grep} />/
                </span>
              </>
            )}
          </p>
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
      <div
        data-testid="logs-read"
        className="mt-2 overflow-hidden rounded border bg-background font-mono leading-6 text-foreground"
      >
        {lines.length === 0 && <p className="px-2 text-muted-foreground">No lines</p>}
        {lines.map((read, i) => {
          const row = logLine(read, i);
          return (
            <div key={row.cursor}>
              {row.gap && <GapRow line={row} />}
              <LineRow line={row} oneStream={oneStream} tag={tagOf(read, since, end)} />
            </div>
          );
        })}
      </div>
      {!all && action.lines.length > INLINE_LINES && (
        <Button variant="ghost" size="sm" className="mt-1" onClick={() => setAll(true)}>
          Show all {action.lines.length} lines
        </Button>
      )}
      <Beyond {...action} />
    </div>
  );
}
