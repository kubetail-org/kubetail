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
// The log view the right sidebar draws in chat mode: a header naming what the
// focused view reads, *Open in window*, a close that collapses the sidebar, and the
// viewer under them. The panel does not scroll; the viewer owns its scroller. A log
// window draws the same header, without the buttons.
import { Button } from '@kubetail/ui/elements/button';
import { ExternalLink, X } from 'lucide-react';

import { Names, SourceLine } from '@/components/logs/log-source-line';
import { LogViewer } from '@/components/logs/log-viewer';
import { VisibleText } from '@/components/primitives/visible-text';
import type { LogViewportHandle } from '@/lib/log-viewport';
import { anchorLine } from '@/lib/logs-view';
import type { LogsView } from '@/lib/logs-view';

// `grep` is the viewer's, which its toolbar can change from the action's. Each button
// draws only with its handler.
export function LogsViewHeader({
  view,
  grep,
  onClose,
  onOpenInWindow,
}: {
  view: LogsView;
  grep: string;
  onClose?: () => void;
  onOpenInWindow?: () => void;
}) {
  const { sources, filters, anchor, pinToEnd } = view.action;
  const [first] = sources;
  const more = sources.length - 1;
  const detail = 'truncate text-xs text-muted-foreground';
  return (
    <header className="flex items-start gap-2 border-b p-4">
      <div className="min-w-0 flex-1 text-sm">
        <h2 className="truncate font-semibold">
          Logs: <SourceLine source={first} />
          {more > 0 && ` and ${more} more`}
        </h2>
        <ul className="text-xs text-muted-foreground">
          {sources.map((source) => (
            <li key={`${source.namespace}/${source.kind}/${source.name}`} className="truncate">
              <SourceLine source={source} />
            </li>
          ))}
        </ul>
        {filters.map(({ field, values }) => (
          <p key={field} className={detail}>
            {field.toLowerCase()}: <Names names={values} />
          </p>
        ))}
        <p className={detail}>
          {anchorLine(anchor)}
          {pinToEnd && ', pinned to the end'}
        </p>
        {grep !== '' && (
          <p className={detail}>
            matching{' '}
            <span className="font-mono">
              /<VisibleText text={grep} />/
            </span>
          </p>
        )}
      </div>
      {onOpenInWindow && (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Open in window"
          title="Open in window"
          onClick={onOpenInWindow}
        >
          <ExternalLink aria-hidden />
        </Button>
      )}
      {onClose && (
        <Button variant="ghost" size="icon-sm" aria-label="Close log view" onClick={onClose}>
          <X aria-hidden />
        </Button>
      )}
    </header>
  );
}

export function LogsViewPanel({
  view,
  viewport,
  onClose,
  onOpenInWindow,
  onGrepApplied,
}: {
  view: LogsView;
  viewport: LogViewportHandle;
  onClose: () => void;
  /** Absent until the log windows' provider is ready, which hides the button. */
  onOpenInWindow?: () => void;
  /** Called with each grep the user applies from the viewer's toolbar. */
  onGrepApplied: (grep: string) => void;
}) {
  const grep = view.grep ?? view.action.grep;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <LogsViewHeader view={view} grep={grep} onClose={onClose} onOpenInWindow={onOpenInWindow} />
      {/* Keyed on the call: a new call lands at its anchor, a re-render of the same
          call leaves the user where they scrolled. */}
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
