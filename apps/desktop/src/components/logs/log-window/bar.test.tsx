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
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  MAC_USER_AGENT,
  NON_MAC_USER_AGENT,
  clusterRecord,
  mockTauriCore,
  mockTauriWindow,
  restoreUserAgent,
  setUserAgent,
} from '@/test-utils';

const { factory } = mockTauriWindow();
vi.mock('@tauri-apps/api/window', () => factory());

const { invokeMock, factory: coreFactory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => coreFactory());

const { onError } = await import('@/lib/error-bus');
const { LogsWindowBar } = await import('./bar');

beforeEach(() => {
  invokeMock.mockReset();
  invokeMock.mockResolvedValue(undefined);
  setUserAgent(NON_MAC_USER_AGENT);
});

afterEach(() => {
  restoreUserAgent();
});

describe('LogsWindowBar', () => {
  it("draws the cluster's dot and name and the chat's title", () => {
    render(<LogsWindowBar cluster={clusterRecord({ context: 'prod' })} chatTitle="Why is web down" />);
    expect(screen.getByTestId('cluster-dot')).toHaveAttribute('data-tone', 'ok');
    expect(screen.getByTestId('logs-window-cluster')).toHaveTextContent('prod');
    expect(screen.getByTestId('logs-window-chat')).toHaveTextContent('Why is web down');
  });

  it('spells an invisible character in the chat title out', () => {
    const reordered = 'web\u202Edown';
    render(<LogsWindowBar cluster={clusterRecord({ context: 'prod' })} chatTitle={reordered} />);
    expect(screen.getByTitle('An invisible character')).toBeInTheDocument();
  });

  it('draws no dot or name for a cluster the map lacks', () => {
    render(<LogsWindowBar cluster={undefined} chatTitle="Why is web down" />);
    expect(screen.queryByTestId('cluster-dot')).not.toBeInTheDocument();
    expect(screen.queryByTestId('logs-window-cluster')).not.toBeInTheDocument();
  });

  it('asks the host to show the chat, from a button that does not drag', () => {
    render(<LogsWindowBar cluster={undefined} chatTitle="" />);
    const button = screen.getByRole('button', { name: 'Show chat' });
    expect(button).toHaveAttribute('data-tauri-drag-region', 'false');
    fireEvent.click(button);
    expect(invokeMock).toHaveBeenCalledWith('log_window_show_chat');
  });

  it('reports a Show chat the host refuses', async () => {
    const reported = vi.fn();
    const off = onError(reported);
    invokeMock.mockRejectedValue(new Error('no pair'));
    render(<LogsWindowBar cluster={undefined} chatTitle="" />);
    fireEvent.click(screen.getByRole('button', { name: 'Show chat' }));
    await waitFor(() => expect(reported).toHaveBeenCalledWith(expect.objectContaining({ message: 'no pair' })));
    off();
  });

  it('is a 40px drag region', () => {
    render(<LogsWindowBar cluster={undefined} chatTitle="" />);
    const bar = screen.getByTestId('logs-window-bar');
    expect(bar).toHaveAttribute('data-tauri-drag-region', 'deep');
    expect(bar).toHaveClass('h-10');
  });

  it('draws the caption buttons and no menu off macOS', () => {
    render(<LogsWindowBar cluster={undefined} chatTitle="" />);
    expect(screen.getByRole('button', { name: 'Close' })).toBeInTheDocument();
    expect(screen.queryByTestId('traffic-light-gutter')).not.toBeInTheDocument();
  });

  it('leaves the traffic lights their gutter on macOS, with no caption buttons', () => {
    setUserAgent(MAC_USER_AGENT);
    render(<LogsWindowBar cluster={undefined} chatTitle="" />);
    expect(screen.getByTestId('traffic-light-gutter')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Close' })).not.toBeInTheDocument();
  });
});
