// Copyright 2024-2025 Andres Morey
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
import { createStore, Provider } from 'jotai';
import { createRef } from 'react';
import { MemoryRouter } from 'react-router-dom';

import type { LogViewerHandle } from '@/components/widgets/log-viewer';

import { Main } from './main';
import { ALL_VIEWER_COLUMNS, PageContext, ViewerColumn } from './shared';
import { visibleColsAtom } from './state';

// Mock LogViewer and useLogViewerState
const mockUseLogViewerState = vi.fn();

vi.mock('@/components/widgets/log-viewer', () => ({
  LogViewer: ({ children }: { children: React.ReactNode }) => <div data-testid="log-viewer">{children}</div>,
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

      expect(screen.queryByText('Pod/Container')).not.toBeInTheDocument();
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
      expect(screen.getByText('Pod/Container')).toBeInTheDocument();
      expect(screen.getByText('Region')).toBeInTheDocument();
      expect(screen.getByText('Zone')).toBeInTheDocument();
      expect(screen.getByText('OS')).toBeInTheDocument();
      expect(screen.getByText('Arch')).toBeInTheDocument();
      expect(screen.getByText('Node')).toBeInTheDocument();
      expect(screen.getByText('Message')).toBeInTheDocument();
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
