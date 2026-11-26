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
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

// Each tab has a suite of its own; this one is about the frame and which tab is mounted.
vi.mock('./yaml-view', () => ({ ObjectYamlView: () => <div data-testid="yaml-view" /> }));
vi.mock('./events-view', () => ({
  ObjectEventsView: () => <div data-testid="events-view" />,
}));
vi.mock('./logs-view', () => ({ ObjectLogsView: () => <div data-testid="logs-view" /> }));

const { ObjectPanel } = await import('./panel');

const POD = { apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u1', namespace: 'shop', name: 'web-1' };

describe('ObjectPanel', () => {
  it('names the object, and mounts the open tab alone', () => {
    const { rerender } = render(<ObjectPanel object={POD} tab="yaml" onTab={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByRole('heading')).toHaveTextContent('Pod: web-1');
    expect(screen.getByText('shop')).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'YAML' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('yaml-view')).toBeInTheDocument();
    expect(screen.queryByTestId('events-view')).not.toBeInTheDocument();

    rerender(<ObjectPanel object={POD} tab="events" onTab={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByTestId('events-view')).toBeInTheDocument();
    expect(screen.queryByTestId('yaml-view')).not.toBeInTheDocument();

    rerender(<ObjectPanel object={POD} tab="logs" onTab={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getByTestId('logs-view')).toBeInTheDocument();
    expect(screen.queryByTestId('events-view')).not.toBeInTheDocument();
  });

  // The sidebar's toggle is in the app bar, so the header's top-right corner is its own.
  it('pads its header evenly, keeping its corner for its own close', () => {
    render(<ObjectPanel object={POD} tab="yaml" onTab={vi.fn()} onClose={vi.fn()} />);
    const header = screen.getByRole('heading').closest('header')!;
    expect(header).toHaveClass('p-4');
    expect(header.className).not.toMatch(/\bpr-/);
  });

  it('offers Logs for a kind a log view reads alone', () => {
    const { rerender } = render(<ObjectPanel object={POD} tab="yaml" onTab={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(['YAML', 'Events', 'Logs']);
    const service = { ...POD, resource: 'services', kind: 'Service' };
    rerender(<ObjectPanel object={service} tab="yaml" onTab={vi.fn()} onClose={vi.fn()} />);
    expect(screen.getAllByRole('tab').map((t) => t.textContent)).toEqual(['YAML', 'Events']);
  });

  it('draws no namespace line for a cluster-scoped object', () => {
    render(
      <ObjectPanel object={{ ...POD, kind: 'Node', namespace: '' }} tab="yaml" onTab={vi.fn()} onClose={vi.fn()} />,
    );
    expect(screen.getByRole('heading')).toHaveTextContent('Node: web-1');
    expect(screen.queryByText('shop')).not.toBeInTheDocument();
  });

  it('asks for another tab, and closes', async () => {
    const user = userEvent.setup();
    const onTab = vi.fn();
    const onClose = vi.fn();
    render(<ObjectPanel object={POD} tab="yaml" onTab={onTab} onClose={onClose} />);
    await user.click(screen.getByRole('tab', { name: 'Events' }));
    expect(onTab).toHaveBeenCalledWith('events');
    await user.click(screen.getByRole('button', { name: 'Close object' }));
    expect(onClose).toHaveBeenCalledOnce();
  });
});
