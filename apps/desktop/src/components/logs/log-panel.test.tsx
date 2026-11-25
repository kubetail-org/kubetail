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
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { createLogViewportHandle } from '@/lib/log-viewport';
import type { LogsView } from '@/lib/logs-view';

import { LogsViewHeader, LogsViewPanel } from './log-panel';

// The viewer has a suite of its own; this file is about the panel around it.
// The viewport the viewer was handed, by identity.
const { mountedWith } = vi.hoisted(() => ({ mountedWith: { viewport: {} } }));
vi.mock('@/components/logs/log-viewer', () => ({
  LogViewer: ({
    clusterId,
    query,
    openAt,
    pinToEnd,
    viewport,
    onGrepApplied,
  }: {
    clusterId: string;
    query: { grep: string };
    openAt: { kind: string; at: string | null };
    pinToEnd: boolean;
    viewport: object;
    onGrepApplied: (grep: string) => void;
  }) => {
    mountedWith.viewport = viewport;
    return (
      <div
        data-testid="log-viewer"
        data-cluster={clusterId}
        data-grep={query.grep}
        data-open-at={openAt.at ?? openAt.kind}
        data-pinned={pinToEnd}
      >
        <button type="button" onClick={() => onGrepApplied('panic')}>
          apply panic
        </button>
        <button type="button" onClick={() => onGrepApplied('')}>
          clear grep
        </button>
      </div>
    );
  },
}));

const view: LogsView = {
  clusterId: 'cluster-1',
  chatId: 'chat-1',
  callId: 'call-1',
  action: {
    sources: [
      {
        namespace: 'prod',
        kind: 'Deployment',
        name: 'webapp',
        containers: ['app', 'istio-proxy'],
        allContainers: false,
        previous: false,
      },
      { namespace: 'prod', kind: 'Pod', name: 'work\u202Eer-1', containers: [], allContainers: false, previous: true },
    ],
    filters: [{ field: 'Node', values: ['node-a', 'node-b'] }],
    grep: 'error|warn',
    anchor: { kind: 'At', at: '2026-10-08T14:02:00Z' },
    pinToEnd: false,
    resolvedAt: '2026-10-08T14:04:00Z',
  },
};

describe('LogsViewPanel', () => {
  it('names the first source and how many more, then every source, the filters and the grep', () => {
    render(
      <LogsViewPanel
        view={view}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByRole('heading')).toHaveTextContent(
      'Logs: Deployment webapp in prod (app, istio-proxy) and 1 more',
    );
    const items = screen.getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('Deployment webapp in prod (app, istio-proxy)');
    expect(items[1]).toHaveTextContent(/, previous instance$/);
    expect(screen.getByText(/^node:/)).toHaveTextContent('node: node-a, node-b');
    expect(screen.getByText(/matching/)).toHaveTextContent('matching /error|warn/');
    expect(screen.getByText(/^from /)).toHaveTextContent('from 2026-10-08T14:02:00Z');
  });

  it('says where the view opens and whether it is pinned', () => {
    const tail = { ...view, action: { ...view.action, anchor: { kind: 'Tail' as const, at: null }, pinToEnd: true } };
    render(
      <LogsViewPanel
        view={tail}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByText(/newest line/)).toHaveTextContent('at the newest line, pinned to the end');

    const head = { ...view, action: { ...view.action, anchor: { kind: 'Head' as const, at: null } } };
    render(
      <LogsViewPanel
        view={head}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByText(/^from the start$/)).toBeInTheDocument();
  });

  it('spells a reordering character in a name out, never drawing it', () => {
    render(
      <LogsViewPanel
        view={view}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByTitle('An invisible character')).toBeInTheDocument();
  });

  it('draws one source alone, with no filter and no grep lines', () => {
    render(
      <LogsViewPanel
        view={{
          ...view,
          action: { ...view.action, sources: [view.action.sources[0]], filters: [], grep: '' },
        }}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByRole('heading')).toHaveTextContent(/^Logs: Deployment webapp in prod \(app, istio-proxy\)$/);
    expect(screen.queryByText(/^node:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/matching/)).not.toBeInTheDocument();
  });

  it('closes from its button', () => {
    const onClose = vi.fn();
    render(
      <LogsViewPanel
        view={view}
        viewport={createLogViewportHandle()}
        onClose={onClose}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Close log view' }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it('opens the view in a window from its button', () => {
    const onOpenInWindow = vi.fn();
    render(
      <LogsViewPanel
        view={view}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={onOpenInWindow}
        onGrepApplied={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Open in window' }));
    expect(onOpenInWindow).toHaveBeenCalledOnce();
  });

  it('mounts the viewer on the view’s cluster with the viewport it is handed', () => {
    const viewport = createLogViewportHandle();
    render(
      <LogsViewPanel
        view={view}
        viewport={viewport}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-cluster', 'cluster-1');
    expect(mountedWith.viewport).toBe(viewport);
  });

  it('mounts the viewer at the action’s anchor, pinned as the action says', () => {
    render(
      <LogsViewPanel
        view={view}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-open-at', '2026-10-08T14:02:00Z');
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-pinned', 'false');
  });

  it('draws and runs the user’s grep over the action’s, and hands up each one the viewer applies', () => {
    const onGrepApplied = vi.fn();
    render(
      <LogsViewPanel
        view={{ ...view, grep: 'panic' }}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={onGrepApplied}
      />,
    );
    expect(screen.getByText(/matching/)).toHaveTextContent('matching /panic/');
    expect(screen.getByTestId('log-viewer')).toHaveAttribute('data-grep', 'panic');

    fireEvent.click(screen.getByRole('button', { name: 'clear grep' }));
    expect(onGrepApplied).toHaveBeenCalledWith('');
  });

  it('draws no grep line for a user’s empty grep over the action’s', () => {
    render(
      <LogsViewPanel
        view={{ ...view, grep: '' }}
        viewport={createLogViewportHandle()}
        onClose={() => {}}
        onOpenInWindow={() => {}}
        onGrepApplied={() => {}}
      />,
    );
    expect(screen.queryByText(/matching/)).not.toBeInTheDocument();
  });
});

describe('LogsViewPanel without a window handler', () => {
  it('draws the close alone', () => {
    render(
      <LogsViewPanel view={view} viewport={createLogViewportHandle()} onClose={() => {}} onGrepApplied={() => {}} />,
    );
    expect(screen.getByRole('button', { name: 'Close log view' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Open in window' })).not.toBeInTheDocument();
  });
});

describe('LogsViewHeader', () => {
  it('draws neither button without its handler', () => {
    render(<LogsViewHeader view={view} grep="panic" />);
    expect(screen.getByRole('heading')).toHaveTextContent(/^Logs: Deployment webapp/);
    expect(screen.getByText(/matching/)).toHaveTextContent('matching /panic/');
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  // The sidebar's toggle is in the app bar, so the header's top-right corner is its own.
  it('pads its header evenly', () => {
    render(<LogsViewHeader view={view} grep="panic" />);
    const header = screen.getByRole('heading').closest('header')!;
    expect(header).toHaveClass('p-4');
    expect(header.className).not.toMatch(/\bpr-/);
  });
});
