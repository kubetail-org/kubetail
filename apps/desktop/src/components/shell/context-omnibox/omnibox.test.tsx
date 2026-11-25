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

import { act, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

// Mocks ---------------------------------------------------------------

// The scope the omnibox switches, and the dialog host the picker's footer asks.
const { useActiveKubeContextMock, setContext, openDialog } = vi.hoisted(() => ({
  useActiveKubeContextMock: vi.fn(),
  setContext: vi.fn(),
  openDialog: vi.fn(),
}));
// A context provider draws its readers again when its value changes; `kubeChanged`
// does that after a `serveContexts`.
const { kube } = vi.hoisted(() => ({ kube: { version: 0, listeners: new Set<() => void>() } }));
vi.mock('@/lib/active-kube-context', async () => {
  const { useSyncExternalStore } = await import('react');
  const subscribe = (listener: () => void) => {
    kube.listeners.add(listener);
    return () => kube.listeners.delete(listener);
  };
  return {
    useActiveKubeContext: () => {
      useSyncExternalStore(subscribe, () => kube.version);
      return useActiveKubeContextMock();
    },
  };
});
const kubeChanged = () =>
  act(() => {
    kube.version += 1;
    kube.listeners.forEach((l) => l());
  });
vi.mock('@/lib/dialog', () => ({ useDialog: () => ({ openDialog }), useDialogHost: () => null }));

// The focused log view, which a switch clears.
const { clearView } = vi.hoisted(() => ({ clearView: vi.fn() }));
vi.mock('@/lib/logs-view', () => ({ useLogsView: () => ({ clear: clearView }) }));

// The cluster records the dot reads its verdict from; the lookups over them are the real ones.
const { records } = vi.hoisted(() => ({ records: { current: [] as unknown[] } }));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: records.current }),
}));

// The picker navigates itself rather than going through `setContext`, so
// the router is the seam for what it writes — and for where the window is, which
// decides whether the switch also has a chat route to leave. `mode-tabs.test.tsx` runs
// the tabs on the real router.
const { navigate, location } = vi.hoisted(() => ({ navigate: vi.fn(), location: { pathname: '/dashboard' } }));
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => navigate,
  useLocation: ({ select }: { select: (loc: { pathname: string }) => unknown }) => select(location),
  Link: ({ to, children, ...props }: { to: string; children?: ReactNode } & Record<string, unknown>) => (
    <a href={to} {...props}>
      {children}
    </a>
  ),
}));

const { ContextOmnibox } = await import('./omnibox');
const { clusterRecord } = await import('@/test-utils');

// Helpers -------------------------------------------------------------

function serveContexts(
  names: string[],
  over: { context?: string; phase?: string; clusterEntry?: Record<string, unknown> | null } = {},
) {
  const context = over.context ?? names[0] ?? '';
  useActiveKubeContextMock.mockReturnValue({
    context,
    contexts: names.map((name) => ({ name, cluster: name, user: name, clusterEntry: null })),
    noneSelected: context === '' && names.length > 0,
    active: { name: context, clusterEntry: over.clusterEntry ?? null },
    phase: over.phase ?? 'live',
    setContext,
  });
}

const trigger = () => screen.getByRole('button', { name: /^cluster:/i });
const statusButton = () => screen.getByRole('button', { name: /^cluster status:/i });

// The picker portals in asynchronously, so wait for its heading rather than reading it.
const pickerOpened = () => screen.findByText('Clusters', { selector: 'p' });
const pickerRow = (name: string) =>
  screen.getByText(name, { selector: '[data-testid="cluster-name"]' }).closest('button')!;

beforeEach(() => {
  vi.clearAllMocks();
  location.pathname = '/dashboard';
  records.current = [];
  serveContexts(['prod', 'staging']);
});

// Tests ---------------------------------------------------------------

describe('ContextOmnibox', () => {
  it('reads as one control, the cluster the window is on', () => {
    render(<ContextOmnibox />);
    expect(screen.getByTestId('context-omnibox').contains(trigger())).toBe(true);
  });

  it('is a 38px white pill with a shadow', () => {
    render(<ContextOmnibox />);
    const box = screen.getByTestId('context-omnibox');
    expect(box).toHaveClass('h-9.5', 'max-w-105', 'min-w-0', 'rounded-full', 'px-1', 'py-0.75', 'gap-3');
    expect(box).toHaveClass('bg-white', 'dark:bg-input/30', 'shadow-pill');
  });

  // It fits its content, so the container that compacts it is the column around it.
  it('fits its content, centred in a container that fills the column', () => {
    render(<ContextOmnibox />);
    const box = screen.getByTestId('context-omnibox');
    expect(box).not.toHaveClass('w-full');
    expect(box).not.toHaveClass('@container');
    expect(box.parentElement).toHaveClass('@container', 'w-full', 'justify-center');
  });

  it('holds the cluster chip, then the Chat and Dashboard tabs', () => {
    render(<ContextOmnibox />);
    const [chip, tabs] = Array.from(screen.getByTestId('context-omnibox').children);
    expect(chip).toBe(screen.getByTestId('cluster-chip'));
    expect(chip.contains(statusButton())).toBe(true);
    expect(chip.contains(trigger())).toBe(true);
    expect(tabs).toBe(screen.getByRole('navigation', { name: 'View' }));
    expect(screen.getByRole('link', { name: 'Chat' })).toHaveAttribute('href', '/chat');
    expect(screen.getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '/dashboard');
  });

  it('draws the chip as the status button and a left-aligned, semibold 13px name', () => {
    render(<ContextOmnibox />);
    expect(screen.getByTestId('cluster-chip')).toHaveClass('h-8', 'rounded-full', 'pl-1.5', 'hover:bg-app-bar-hover');
    expect(statusButton()).toHaveClass('size-7', 'rounded-full', 'text-muted-foreground');
    const name = screen.getByTestId('cluster-trigger-name');
    expect(name).toHaveClass('truncate', 'font-semibold', 'text-foreground');
    expect(name.parentElement).toHaveClass('text-[13px]');
    expect(trigger()).toHaveClass('pr-2.5');
  });

  // A strip of the pill outside every control would take a press and open nothing.
  it('covers the pill’s padding and gaps with its controls’ hit areas', () => {
    render(<ContextOmnibox />);
    // From inside the status button's own 1px border: the chip's 6px and the pill's 4px to
    // its left, and the chip's 2px and the pill's 3px above and below.
    expect(statusButton()).toHaveClass('border', 'border-transparent', 'after:-left-2.75', 'after:-inset-y-1.5');
    // The trigger holds the chip's gap and right padding; its hit area reaches half the 12px
    // gap before the tabs and the pill's top and bottom.
    expect(trigger()).toHaveClass('h-full', 'after:-right-1.5', 'after:-inset-y-0.75');
    // The track's reaches back over the other half of the gap, and to the pill's edge.
    expect(screen.getByRole('navigation', { name: 'View' })).toHaveClass(
      'shrink-0',
      'after:-left-1.5',
      'after:-right-1',
      'after:-inset-y-0.75',
    );
  });

  // Over the content, the hit area would take the hover from the TLS badge and the status
  // dot, and their tooltips with it. `isolate` keeps it above the pill's own fill.
  it('lays each control’s hit area behind its content', () => {
    render(<ContextOmnibox />);
    for (const control of [statusButton(), trigger(), screen.getByRole('navigation', { name: 'View' })]) {
      expect(control).toHaveClass('relative', 'isolate', 'after:absolute', 'after:-z-10');
    }
  });

  it('compacts to a narrow pill, keeping every control and the TLS warning', () => {
    serveContexts(['prod'], { clusterEntry: { server: 'https://prod:6443', insecureSkipTLSVerify: true } });
    render(<ContextOmnibox />);
    expect(screen.getByTestId('context-omnibox').parentElement).toHaveClass('@container');
    expect(screen.getByTestId('cluster-chip')).toHaveClass('@max-[14rem]:pl-0');
    expect(statusButton()).toHaveClass('@max-[14rem]:after:-left-1.25');
    expect(screen.getByTestId('cluster-trigger-name')).toHaveClass('truncate');
    expect(screen.getByTestId('cluster-trigger-name').parentElement).toHaveClass('@max-[14rem]:gap-1');
    // The words give way to an icon, the warning's tooltip and spoken name kept.
    const badge = screen.getByTitle(/insecure-skip-tls-verify/);
    expect(badge).toHaveClass('shrink-0', '@max-[14rem]:px-0.5');
    expect(badge.querySelector('svg')).toHaveClass('hidden', '@max-[14rem]:block');
    expect(screen.getByText('Unverified TLS')).toHaveClass('@max-[14rem]:sr-only');
  });

  it('opts out of the app bar’s drag region', () => {
    render(<ContextOmnibox />);
    // Pressing a control's padding should no more move the window than a
    // browser's address bar does.
    expect(screen.getByTestId('context-omnibox')).toHaveAttribute('data-tauri-drag-region', 'false');
  });

  it('shows the cluster in view', () => {
    serveContexts(['prod', 'staging'], { context: 'staging' });
    render(<ContextOmnibox />);
    expect(trigger()).toHaveTextContent('staging');
  });

  it.each([
    ['name', () => screen.getByText('prod', { selector: '[data-testid="cluster-trigger-name"]' })],
    ['TLS badge', () => screen.getByText('Unverified TLS')],
  ])('opens the cluster picker from the trigger’s %s', async (_part, target) => {
    serveContexts(['prod'], { clusterEntry: { server: 'https://prod:6443', insecureSkipTLSVerify: true } });
    const user = userEvent.setup();
    render(<ContextOmnibox />);

    await user.click(target());
    expect(await pickerOpened()).toBeInTheDocument();
  });

  it('opens the cluster’s status, and not the picker, from the server icon', async () => {
    const user = userEvent.setup();
    render(<ContextOmnibox />);

    await user.click(statusButton());
    expect(await screen.findByTestId('cluster-status')).toHaveAccessibleName('Cluster status');
    expect(screen.queryByText('Clusters', { selector: 'p' })).not.toBeInTheDocument();
  });

  // jsdom lays nothing out, so the positioner's alignment stands in for where it hangs.
  it('centres the status under the omnibox, as the picker is', async () => {
    const user = userEvent.setup();
    render(<ContextOmnibox />);

    await user.click(statusButton());
    const popup = await screen.findByTestId('cluster-status');
    await waitFor(() => expect(popup.closest('[data-align]')).toHaveAttribute('data-align', 'center'));
  });

  it('switches cluster from a picker row', async () => {
    const user = userEvent.setup();
    render(<ContextOmnibox />);

    await user.click(trigger());
    await pickerOpened();
    await user.click(pickerRow('staging'));

    expect(navigate).toHaveBeenCalledTimes(1);
    expect(navigate.mock.calls[0][0].search({ kubeContext: 'prod' })).toEqual({
      kubeContext: 'staging',
      chat: undefined,
    });
  });

  it('colours the server icon’s dot with the window’s cluster’s verdict, its summary as the title', () => {
    const dot = () => within(statusButton()).getByTestId('cluster-dot');
    records.current = [clusterRecord({ context: 'prod' })];
    const { unmount } = render(<ContextOmnibox />);
    expect(dot()).toHaveAttribute('data-tone', 'ok');
    expect(dot()).toHaveAttribute('title', 'Active · Current');
    expect(statusButton()).toHaveAccessibleName('Cluster status: Active · Current');
    unmount();

    records.current = [clusterRecord({ context: 'prod', connected: 'False' })];
    const second = render(<ContextOmnibox />);
    expect(dot()).toHaveAttribute('data-tone', 'error');
    expect(dot()).toHaveAttribute('title', 'Disconnected · Stalled');
    second.unmount();

    records.current = [clusterRecord({ context: 'staging' })];
    render(<ContextOmnibox />);
    expect(dot()).toHaveAttribute('data-tone', 'muted');
    expect(dot()).toHaveAttribute('title', 'Not reached');
  });

  it('warns when the cluster is reached with its certificate unchecked', () => {
    serveContexts(['prod'], { clusterEntry: { server: 'https://prod:6443', insecureSkipTLSVerify: true } });
    render(<ContextOmnibox />);
    // The one place the app says a connection goes unverified.
    expect(screen.getByText('Unverified TLS').parentElement).toHaveAttribute(
      'title',
      expect.stringContaining('insecure-skip-tls-verify'),
    );
  });

  it('warns for a plain http server too', () => {
    serveContexts(['prod'], { clusterEntry: { server: 'http://localhost:8080', insecureSkipTLSVerify: false } });
    render(<ContextOmnibox />);
    expect(screen.getByText('Unverified TLS').parentElement).toHaveAttribute(
      'title',
      expect.stringContaining('plain http'),
    );
  });

  // The button's name replaces its contents for a screen reader, the badge's words with them.
  it.each([
    { reason: 'insecure-skip-tls-verify', entry: { server: 'https://prod:6443', insecureSkipTLSVerify: true } },
    { reason: 'a plain http server', entry: { server: 'http://localhost:8080', insecureSkipTLSVerify: false } },
  ])('announces the warning with the cluster for $reason', ({ entry }) => {
    serveContexts(['prod'], { clusterEntry: entry });
    render(<ContextOmnibox />);
    expect(trigger()).toHaveAccessibleName('Cluster: prod, unverified TLS');
  });

  it('announces a verified cluster by name alone', () => {
    serveContexts(['prod'], { clusterEntry: { server: 'https://prod:6443', insecureSkipTLSVerify: false } });
    render(<ContextOmnibox />);
    expect(trigger()).toHaveAccessibleName('Cluster: prod');
  });

  it('stays quiet for a verified cluster', () => {
    serveContexts(['prod'], { clusterEntry: { server: 'https://prod:6443', insecureSkipTLSVerify: false } });
    render(<ContextOmnibox />);
    expect(screen.queryByText('Unverified TLS')).not.toBeInTheDocument();
  });

  it('keeps the cluster in view while the context list grows', () => {
    render(<ContextOmnibox />);
    serveContexts(['prod', 'staging', 'qa']);
    kubeChanged();
    expect(trigger()).toHaveTextContent('prod');
  });

  it('keeps saying it is connecting while the context watch answers again', () => {
    serveContexts([], { phase: 'connecting' });
    render(<ContextOmnibox />);
    kubeChanged();
    expect(trigger()).toHaveTextContent('Connecting…');
  });

  it('says it is still connecting rather than "No kubeconfig"', () => {
    serveContexts([], { phase: 'connecting' });
    render(<ContextOmnibox />);
    expect(screen.getByRole('button', { name: 'Cluster: connecting' })).toHaveTextContent('Connecting…');
  });

  // About 22px is left for the words at the window's 600px minimum: the spinner and the
  // bare icon say it, and the button's name keeps the words.
  it.each([
    { phase: 'connecting' as const, words: 'Connecting…', name: 'Cluster: connecting' },
    { phase: 'live' as const, words: 'No kubeconfig', name: 'Cluster: no kubeconfig' },
    { phase: 'live' as const, words: 'No cluster selected', name: 'Cluster: none selected', listed: ['prod'] },
  ])('drops "$words" in a narrow box, keeping it as the name', ({ phase, words, name, listed = [] }) => {
    serveContexts(listed, { phase, context: '' });
    render(<ContextOmnibox />);
    expect(screen.getByText(words)).toHaveClass('@max-[14rem]:hidden');
    expect(screen.getByRole('button', { name })).toBeInTheDocument();
  });

  // The picker still opens: a user with no kubeconfig reaches the dialog's orphaned
  // caches, and one whose window resolves no cluster picks one there.
  it.each([
    {
      what: 'there is no kubeconfig once connected with nothing in it',
      listed: [],
      name: 'Cluster: no kubeconfig',
      words: 'No kubeconfig',
    },
    {
      what: 'no cluster is selected when contexts are listed and none resolves',
      listed: ['prod', 'staging'],
      name: 'Cluster: none selected',
      words: 'No cluster selected',
    },
  ])('says $what, and still opens the picker', async ({ listed, name, words }) => {
    serveContexts(listed, { context: '', phase: 'live' });
    const user = userEvent.setup();
    render(<ContextOmnibox />);

    const empty = screen.getByRole('button', { name });
    expect(empty).toHaveTextContent(words);
    // No cluster, no status: the bare icon is the picker's.
    expect(screen.queryByRole('button', { name: /^cluster status/i })).not.toBeInTheDocument();
    expect(within(empty).getByTestId('cluster-glyph')).toBeInTheDocument();
    // Its hit area takes the status button's reach over the chip's and the pill's padding.
    expect(empty).toHaveClass('after:-left-2.5', '@max-[14rem]:after:-left-1');
    expect(empty).not.toHaveClass('pl-1.5');
    await user.click(empty);
    expect(await pickerOpened()).toBeInTheDocument();
  });
});
