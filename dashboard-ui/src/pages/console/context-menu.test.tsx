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

import { render, screen, fireEvent, waitFor } from '@testing-library/react';

import type { LogRecord } from '@/components/widgets/log-viewer';

import { CellContextMenu } from './context-menu';
import { ViewerColumn } from './shared';

const makeRecord = (overrides: Partial<LogRecord> = {}): LogRecord => ({
  timestamp: '2024-06-15T10:30:01.123Z',
  message: '\x1b[31mERROR\x1b[0m: something failed',
  cursor: 'cursor-1',
  source: {
    metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
    namespace: 'default',
    podName: 'my-pod-abc',
    containerName: 'my-container',
  },
  ...overrides,
});

beforeEach(() => {
  Object.assign(navigator, {
    clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
  });
});

function renderWithContextMenu(
  col: ViewerColumn,
  record: LogRecord = makeRecord(),
  timezone = 'UTC',
  timestampFormat = 'iso8601',
) {
  render(
    <CellContextMenu col={col} record={record} timezone={timezone} timestampFormat={timestampFormat}>
      <div>cell content</div>
    </CellContextMenu>,
  );
}

async function openContextMenu() {
  const trigger = screen.getByText('cell content');
  fireEvent.contextMenu(trigger);
  // Wait for the menu to appear in the DOM
  await waitFor(() => {
    expect(screen.getByRole('menu')).toBeInTheDocument();
  });
}

describe('CellContextMenu', () => {
  describe('ColorDot column', () => {
    it('renders children without a context menu wrapper', () => {
      renderWithContextMenu(ViewerColumn.ColorDot);
      expect(screen.getByText('cell content')).toBeInTheDocument();
      fireEvent.contextMenu(screen.getByText('cell content'));
      expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    });
  });

  describe('Timestamp column', () => {
    it('shows "Copy timestamp" menu item', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      expect(screen.getByText('Copy timestamp')).toBeInTheDocument();
    });

    it('shows "Copy as..." submenu trigger', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      expect(screen.getByText('Copy as...')).toBeInTheDocument();
    });

    it('copies displayed timestamp on "Copy timestamp" click', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy timestamp'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2024-06-15T10:30:01.123+00:00');
    });

    it('copies timestamp in the selected timezone', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp, makeRecord(), 'America/New_York');
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy timestamp'));
      // 10:30 UTC = 06:30 EDT (June is DST)
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2024-06-15T06:30:01.123-04:00');
    });

    it('copies timestamp in the selected format', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp, makeRecord(), 'UTC', 'rfc1123');
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy timestamp'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('Sat, 15 Jun 2024 10:30:01 +0000');
    });

    it('shows timestamp format options in submenu', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy as...'));
      await waitFor(() => {
        expect(screen.getByText('ISO 8601')).toBeInTheDocument();
        expect(screen.getByText('Unix (seconds)')).toBeInTheDocument();
        expect(screen.getByText('Unix (milliseconds)')).toBeInTheDocument();
        expect(screen.getByText('Local time')).toBeInTheDocument();
      });
    });

    it('copies ISO 8601 format', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy as...'));
      await waitFor(() => expect(screen.getByText('ISO 8601')).toBeInTheDocument());
      fireEvent.click(screen.getByText('ISO 8601'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('2024-06-15T10:30:01.123Z');
    });

    it('copies Unix seconds', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy as...'));
      await waitFor(() => expect(screen.getByText('Unix (seconds)')).toBeInTheDocument());
      fireEvent.click(screen.getByText('Unix (seconds)'));
      const expected = String(Math.floor(new Date('2024-06-15T10:30:01.123Z').getTime() / 1000));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expected);
    });

    it('copies Unix milliseconds', async () => {
      renderWithContextMenu(ViewerColumn.Timestamp);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy as...'));
      await waitFor(() => expect(screen.getByText('Unix (milliseconds)')).toBeInTheDocument());
      fireEvent.click(screen.getByText('Unix (milliseconds)'));
      const expected = String(new Date('2024-06-15T10:30:01.123Z').getTime());
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(expected);
    });
  });

  describe('Message column', () => {
    it('shows "Copy message" and "Copy message (ANSI)" items', async () => {
      renderWithContextMenu(ViewerColumn.Message);
      await openContextMenu();
      expect(screen.getByText('Copy message')).toBeInTheDocument();
      expect(screen.getByText('Copy message (ANSI)')).toBeInTheDocument();
    });

    it('copies ANSI-stripped text on "Copy message"', async () => {
      renderWithContextMenu(ViewerColumn.Message);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy message'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('ERROR: something failed');
    });

    it('copies raw text with ANSI on "Copy message (ANSI)"', async () => {
      renderWithContextMenu(ViewerColumn.Message);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy message (ANSI)'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('\x1b[31mERROR\x1b[0m: something failed');
    });
  });

  describe('default columns (Pod, Container, etc.)', () => {
    it('shows a single "Copy" item for Pod column', async () => {
      renderWithContextMenu(ViewerColumn.Pod);
      await openContextMenu();
      expect(screen.getByText('Copy')).toBeInTheDocument();
    });

    it('copies pod name on "Copy" click', async () => {
      renderWithContextMenu(ViewerColumn.Pod);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-pod-abc');
    });

    it('copies container name for Container column', async () => {
      renderWithContextMenu(ViewerColumn.Container);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('my-container');
    });

    it('copies region for Region column', async () => {
      renderWithContextMenu(ViewerColumn.Region);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('us-east-1');
    });

    it('copies node for Node column', async () => {
      renderWithContextMenu(ViewerColumn.Node);
      await openContextMenu();
      fireEvent.click(screen.getByText('Copy'));
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('node-1');
    });
  });
});
