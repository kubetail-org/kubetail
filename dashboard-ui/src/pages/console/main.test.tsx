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
import { createStore, Provider } from 'jotai';
import { createRef } from 'react';
import { MemoryRouter } from 'react-router-dom';

import type { LogViewerHandle, LogViewerVirtualRow } from '@/components/widgets/log-viewer';
import { PreferencesProvider } from '@/lib/preferences';

import { Main, RecordRow } from './main';
import { ALL_VIEWER_COLUMNS, PageContext, ViewerColumn } from './shared';
import { visibleColsAtom } from './state';

// Mock LogViewer and useLogViewerState
const mockUseLogViewerState = vi.fn();

vi.mock('@/components/widgets/log-viewer', () => ({
  LogViewer: () => <div data-testid="log-viewer" />,
  useLogViewerState: (...args: unknown[]) => mockUseLogViewerState(...args),
}));

// Create a default page context value for tests
const createDefaultPageContext = (overrides = {}) => ({
  kubeContext: 'test-context',
  shouldUseClusterAPI: true,
  logServerClient: undefined,
  grep: null,
  logViewerRef: createRef<LogViewerHandle>(),
  isSidebarOpen: true,
  setIsSidebarOpen: vi.fn(),
  ...overrides,
});

// Test wrapper that provides required providers
const TestWrapper = ({
  children,
  store,
  contextValue = createDefaultPageContext(),
  initialEntries = ['/'],
}: {
  children: React.ReactNode;
  store?: ReturnType<typeof createStore>;
  contextValue?: ReturnType<typeof createDefaultPageContext>;
  initialEntries?: string[];
}) => {
  const content = (
    <MemoryRouter initialEntries={initialEntries}>
      <PreferencesProvider>
        <PageContext.Provider value={contextValue}>{children}</PageContext.Provider>
      </PreferencesProvider>
    </MemoryRouter>
  );

  return store ? <Provider store={store}>{content}</Provider> : content;
};

beforeEach(() => {
  vi.clearAllMocks();
  mockUseLogViewerState.mockReturnValue({ isLoading: false });
});

describe('Main', () => {
  describe('LoadingOverlay', () => {
    it('shows loading overlay when isLoading is true', () => {
      mockUseLogViewerState.mockReturnValue({ isLoading: true });

      render(
        <TestWrapper>
          <Main />
        </TestWrapper>,
      );

      expect(screen.getByText('Loading')).toBeInTheDocument();
    });

    it('hides loading overlay when isLoading is false', () => {
      mockUseLogViewerState.mockReturnValue({ isLoading: false });

      render(
        <TestWrapper>
          <Main />
        </TestWrapper>,
      );

      expect(screen.queryByText('Loading')).not.toBeInTheDocument();
    });
  });

  describe('HeaderRow', () => {
    it('renders visible column headers', () => {
      const store = createStore();
      store.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.Message]));

      render(
        <TestWrapper store={store}>
          <Main />
        </TestWrapper>,
      );

      expect(screen.getByText('Timestamp')).toBeInTheDocument();
      expect(screen.getByText('Message')).toBeInTheDocument();
    });

    it('does not render hidden column headers', () => {
      const store = createStore();
      store.set(visibleColsAtom, new Set([ViewerColumn.Timestamp, ViewerColumn.Message]));

      render(
        <TestWrapper store={store}>
          <Main />
        </TestWrapper>,
      );

      expect(screen.queryByText('Pod')).not.toBeInTheDocument();
      expect(screen.queryByText('Container')).not.toBeInTheDocument();
      expect(screen.queryByText('Region')).not.toBeInTheDocument();
      expect(screen.queryByText('Node')).not.toBeInTheDocument();
    });

    it('renders all columns when all are visible', () => {
      const store = createStore();
      store.set(visibleColsAtom, new Set(ALL_VIEWER_COLUMNS));

      render(
        <TestWrapper store={store}>
          <Main />
        </TestWrapper>,
      );

      expect(screen.getByText('Timestamp')).toBeInTheDocument();
      expect(screen.getByText('Pod')).toBeInTheDocument();
      expect(screen.getByText('Container')).toBeInTheDocument();
      expect(screen.getByText('Region')).toBeInTheDocument();
      expect(screen.getByText('Zone')).toBeInTheDocument();
      expect(screen.getByText('OS')).toBeInTheDocument();
      expect(screen.getByText('Arch')).toBeInTheDocument();
      expect(screen.getByText('Node')).toBeInTheDocument();
      expect(screen.getByText('Message')).toBeInTheDocument();
    });

    it('renders columns in the order they appear in visibleColsAtom', () => {
      const store = createStore();
      // Pod before Timestamp — reversed from ALL_VIEWER_COLUMNS order
      store.set(visibleColsAtom, new Set([ViewerColumn.Pod, ViewerColumn.Timestamp, ViewerColumn.Message]));

      render(
        <TestWrapper store={store}>
          <Main />
        </TestWrapper>,
      );

      const colIds = [...document.querySelectorAll('[data-col-id]')].map((el) => el.getAttribute('data-col-id'));
      expect(colIds).toEqual(['Pos', ViewerColumn.Pod, ViewerColumn.Timestamp, ViewerColumn.Message]);
    });
  });

  describe('RecordRow timestamp timezone', () => {
    const mockRow: LogViewerVirtualRow = {
      index: 0,
      key: 0,
      size: 20,
      start: 0,
      record: {
        timestamp: '2024-06-15T10:30:01.123Z',
        message: 'test message',
        cursor: 'cursor-1',
        source: {
          metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
          namespace: 'default',
          podName: 'my-pod',
          containerName: 'my-container',
        },
      },
    };

    const defaultProps = {
      row: mockRow,
      gridTemplate: 'auto 1fr',
      visibleCols: new Set([ViewerColumn.Timestamp, ViewerColumn.Message]),
      timezone: 'UTC',
      timestampFormat: 'iso8601',
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCols: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsAbove: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsBelow: undefined as Set<ViewerColumn> | undefined,
      isCursorText: true,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellMouseDown: vi.fn(),
    };

    it('formats timestamps in ISO 8601 / UTC by default', () => {
      render(<RecordRow {...defaultProps} />);
      expect(screen.getByText('2024-06-15T10:30:01.123+00:00')).toBeInTheDocument();
    });

    it('formats timestamps in the selected timezone', () => {
      render(<RecordRow {...defaultProps} timezone="America/New_York" />);
      // 10:30 UTC = 06:30 EDT (June is DST)
      expect(screen.getByText('2024-06-15T06:30:01.123-04:00')).toBeInTheDocument();
    });

    it('formats timestamps using the selected timestamp format', () => {
      render(<RecordRow {...defaultProps} timestampFormat="rfc1123" />);
      expect(screen.getByText('Sat, 15 Jun 2024 10:30:01 +0000')).toBeInTheDocument();
    });
  });

  describe('RecordRow click behavior', () => {
    const mockRow: LogViewerVirtualRow = {
      index: 0,
      key: 0,
      size: 20,
      start: 0,
      record: {
        timestamp: '2024-06-15T10:30:01.123Z',
        message: 'test message',
        cursor: 'cursor-1',
        source: {
          metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
          namespace: 'default',
          podName: 'my-pod',
          containerName: 'my-container',
        },
      },
    };

    const defaultProps = {
      row: mockRow,
      gridTemplate: 'auto 1fr',
      visibleCols: new Set([ViewerColumn.Timestamp, ViewerColumn.Message]),
      timezone: 'UTC',
      timestampFormat: 'iso8601',
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCols: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsAbove: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsBelow: undefined as Set<ViewerColumn> | undefined,
      isCursorText: true,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellMouseDown: vi.fn(),
    };

    it('calls onRowMouseDown when mousedown on the pos cell', () => {
      render(<RecordRow {...defaultProps} />);
      const posCell = screen.getByRole('button');
      fireEvent.mouseDown(posCell);
      expect(defaultProps.onRowMouseDown).toHaveBeenCalledTimes(1);
      expect(defaultProps.onRowMouseDown).toHaveBeenCalledWith(0, expect.any(Object));
    });

    it('calls onCellMouseDown when mousedown on the timestamp cell', () => {
      const onCellMouseDown = vi.fn();
      render(<RecordRow {...defaultProps} onCellMouseDown={onCellMouseDown} />);
      const timestampCell = screen.getByText(/2024-06-15T/);
      fireEvent.mouseDown(timestampCell);
      expect(onCellMouseDown).toHaveBeenCalledWith(0, ViewerColumn.Timestamp, expect.any(Object));
    });

    it('calls onCellMouseDown when mousedown on the message cell', () => {
      const onCellMouseDown = vi.fn();
      render(<RecordRow {...defaultProps} onCellMouseDown={onCellMouseDown} />);
      const messageCell = screen.getByText('test message');
      fireEvent.mouseDown(messageCell);
      expect(onCellMouseDown).toHaveBeenCalledWith(0, ViewerColumn.Message, expect.any(Object));
    });

    it('does not call onRowMouseDown when clicking the row background', () => {
      const onRowMouseDown = vi.fn();
      render(<RecordRow {...defaultProps} onRowMouseDown={onRowMouseDown} />);
      const row = screen.getByRole('row');
      fireEvent.mouseDown(row);
      expect(onRowMouseDown).not.toHaveBeenCalled();
    });
  });

  describe('RecordRow cell selection', () => {
    const mockRow: LogViewerVirtualRow = {
      index: 0,
      key: 0,
      size: 20,
      start: 0,
      record: {
        timestamp: '2024-06-15T10:30:01.123Z',
        message: 'test message',
        cursor: 'cursor-1',
        source: {
          metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
          namespace: 'default',
          podName: 'my-pod',
          containerName: 'my-container',
        },
      },
    };

    const defaultProps = {
      row: mockRow,
      gridTemplate: 'auto 1fr',
      visibleCols: new Set([ViewerColumn.Timestamp, ViewerColumn.Message]),
      timezone: 'UTC',
      timestampFormat: 'iso8601',
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCols: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsAbove: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsBelow: undefined as Set<ViewerColumn> | undefined,
      isCursorText: true,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellMouseDown: vi.fn(),
    };

    it('does not call onCellMouseDown on ColorDot column', () => {
      const onCellMouseDown = vi.fn();
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.ColorDot, ViewerColumn.Message])}
          onCellMouseDown={onCellMouseDown}
        />,
      );
      const colorDotCell = document.querySelector('[data-col-id="Color Dot"]') as HTMLElement;
      fireEvent.mouseDown(colorDotCell);
      expect(onCellMouseDown).not.toHaveBeenCalled();
    });

    it('data cells have gridcell role', () => {
      render(<RecordRow {...defaultProps} />);
      const gridcells = screen.getAllByRole('gridcell');
      expect(gridcells.length).toBe(2); // Timestamp + Message
    });

    it('data cells have select-none by default', () => {
      render(<RecordRow {...defaultProps} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('select-none')).toBe(true);
    });

    it('single selected cell has all 4 edge shadows', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      const shadow = messageCell.style.boxShadow;
      expect(shadow).toContain('inset 0 2px 0 0'); // top
      expect(shadow).toContain('inset 0 -2px 0 0'); // bottom
      expect(shadow).toContain('inset 2px 0 0 0'); // left
      expect(shadow).toContain('inset -2px 0 0 0'); // right
    });

    it('non-selected cells have no boxShadow', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} />);
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      expect(timestampCell.style.boxShadow).toBe('');
    });

    it('adjacent selected cells share edges (no inner border)', () => {
      render(
        <RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])} />,
      );
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      // Timestamp: has left edge, no right edge (Message is adjacent)
      expect(timestampCell.style.boxShadow).toContain('inset 2px 0 0 0'); // left
      expect(timestampCell.style.boxShadow).not.toContain('inset -2px 0 0 0'); // no right
      // Message: no left edge (Timestamp is adjacent), has right edge
      expect(messageCell.style.boxShadow).not.toContain('inset 2px 0 0 0'); // no left
      expect(messageCell.style.boxShadow).toContain('inset -2px 0 0 0'); // right
    });

    it('cell with selectedCellColsAbove has no top border', () => {
      render(
        <RecordRow
          {...defaultProps}
          selectedCellCols={new Set([ViewerColumn.Message])}
          selectedCellColsAbove={new Set([ViewerColumn.Message])}
        />,
      );
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.style.boxShadow).not.toContain('inset 0 2px 0 0'); // no top
      expect(messageCell.style.boxShadow).toContain('inset 0 -2px 0 0'); // bottom
    });

    it('cell with selectedCellColsBelow has no bottom border', () => {
      render(
        <RecordRow
          {...defaultProps}
          selectedCellCols={new Set([ViewerColumn.Message])}
          selectedCellColsBelow={new Set([ViewerColumn.Message])}
        />,
      );
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.style.boxShadow).toContain('inset 0 2px 0 0'); // top
      expect(messageCell.style.boxShadow).not.toContain('inset 0 -2px 0 0'); // no bottom
    });

    it('selected cell in text-select mode has userSelect text style', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} isCellTextSelectable />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.style.userSelect).toBe('text');
    });

    it('non-selected cells do not have userSelect when another cell is text-selectable', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} isCellTextSelectable />);
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      expect(timestampCell.style.userSelect).toBe('');
    });

    it('unselected cells have cursor-default class', () => {
      render(<RecordRow {...defaultProps} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('cursor-default')).toBe(true);
    });

    it('selected cell with isCursorText has cursor-text class', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} isCursorText />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('cursor-text')).toBe(true);
    });

    it('selected cell without isCursorText has cursor-default class', () => {
      render(<RecordRow {...defaultProps} selectedCellCols={new Set([ViewerColumn.Message])} isCursorText={false} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('cursor-default')).toBe(true);
    });

    it('ColorDot cell has selection box-shadow when both neighbors are selected', () => {
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message])}
          selectedCellCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])}
        />,
      );
      const colorDotCell = document.querySelector('[data-col-id="Color Dot"]') as HTMLElement;
      expect(colorDotCell.style.boxShadow).not.toBe('');
    });

    it('ColorDot cell has no selection box-shadow when only one neighbor is selected', () => {
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message])}
          selectedCellCols={new Set([ViewerColumn.Message])}
        />,
      );
      const colorDotCell = document.querySelector('[data-col-id="Color Dot"]') as HTMLElement;
      expect(colorDotCell.style.boxShadow).toBe('');
    });

    it('selected cell adjacent to ColorDot has no inner border when other side also selected', () => {
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message])}
          selectedCellCols={new Set([ViewerColumn.Timestamp, ViewerColumn.Message])}
        />,
      );
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      // Timestamp should NOT have a right border since Message (across ColorDot) is also selected
      expect(timestampCell.style.boxShadow).not.toContain('inset -2px 0 0 0');
    });

    it('selected cell adjacent to ColorDot has border when other side is not selected', () => {
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.Timestamp, ViewerColumn.ColorDot, ViewerColumn.Message])}
          selectedCellCols={new Set([ViewerColumn.Timestamp])}
        />,
      );
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      // Timestamp SHOULD have a right border since Message is not selected
      expect(timestampCell.style.boxShadow).toContain('inset -2px 0 0 0');
    });
  });

  describe('RecordRow Key column', () => {
    const mockRow: LogViewerVirtualRow = {
      index: 0,
      key: 0,
      size: 20,
      start: 0,
      record: {
        timestamp: '2024-06-15T10:30:01.123Z',
        message: 'test message',
        cursor: 'cursor-1',
        source: {
          metadata: { region: 'us-east-1', zone: 'us-east-1a', os: 'linux', arch: 'amd64', node: 'node-1' },
          namespace: 'default',
          podName: 'my-pod',
          containerName: 'my-container',
        },
      },
    };

    const defaultProps = {
      row: mockRow,
      gridTemplate: 'auto auto 1fr',
      visibleCols: new Set([ViewerColumn.Timestamp, ViewerColumn.Message]),
      timezone: 'UTC',
      timestampFormat: 'iso8601',
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCols: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsAbove: undefined as Set<ViewerColumn> | undefined,
      selectedCellColsBelow: undefined as Set<ViewerColumn> | undefined,
      isCursorText: true,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellMouseDown: vi.fn(),
    };

    it('renders "0" when row key is 0', () => {
      render(<RecordRow {...defaultProps} row={{ ...mockRow, key: 0 }} />);
      expect(screen.getByText('0')).toBeInTheDocument();
    });

    it('renders "+2" when row key is positive', () => {
      render(<RecordRow {...defaultProps} row={{ ...mockRow, key: 2 }} />);
      expect(screen.getByText('+')).toBeInTheDocument();
      expect(screen.getByText('2')).toBeInTheDocument();
    });

    it('renders "-3" when row key is negative', () => {
      render(<RecordRow {...defaultProps} row={{ ...mockRow, key: -3 }} />);
      expect(screen.getByText('-')).toBeInTheDocument();
      expect(screen.getByText('3')).toBeInTheDocument();
    });
  });

  describe('LogViewer integration', () => {
    it('renders LogViewer when logServerClient is provided', () => {
      const mockClient = { fetch: vi.fn(), subscribe: vi.fn() };
      const contextValue = createDefaultPageContext({ logServerClient: mockClient });

      render(
        <TestWrapper contextValue={contextValue}>
          <Main />
        </TestWrapper>,
      );

      expect(screen.getByTestId('log-viewer')).toBeInTheDocument();
    });

    it('does not render LogViewer when logServerClient is undefined', () => {
      render(
        <TestWrapper>
          <Main />
        </TestWrapper>,
      );

      expect(screen.queryByTestId('log-viewer')).not.toBeInTheDocument();
    });
  });
});
