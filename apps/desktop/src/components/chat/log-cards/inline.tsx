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

// A view's lines on its transcript card: eight rows at a fixed height from the first paint,
// so a card filling in never moves the transcript, and no scroller of its own. One line per
// row, cut at the card's edge. Every name and message is cluster text, drawn through
// `VisibleText` as the viewer's rows are, a message with its colours.
import { cn } from '@kubetail/ui/lib/utils';

import { AnsiText } from '@/components/logs/ansi-text';
import { CutNotice, Dot, GapNotice, LOG_ROW_PX } from '@/components/logs/log-viewer';
import { VisibleText } from '@/components/primitives/visible-text';
import { INLINE_LINES, type InlineState } from '@/lib/inline-log-lines';
import { lineTime } from '@/lib/log-viewer';
import type { LogLine } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';

const MORE = 'More lines may match — Expand to read on';
const STOPPED = 'Stopped following';

type Row = { kind: 'line'; line: LogLine } | { kind: 'gap'; line: LogLine } | { kind: 'notice'; text: string };

// A pod with its default container reads one stream; any other source may read several.
export function readsOneStream({ sources }: Pick<LogsViewAction, 'sources'>): boolean {
  if (sources.length !== 1) return false;
  const [source] = sources;
  return source.kind === 'Pod' && !source.allContainers && source.containers.length <= 1;
}

// Each notice takes a row, displacing a line from the end the window is not anchored to.
function rowsOf({ lines, more, stopped }: InlineState, tail: boolean): Row[] {
  let rows: Row[] = lines.flatMap((line): Row[] =>
    line.gap
      ? [
          { kind: 'gap', line },
          { kind: 'line', line },
        ]
      : [{ kind: 'line', line }],
  );
  const room = INLINE_LINES - (more ? 1 : 0) - (stopped ? 1 : 0);
  if (tail) {
    rows = rows.slice(Math.max(rows.length - room, 0));
  } else {
    rows = rows.slice(0, room);
    // A gap's notice sits above its line, so it never ends the window alone.
    if (rows.at(-1)?.kind === 'gap') rows = rows.slice(0, -1);
  }
  if (more) {
    const notice: Row = { kind: 'notice', text: MORE };
    rows = tail ? [notice, ...rows] : [...rows, notice];
  }
  if (stopped) rows = [...rows, { kind: 'notice', text: STOPPED }];
  return rows;
}

// The notice of the hole before a line, a row of its own.
export function GapRow({ line }: { line: LogLine }) {
  return (
    <div style={{ height: LOG_ROW_PX }}>
      <GapNotice line={line} gap={line.gap!} />
    </div>
  );
}

/** A line outside a read's range is tagged with the side it lies on, and muted. */
export type LineTag = 'before' | 'after' | null;

export function LineRow({ line, oneStream, tag = null }: { line: LogLine; oneStream: boolean; tag?: LineTag }) {
  return (
    <div
      data-testid="logs-inline-row"
      className={cn('flex gap-2 px-2 whitespace-nowrap', tag !== null && 'text-muted-foreground')}
      style={{ height: LOG_ROW_PX }}
    >
      {tag !== null && <span className="shrink-0 italic">{tag}</span>}
      <span className="shrink-0 text-muted-foreground tabular-nums">{lineTime(line.timestamp)}</span>
      <Dot line={line} />
      {!oneStream && (
        <span data-testid="logs-inline-stream" className="shrink-0 text-muted-foreground">
          <VisibleText text={`${line.stream.pod}/${line.stream.container}`} />
        </span>
      )}
      <span className="min-w-0 truncate">
        <AnsiText text={line.message} />
        <CutNotice bytes={line.truncatedBytes} />
      </span>
    </div>
  );
}

export function LogsViewInline({ action, state }: { action: LogsViewAction; state: InlineState }) {
  const oneStream = readsOneStream(action);
  let body;
  if (state.failed) {
    body = <p className="px-2 text-muted-foreground">Couldn’t read these logs</p>;
  } else if (!state.loading && state.lines.length === 0 && !state.more && !state.stopped) {
    body = <p className="px-2 text-muted-foreground">No lines</p>;
  } else {
    body = rowsOf(state, action.anchor.kind === 'Tail').map((row) => {
      if (row.kind === 'notice') {
        return (
          <p key={row.text} className="px-2 text-muted-foreground italic" style={{ height: LOG_ROW_PX }}>
            {row.text}
          </p>
        );
      }
      if (row.kind === 'gap') return <GapRow key={`gap:${row.line.cursor}`} line={row.line} />;
      return <LineRow key={row.line.cursor} line={row.line} oneStream={oneStream} />;
    });
  }
  return (
    <div
      data-testid="logs-inline"
      aria-busy={state.loading}
      className="mt-2 overflow-hidden rounded border bg-background font-mono leading-6 text-foreground"
      style={{ height: INLINE_LINES * LOG_ROW_PX }}
    >
      {body}
    </div>
  );
}
