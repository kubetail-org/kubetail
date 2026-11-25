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
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { InlineState } from '@/lib/inline-log-lines';
import type { LogLine } from '@/lib/log-viewer';
import type { LogsViewAction } from '@/lib/logs-view';

import { LineRow, LogsViewInline } from './inline';

const action = (over: Partial<LogsViewAction> = {}): LogsViewAction => ({
  sources: [{ namespace: 'prod', kind: 'Pod', name: 'web-0', containers: [], allContainers: false, previous: false }],
  filters: [],
  grep: '',
  anchor: { kind: 'Tail', at: null },
  pinToEnd: true,
  resolvedAt: '2026-10-08T14:04:00Z',
  ...over,
});

const deployment = action({
  sources: [
    { namespace: 'prod', kind: 'Deployment', name: 'web', containers: [], allContainers: false, previous: false },
  ],
});
const head = action({ anchor: { kind: 'Head', at: null } });

const line = (i: number, over: Partial<LogLine> = {}): LogLine => ({
  timestamp: `2026-10-08T14:00:${String(i).padStart(2, '0')}Z`,
  cursor: `c${i}`,
  message: `line ${i}`,
  truncatedBytes: 0,
  stream: { namespace: 'prod', pod: 'web-0', container: 'app', previous: false },
  place: { node: 'n', region: '', zone: '', os: 'linux', arch: 'amd64' },
  gap: null,
  ...over,
});
const lines = (count: number) => Array.from({ length: count }, (_, i) => line(i));

const state = (over: Partial<InlineState> = {}): InlineState => ({
  lines: [],
  more: false,
  loading: false,
  failed: false,
  stopped: false,
  ...over,
});

const draw = (s: InlineState, a: LogsViewAction = action()) => render(<LogsViewInline action={a} state={s} />);
const rows = () => screen.queryAllByTestId('logs-inline-row').map((row) => row.textContent);
const gap = { after: '2026-10-08T13:59:00Z', before: '2026-10-08T14:00:00Z' };

describe('LogsViewInline', () => {
  it('is eight rows high loading, with fewer lines, and with none', () => {
    [state({ loading: true }), state({ lines: lines(3) }), state()].forEach((s) => {
      const { unmount } = draw(s);
      expect(screen.getByTestId('logs-inline')).toHaveStyle({ height: '192px' });
      unmount();
    });
  });

  it('says No lines when there is none and no more to read', () => {
    draw(state());
    expect(screen.getByTestId('logs-inline')).toHaveTextContent('No lines');
  });

  it('draws one row per line, its message', () => {
    draw(state({ lines: lines(3) }));
    expect(rows()).toHaveLength(3);
    expect(rows()[2]).toContain('line 2');
  });

  it('adds the lines that arrive, and keeps them when the card draws again', () => {
    const a = action();
    const first = state({ lines: lines(2), loading: true });
    const { rerender } = render(<LogsViewInline action={a} state={first} />);
    const grown = state({ lines: lines(3) });
    rerender(<LogsViewInline action={a} state={grown} />);
    rerender(<LogsViewInline action={a} state={grown} />);
    expect(rows()).toHaveLength(3);
    expect(rows()[2]).toContain('line 2');
  });

  it('spells a message and a pod name through VisibleText', () => {
    const { container } = draw(
      state({
        lines: [
          line(0, { message: 'ok‮evil', stream: { namespace: 'prod', pod: 'web‮1', container: 'app', previous: false } }),
        ],
      }),
      deployment,
    );
    expect(container.querySelectorAll('mark').length).toBeGreaterThanOrEqual(2);
  });

  it('names pod/container only when the view reads more than one stream', () => {
    const { unmount } = draw(state({ lines: [line(0)] }));
    expect(screen.queryByTestId('logs-inline-stream')).not.toBeInTheDocument();
    unmount();
    draw(state({ lines: [line(0)] }), deployment);
    expect(screen.getByTestId('logs-inline-stream')).toHaveTextContent('web-0/app');
  });

  it('a gap takes a row, displacing the oldest line of a tail window', () => {
    draw(state({ lines: [...lines(7), line(7, { gap })] }));
    expect(rows()).toHaveLength(7);
    expect(rows()[0]).toContain('line 1');
    expect(screen.getByTestId('log-gap')).toBeInTheDocument();
  });

  it('a gap takes a row, displacing the newest line of a head window', () => {
    draw(state({ lines: [line(0), line(1, { gap }), ...lines(8).slice(2)] }), head);
    expect(rows()).toHaveLength(7);
    expect(rows()[6]).toContain('line 6');
  });

  it('says more may match on the side the read stopped', () => {
    draw(state({ lines: lines(8), more: true }));
    expect(screen.getByTestId('logs-inline').firstElementChild).toHaveTextContent('More lines may match');
    expect(rows()[0]).toContain('line 1');
  });

  it('says more may match at the bottom of a head window', () => {
    draw(state({ lines: lines(8), more: true }), head);
    expect(screen.getByTestId('logs-inline').lastElementChild).toHaveTextContent('More lines may match');
    expect(rows().at(-1)).toContain('line 6');
  });

  it('says a stopped follow stopped, under its lines', () => {
    draw(state({ lines: lines(8), stopped: true }));
    expect(screen.getByTestId('logs-inline').lastElementChild).toHaveTextContent('Stopped following');
    expect(rows()).toHaveLength(7);
  });

  it('says a failed read failed', () => {
    draw(state({ failed: true }));
    expect(screen.getByTestId('logs-inline')).toHaveTextContent('Couldn’t read these logs');
  });
});

describe('LineRow', () => {
  const row = () => screen.getByTestId('logs-inline-row');

  it('ends a cut line with what the server cut', () => {
    render(<LineRow line={line(1, { truncatedBytes: 2048 })} oneStream />);
    expect(row()).toHaveTextContent('line 1 … 2 KiB cut');
  });

  it('mutes a tagged line and names its tag before its time', () => {
    render(<LineRow line={line(1)} oneStream tag="after" />);
    expect(row()).toHaveClass('text-muted-foreground');
    expect(row().firstElementChild).toHaveTextContent(/^after$/);
  });

  it('draws an untagged line as it is', () => {
    render(<LineRow line={line(1)} oneStream tag={null} />);
    expect(row()).not.toHaveClass('text-muted-foreground');
    // The row draws local time, so the expected text follows the machine's zone.
    const at = new Date(line(1).timestamp);
    const pad = (n: number) => String(n).padStart(2, '0');
    expect(row().firstElementChild).toHaveTextContent(
      `${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`,
    );
  });
});
