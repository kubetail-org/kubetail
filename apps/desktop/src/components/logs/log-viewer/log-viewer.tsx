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

// The log viewer: the library's core behind a view's props. It builds the view's client
// once per mount (the caller keys it on the call's id, so a new call is a fresh landing
// and a re-render of the same call moves nothing) and again for each grep the toolbar
// submits, lands at `openAt`, starts pinned as `pinToEnd` says, draws each line as a row,
// and writes what is on screen and what is selected into `viewport` for whoever pulls it.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { cn } from '@kubetail/ui/lib/utils';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useClient } from 'urql';

import { VisibleText } from '@/components/primitives/visible-text';
import {
  type Client,
  type CoreHandle,
  type CoreRow,
  type CoreRows,
  type Edge,
  type EdgeState,
  type LogLine,
  LogViewerCore,
  createCoreStore,
  useCoreFields,
} from '@/lib/log-viewer';
import { EMPTY_LOG_VIEWPORT, type LogViewportHandle } from '@/lib/log-viewport';
import { createLogsClient } from '@/lib/logs-client';
import type { LogsViewAction } from '@/lib/logs-view';
import { usePersistedFlag } from '@/lib/persisted-flag';

import { type Column, useLogViewerColumns } from './columns';
import { LogViewerHeader } from './header';
import { useMessageWidth } from './message-width';
import { LOG_ROW_H, LOG_ROW_PX, LogViewerRow, estimateRowHeight, gridTemplate } from './row';
import { hasMultipleSelectedCells, useSelection } from './selection';
import { LogViewerToolbar } from './toolbar';
import { useMeasureWidths } from './widths';

export type LogViewerProps = {
  /** The view's cluster, which the client reads. */
  clusterId: string;
  query: LogsViewAction;
  /** Where the viewer lands on mount. */
  openAt: LogsViewAction['anchor'];
  /** Whether it starts pinned to the end. */
  pinToEnd: boolean;
  viewport: LogViewportHandle;
  /** Called with each grep the toolbar submits and the sidecar accepts. */
  onGrepApplied: (grep: string) => void;
};

const measureHeight = (el: Element) => el.getBoundingClientRect().height;

const PAGE_FAILED = 'Could not load more lines.';

const errorText = (error: unknown) => (error instanceof Error ? error.message : String(error));

// The client as the core sees it, recording the last failure for the viewer to draw: the
// core never says what failed, and nothing reports a success. `errorReportExchange` has
// already reported the operation's error. A follow's error is passed on, so the core still
// unpins.
function watchClient(client: Client, record: (error: string | null) => void): Client {
  return {
    fetch: (query) =>
      client.fetch(query).then(
        (page) => {
          record(null);
          return page;
        },
        (error: unknown) => {
          record(PAGE_FAILED);
          throw error;
        },
      ),
    tail: (cursor, seen, onFrame, onFailed) => {
      try {
        const stop = client.tail(cursor, seen, onFrame, (error) => {
          record(errorText(error));
          onFailed(error);
        });
        record(null);
        return stop;
      } catch (error) {
        record(errorText(error));
        throw error;
      }
    },
  };
}

// Drawn at the top of the sizer while the before edge has more, and at its foot while the
// after edge does, each the height the core leaves for it. The core pages again only on the
// user's next scroll, so after an empty page — a read that spent its budget — the button
// reads on by hand.
function EdgeRow({ edge, state, loadMore }: { edge: Edge; state: EdgeState; loadMore: (edge: Edge) => void }) {
  let body = null;
  if (state.loading) {
    body = 'Loading…';
  } else if (state.empty) {
    body = (
      <>
        No more in the last read
        <Button variant="link" size="sm" className="h-auto p-0 text-xs" onClick={() => loadMore(edge)}>
          Keep looking
        </Button>
      </>
    );
  }
  return (
    <div
      className={cn(
        LOG_ROW_H,
        'absolute left-0 flex items-center gap-2 text-muted-foreground',
        edge === 'before' ? 'top-0' : 'bottom-0',
      )}
    >
      {body}
    </div>
  );
}

// Over the header and the rows while a landing is in flight (kubetail's `LoadingOverlay`).
function LoadingOverlay() {
  return (
    <div className="absolute inset-0 z-50 flex items-center justify-center bg-muted opacity-85">
      <div className="flex items-center space-x-4 rounded-md border bg-background p-3">
        <div>Loading</div>
        <Spinner size="xs" />
      </div>
    </div>
  );
}

type SelectionBand = { top: number; height: number };

// Kubetail's: one band per run of selected rows on screen, painted behind them, so
// neighbouring rows leave no seam between their fills.
function computeSelectionBands(rows: CoreRow[], selectedKeys: Set<number>): SelectionBand[] {
  const bands: SelectionBand[] = [];
  let start: CoreRow | null = null;
  let end: CoreRow | null = null;
  const flush = () => {
    if (start && end) bands.push({ top: start.start, height: end.start + end.size - start.start });
    start = null;
    end = null;
  };
  rows.forEach((row) => {
    if (!selectedKeys.has(row.key)) {
      flush();
      return;
    }
    const adjacent = end !== null && row.key === end.key + 1 && Math.abs(row.start - (end.start + end.size)) < 0.5;
    if (adjacent) {
      end = row;
    } else {
      flush();
      start = row;
      end = row;
    }
  });
  flush();
  return bands;
}

type RowsProps = {
  rows: CoreRows;
  columns: Column[];
  gridTemplate: string;
  widths: ReturnType<typeof useMeasureWidths>;
  wrap: boolean;
  scroller: HTMLDivElement | null;
  onSelectionChange: (lines: LogLine[]) => void;
  onRetry: () => void;
};

// Drawn by the core's render function under the landing's key, so a landing — whose keys
// start again at 0 — mounts a fresh selection.
function Rows({
  rows,
  columns,
  gridTemplate: template,
  widths,
  wrap,
  scroller,
  onSelectionChange,
  onRetry,
}: RowsProps) {
  const {
    selectedKeys,
    selectionTopKeys,
    selectionBottomKeys,
    selectedCells,
    anchorCell,
    handleRowMouseDown,
    handleCellMouseDown,
  } = useSelection(rows, scroller, columns, onSelectionChange);
  const isMultiCellSelection = hasMultipleSelectedCells(selectedCells);

  // While loading, the overlay over the header and the rows says so.
  let status = null;
  if (rows.failed) {
    status = (
      <>
        <p>Could not load lines.</p>
        <Button variant="outline" size="sm" onClick={onRetry}>
          Try again
        </Button>
      </>
    );
  } else if (!rows.loading && rows.count === 0) {
    status = <p>No lines.</p>;
  }
  return (
    <div
      className="relative"
      style={{ height: rows.totalSize, minWidth: wrap ? undefined : widths.rowWidth || '100%' }}
    >
      {status && (
        <div className="absolute inset-x-0 top-0 flex flex-col items-center gap-2 p-4 text-sm text-muted-foreground">
          {status}
        </div>
      )}
      {rows.before.more && <EdgeRow edge="before" state={rows.before} loadMore={rows.loadMore} />}
      {computeSelectionBands(rows.rows, selectedKeys).map((band) => (
        <div
          key={band.top}
          aria-hidden
          data-selection-band
          className="pointer-events-none absolute inset-x-0"
          style={{ top: band.top, height: band.height, backgroundColor: 'var(--selection-band)' }}
        />
      ))}
      {rows.rows.map((row) => (
        <LogViewerRow
          key={row.key}
          line={row.line}
          rowKey={row.key}
          columns={columns}
          gridTemplate={template}
          widths={widths.widths}
          rowWidth={widths.rowWidth}
          wrap={wrap}
          top={row.start}
          index={row.index}
          measureCell={widths.measureCell}
          measureRow={widths.measureRow}
          measureElement={wrap ? rows.measureElement : undefined}
          selected={selectedKeys.has(row.key)}
          selectionTop={selectionTopKeys.has(row.key)}
          selectionBottom={selectionBottomKeys.has(row.key)}
          selectedCellCols={selectedCells.get(row.key)}
          // A hole's notice parts a row's cells from the row above, so a ring does not join across it.
          selectedCellColsAbove={row.line.gap ? undefined : selectedCells.get(row.key - 1)}
          selectedCellColsBelow={rows.lineOfKey(row.key + 1)?.gap ? undefined : selectedCells.get(row.key + 1)}
          anchorCol={isMultiCellSelection && anchorCell?.rowKey === row.key ? anchorCell.col : undefined}
          onRowMouseDown={handleRowMouseDown}
          onCellMouseDown={handleCellMouseDown}
        />
      ))}
      {rows.after.more && <EdgeRow edge="after" state={rows.after} loadMore={rows.loadMore} />}
    </div>
  );
}

/** What the viewer draws from the core's state; the visible range is the viewport's alone. */
const VIEWER_FIELDS = ['loading', 'failed', 'ended', 'notices'] as const;

export function LogViewer({ clusterId, query, openAt, pinToEnd, viewport, onGrepApplied }: LogViewerProps) {
  // Built once per mount, and again per accepted grep; it opens nothing until the core asks.
  const urql = useClient();
  const [error, setError] = useState<string | null>(null);
  const [client, setClient] = useState(() => watchClient(createLogsClient(urql, clusterId, query), setError));

  const [store] = useState(createCoreStore);
  const state = useCoreFields(store, VIEWER_FIELDS);
  const core = useRef<CoreHandle>(null);

  const [columns, setColumns] = useLogViewerColumns();
  const template = useMemo(() => gridTemplate(columns), [columns]);
  const widths = useMeasureWidths();
  const [wrap, persistWrap] = usePersistedFlag('log-viewer-wrap', false);
  // Chrome like the pin: a new view starts playing, which the mount's key gives for free.
  const [paused, setPaused] = useState(false);
  // Each landing mounts its own scroller; the header and the message width follow whichever is mounted.
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);

  const messageWidth = useMessageWidth(scroller, widths.widths, columns, wrap);
  const wrapped = useCallback((line: LogLine) => estimateRowHeight(line, messageWidth), [messageWidth]);
  // Flipping wrap changes every row's height. The core measures again in the same commit,
  // holding the reader's row as it was before the flip.
  const setWrap = (on: boolean) => {
    persistWrap(on);
    core.current?.measure();
  };

  // The range moves with the store, the selection with the rows, and the pause and the
  // grep with the toolbar; each write carries all four.
  const selection = useRef<LogLine[]>([]);
  const pausedNow = useRef(false);
  const grepNow = useRef(query.grep);
  const writeViewport = useCallback(() => {
    const { range, atEnd, pinnedToEnd, visible } = store.getSnapshot();
    viewport.set({
      range,
      atEnd,
      pinnedToEnd,
      paused: pausedNow.current,
      visible,
      grep: grepNow.current,
      selection: selection.current,
      selectedCount: selection.current.length,
    });
  }, [store, viewport]);
  const onPausedChange = (next: boolean) => {
    setPaused(next);
    pausedNow.current = next;
    writeViewport();
  };
  const onSelectionChange = useCallback(
    (lines: LogLine[]) => {
      selection.current = lines;
      writeViewport();
    },
    [writeViewport],
  );

  // The grep in effect. A submitted one takes over only once the sidecar accepts it, so a
  // refused pattern leaves the last good view in place. Submissions can overlap; the latest
  // alone counts.
  const [grep, setGrep] = useState(query.grep);
  const [grepError, setGrepError] = useState<string | null>(null);
  const submissions = useRef(0);
  const onGrepChange = (next: string) => {
    submissions.current += 1;
    // The grep in effect needs no read and no landing; submitting it drops a pending one.
    if (next === grep) {
      setGrepError(null);
      return;
    }
    const mine = submissions.current;
    const candidate = createLogsClient(urql, clusterId, { ...query, grep: next });
    // The sidecar compiles a grep before it reads, so a one-line read says whether it holds.
    candidate.fetch({ kind: 'Last', limit: 1 }).then(
      () => {
        if (mine !== submissions.current) return;
        setGrep(next);
        setGrepError(null);
        setClient(watchClient(candidate, setError));
        onGrepApplied(next);
        // The landing writes the viewport as it publishes, so no write pairs the old
        // lines with the new grep. It mounts a fresh selection.
        grepNow.current = next;
        selection.current = [];
        // The new client lands as the last landing did; jump to where the user is now instead.
        const { range, pinnedToEnd } = store.getSnapshot();
        if (pinnedToEnd) core.current?.jumpTo({ kind: 'Tail', at: null });
        else if (range !== null) core.current?.jumpTo({ kind: 'At', at: range.from });
      },
      (refused: unknown) => {
        if (mine === submissions.current) setGrepError(errorText(refused));
      },
    );
  };

  // A closed view leaves no range behind for the composer to pull.
  useEffect(() => {
    writeViewport();
    const unsubscribe = store.subscribe(writeViewport);
    return () => {
      unsubscribe();
      viewport.set(EMPTY_LOG_VIEWPORT);
    };
  }, [store, viewport, writeViewport]);
  // Nor a pending grep to write one back: the handle outlives the viewer, and the next
  // view's viewer shares it.
  useEffect(
    () => () => {
      submissions.current += 1;
    },
    [],
  );

  const muted = 'border-b px-3 py-1 text-xs text-muted-foreground';
  return (
    <>
      <LogViewerToolbar
        loading={state.loading}
        paused={paused}
        columns={columns}
        wrap={wrap}
        grep={grep}
        grepError={grepError}
        onJumpTo={(at) => core.current?.jumpTo({ kind: 'At', at: at.toISOString() })}
        onStart={() => core.current?.jumpTo({ kind: 'Head', at: null })}
        onEnd={() => core.current?.jumpTo({ kind: 'Tail', at: null })}
        onPausedChange={onPausedChange}
        onColumnsChange={setColumns}
        onWrapChange={setWrap}
        onGrepChange={onGrepChange}
      />
      {state.notices.map(({ code, message }) => (
        <p key={`${code}:${message}`} className={muted} title={code}>
          <VisibleText text={message} />
        </p>
      ))}
      {state.ended !== null && (
        <p className={muted}>
          No new lines: <VisibleText text={state.ended} />
        </p>
      )}
      {/* A failed landing says so over the rows, and the landing Try again starts shows the overlay alone. */}
      {error !== null && !state.loading && !state.failed && (
        <p className={muted}>
          <VisibleText text={error} />
        </p>
      )}
      {/* `relative` anchors the loading overlay over the header and the lines. */}
      <div className="relative flex min-h-0 flex-1 flex-col">
        {state.loading && <LoadingOverlay />}
        <LogViewerHeader
          gridTemplate={template}
          columns={columns}
          widths={widths.widths}
          rowWidth={widths.rowWidth}
          wrap={wrap}
          scroller={scroller}
          measureCell={widths.measureCell}
        />
        <LogViewerCore
          ref={core}
          scrollerRef={setScroller}
          client={client}
          anchor={openAt}
          pinToEnd={pinToEnd}
          paused={paused}
          store={store}
          firstPageSize={300}
          pageSize={250}
          edgeRowHeight={LOG_ROW_PX}
          estimateRowHeight={wrap ? wrapped : estimateRowHeight}
          measureElement={wrap ? measureHeight : undefined}
          className="relative min-h-0 w-full flex-1 font-mono text-xs/6 "
        >
          {(rows) => (
            <Rows
              rows={rows}
              columns={columns}
              gridTemplate={template}
              widths={widths}
              wrap={wrap}
              scroller={scroller}
              onSelectionChange={onSelectionChange}
              onRetry={() => core.current?.retry()}
            />
          )}
        </LogViewerCore>
      </div>
    </>
  );
}
