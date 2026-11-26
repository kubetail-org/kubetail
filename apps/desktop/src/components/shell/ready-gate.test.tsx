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

import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { mockTauriCore } from '@/test-utils';

const { invokeMock, factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { onError } = await import('@/lib/error-bus');
const { ReadyGate } = await import('./ready-gate');

describe('ReadyGate', () => {
  afterEach(() => {
    cleanup();
    invokeMock.mockReset();
  });

  it('draws the mark alone while pending', () => {
    invokeMock.mockReturnValue(new Promise(() => {}));
    render(
      <ReadyGate>
        <p>the app</p>
      </ReadyGate>,
    );
    expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true');
    expect(screen.getByTestId('app-mark')).toBeInTheDocument();
    expect(screen.getByTestId('drag-strip')).toHaveAttribute('data-tauri-drag-region', 'deep');
    // The app bar's height, so the window drags from the same band before it mounts.
    expect(screen.getByTestId('drag-strip')).toHaveClass('h-14');
    expect(screen.queryByText('the app')).not.toBeInTheDocument();
  });

  it('renders its children, and no mark, once the host is ready', async () => {
    invokeMock.mockResolvedValue(undefined);
    render(
      <ReadyGate>
        <p>the app</p>
      </ReadyGate>,
    );
    expect(await screen.findByText('the app')).toBeInTheDocument();
    expect(screen.queryByTestId('app-mark')).not.toBeInTheDocument();
  });

  it('reports a failed start to the bus as a network error', async () => {
    const seen = vi.fn();
    const off = onError(seen);
    invokeMock.mockRejectedValue(new Error('sidecar never bound'));

    render(
      <ReadyGate>
        <p>the app</p>
      </ReadyGate>,
    );
    expect(await screen.findByText('Failed to start: sidecar never bound')).toBeInTheDocument();
    expect(screen.getByTestId('app-mark')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    const strip = screen.getByTestId('drag-strip');
    expect(strip).toHaveAttribute('data-tauri-drag-region', 'deep');
    expect(strip).not.toContainElement(screen.getByRole('button', { name: 'Retry' }));
    off();

    expect(seen).toHaveBeenCalledTimes(1);
    expect(seen.mock.calls[0][0]).toMatchObject({ source: 'network', message: 'sidecar never bound' });
  });

  it('asks the host again on Retry, and renders its children once it answers', async () => {
    invokeMock.mockRejectedValueOnce(new Error('sidecar never bound'));
    render(
      <ReadyGate>
        <p>the app</p>
      </ReadyGate>,
    );
    const retry = await screen.findByRole('button', { name: 'Retry' });

    invokeMock.mockResolvedValue(undefined);
    fireEvent.click(retry);

    expect(screen.getByRole('status')).toHaveAttribute('aria-busy', 'true');
    expect(await screen.findByText('the app')).toBeInTheDocument();
    expect(invokeMock).toHaveBeenCalledTimes(2);
  });

  it('holds on the error screen under VITE_READY_GATE_HOLD=error', async () => {
    vi.stubEnv('VITE_READY_GATE_HOLD', 'error');
    invokeMock.mockResolvedValue(undefined);
    render(
      <ReadyGate>
        <p>the app</p>
      </ReadyGate>,
    );
    expect(await screen.findByText('Failed to start: held by VITE_READY_GATE_HOLD')).toBeInTheDocument();
    expect(invokeMock).not.toHaveBeenCalledWith('ready');
    vi.unstubAllEnvs();
  });
});
