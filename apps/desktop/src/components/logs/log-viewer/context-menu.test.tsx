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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { lineTime } from '@/lib/log-viewer';
import type { LogLine } from '@/lib/log-viewer';

import type { Column } from './columns';
import { CellContextMenu } from './context-menu';

const makeLine = (overrides: Partial<LogLine> = {}): LogLine => ({
  timestamp: '2024-06-15T10:30:01.123456789Z',
  cursor: 'cursor-1',
  message: 'ERROR: something failed',
  truncatedBytes: 0,
  stream: { namespace: 'default', pod: 'my-pod-abc', container: 'my-container', previous: false },
  place: { node: 'node-1', region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64' },
  gap: null,
  ...overrides,
});

beforeEach(() => {
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
});

function renderWithContextMenu(col: Column, line: LogLine = makeLine()) {
  render(
    <CellContextMenu col={col} line={line}>
      <div>cell content</div>
    </CellContextMenu>,
  );
}

async function openContextMenu() {
  fireEvent.contextMenu(screen.getByText('cell content'));
  await waitFor(() => {
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
}

describe('CellContextMenu', () => {
  describe('Dot column', () => {
    it('renders children without a context menu wrapper', () => {
      renderWithContextMenu('Dot');
      expect(screen.getByText('cell content')).toBeInTheDocument();
      fireEvent.contextMenu(screen.getByText('cell content'));
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  describe('Timestamp column', () => {
    it('shows "Copy timestamp" and "Copy as RFC 3339"', async () => {
      renderWithContextMenu('Timestamp');
      await openContextMenu();
      expect(screen.getByText('Copy timestamp')).toBeInTheDocument();
      expect(screen.getByText('Copy as RFC 3339')).toBeInTheDocument();
    });

    it('copies displayed timestamp on "Copy timestamp" click', async () => {
      const line = makeLine();
      renderWithContextMenu('Timestamp', line);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy timestamp'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(lineTime(line.timestamp));
    });

    it('copies the timestamp as the client gave it on "Copy as RFC 3339"', async () => {
      renderWithContextMenu('Timestamp');
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy as RFC 3339'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2024-06-15T10:30:01.123456789Z');
    });
  });

  describe('Message column', () => {
    it('copies the message on "Copy message"', async () => {
      renderWithContextMenu('Message', makeLine({ message: 'a​b' }));
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy message'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('a​b');
    });
  });

  describe('default columns (Pod, Container, etc.)', () => {
    it('shows a single "Copy" item for Pod column', async () => {
      renderWithContextMenu('Pod');
      await openContextMenu();
      expect(screen.getAllByRole('menuitem').map((item) => item.textContent)).toEqual(['Copy']);
    });

    it.each([
      ['Pod', 'my-pod-abc'],
      ['Container', 'my-container'],
      ['Region', 'us-east-1'],
      ['Node', 'node-1'],
    ] as const)('copies the %s on "Copy" click', async (col, want) => {
      renderWithContextMenu(col);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(want);
    });
  });
});
