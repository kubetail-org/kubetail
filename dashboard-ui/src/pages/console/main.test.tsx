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
      <PageContext.Provider value={contextValue}>{children}</PageContext.Provider>
    </MemoryRouter>
  );

  if (store) {
    return <Provider store={store}>{content}</Provider>;
  }

  return content;
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
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCol: null as ViewerColumn | null,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellClick: vi.fn(),
    };

    it('calls onRowMouseDown when mousedown on the pos cell', () => {
      render(<RecordRow {...defaultProps} />);
      const posCell = screen.getByRole('button');
      fireEvent.mouseDown(posCell);
      expect(defaultProps.onRowMouseDown).toHaveBeenCalledTimes(1);
      expect(defaultProps.onRowMouseDown).toHaveBeenCalledWith(0, expect.any(Object));
    });

    it('calls onCellClick when clicking the timestamp cell', () => {
      const onCellClick = vi.fn();
      render(<RecordRow {...defaultProps} onCellClick={onCellClick} />);
      const timestampCell = screen.getByText(/Jun 15, 2024/);
      fireEvent.click(timestampCell);
      expect(onCellClick).toHaveBeenCalledWith(0, ViewerColumn.Timestamp, expect.any(Object));
    });

    it('calls onCellClick when clicking the message cell', () => {
      const onCellClick = vi.fn();
      render(<RecordRow {...defaultProps} onCellClick={onCellClick} />);
      const messageCell = screen.getByText('test message');
      fireEvent.click(messageCell);
      expect(onCellClick).toHaveBeenCalledWith(0, ViewerColumn.Message, expect.any(Object));
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
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCol: null as ViewerColumn | null,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellClick: vi.fn(),
    };

    it('sets userSelect to auto on mousedown of selected cell', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      fireEvent.mouseDown(messageCell);
      expect(messageCell.style.userSelect).toBe('auto');
    });

    it('does not set userSelect on mousedown of unselected cell', () => {
      render(<RecordRow {...defaultProps} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      fireEvent.mouseDown(messageCell);
      expect(messageCell.style.userSelect).toBe('');
    });

    it('does not call onCellClick on ColorDot column', () => {
      const onCellClick = vi.fn();
      render(
        <RecordRow
          {...defaultProps}
          visibleCols={new Set([ViewerColumn.ColorDot, ViewerColumn.Message])}
          onCellClick={onCellClick}
        />,
      );
      const colorDotCell = document.querySelector('[data-col-id="Color Dot"]') as HTMLElement;
      fireEvent.click(colorDotCell);
      expect(onCellClick).not.toHaveBeenCalled();
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

    it('selected cell shows ring highlight', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('ring-2')).toBe(true);
      expect(messageCell.classList.contains('ring-blue-500')).toBe(true);
    });

    it('non-selected cells do not show ring highlight', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} />);
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      expect(timestampCell.classList.contains('ring-2')).toBe(false);
    });

    it('selected cell in text-select mode has userSelect auto style', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} isCellTextSelectable />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.style.userSelect).toBe('auto');
    });

    it('non-selected cells do not have userSelect auto when another cell is text-selectable', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} isCellTextSelectable />);
      const timestampCell = document.querySelector('[data-col-id="Timestamp"]') as HTMLElement;
      expect(timestampCell.style.userSelect).toBe('');
    });

    it('unselected cells have cursor-default class', () => {
      render(<RecordRow {...defaultProps} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('cursor-default')).toBe(true);
    });

    it('selected cell has cursor-text class', () => {
      render(<RecordRow {...defaultProps} selectedCellCol={ViewerColumn.Message} />);
      const messageCell = document.querySelector('[data-col-id="Message"]') as HTMLElement;
      expect(messageCell.classList.contains('cursor-text')).toBe(true);
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
      isWrap: false,
      isSelected: false,
      isSelectionTop: false,
      isSelectionBottom: false,
      maxRowWidth: 500,
      colWidths: new Map<ViewerColumn, number>(),
      selectedCellCol: null as ViewerColumn | null,
      isCellTextSelectable: false,
      measureElement: vi.fn(),
      measureRowElement: vi.fn(),
      measureCellElement: vi.fn(),
      onRowMouseDown: vi.fn(),
      onCellClick: vi.fn(),
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
