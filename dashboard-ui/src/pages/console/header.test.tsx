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

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { createStore, Provider } from 'jotai';
import type { ReactNode } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';

import { createMockLogViewerHandle } from '@/components/widgets/log-viewer/mock';

import { Header } from './header';
import { PageContext } from './shared';

vi.mock('@/components/widgets/log-viewer', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/components/widgets/log-viewer')>();
  return {
    ...actual,
    useLogViewerState: () => ({ isLoading: false }),
  };
});

vi.mock('@/components/widgets/DateRangeDropdown', () => ({
  DateRangeDropdown: ({ onChange, children }: { onChange: (args: unknown) => void; children: ReactNode }) => (
    <div
      role="button"
      tabIndex={0}
      data-testid="date-range-dropdown"
      onClick={() => onChange({ since: new Date('2024-01-01T00:00:00.000Z'), until: null })}
      onKeyDown={(ev) => {
        if (ev.key === 'Enter' || ev.key === ' ') {
          ev.preventDefault();
          onChange({ since: new Date('2024-01-01T00:00:00.000Z'), until: null });
        }
      }}
    >
      {children}
    </div>
  ),
}));

// Helper to render Header with router (needed for URL assertions)
const renderHeader = ({
  initialEntries = ['/'],
  shouldUseClusterAPI,
  isSidebarOpen = true,
  setIsSidebarOpen = vi.fn(),
}: {
  initialEntries?: string[];
  shouldUseClusterAPI?: boolean;
  isSidebarOpen?: boolean;
  setIsSidebarOpen?: React.Dispatch<React.SetStateAction<boolean>>;
} = {}) => {
  const store = createStore();
  const logViewerRef = { current: createMockLogViewerHandle() };

  const contextValue = {
    kubeContext: null,
    shouldUseClusterAPI,
    logServerClient: undefined,
    grep: null,
    logViewerRef,
    isSidebarOpen,
    setIsSidebarOpen,
  };

  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: (
          <Provider store={store}>
            <PageContext.Provider value={contextValue}>
              <Header />
            </PageContext.Provider>
          </Provider>
        ),
      },
    ],
    { initialEntries },
  );

  render(<RouterProvider router={router} />);

  return { router, logViewerRef, setIsSidebarOpen };
};

describe('Header', () => {
  it('renders sidebar toggle when sidebar is closed', () => {
    const setIsSidebarOpen = vi.fn();
    renderHeader({ isSidebarOpen: false, setIsSidebarOpen });

    const toggleButton = screen.getByTitle('Collapse sidebar');
    expect(toggleButton).toBeInTheDocument();

    fireEvent.click(toggleButton);
    expect(setIsSidebarOpen).toHaveBeenCalledWith(true);
  });

  it('does not render search input when search is disabled', () => {
    renderHeader();

    expect(screen.queryByPlaceholderText('Match string or /regex/...')).toBeNull();
  });

  it('renders search input when search is enabled and pre-fills from query', () => {
    renderHeader({ initialEntries: ['/?grep=warning'], shouldUseClusterAPI: true });

    const input = screen.getByPlaceholderText('Match string or /regex/...');
    expect(input).toBeInstanceOf(HTMLInputElement);
    expect((input as HTMLInputElement).value).toBe('warning');
  });

  it('updates search params and seeks time when date range is selected', async () => {
    const { router, logViewerRef } = renderHeader();

    fireEvent.click(screen.getByRole('button', { name: 'Jump to time' }));

    await waitFor(() => {
      expect(logViewerRef.current?.jumpToCursor).toHaveBeenCalledWith('2024-01-01T00:00:00.000Z');
    });

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('cursor');
    expect(params.get('cursor')).toBe('2024-01-01T00:00:00.000Z');
  });

  it('jumps to the beginning and clears cursor', async () => {
    const { router, logViewerRef } = renderHeader({ initialEntries: ['/?mode=tail&cursor=old'] });

    fireEvent.click(screen.getByRole('button', { name: 'Jump to beginning' }));

    await waitFor(() => expect(logViewerRef.current?.jumpToBeginning).toHaveBeenCalled());

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('head');
    expect(params.get('cursor')).toBeNull();
  });

  it('jumps to the end and clears since filter', async () => {
    const { router, logViewerRef } = renderHeader({ initialEntries: ['/?mode=head&cursor=old'] });

    fireEvent.click(screen.getByRole('button', { name: 'Jump to end' }));

    await waitFor(() => expect(logViewerRef.current?.jumpToEnd).toHaveBeenCalled());

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('tail');
    expect(params.get('cursor')).toBeNull();
  });

  it('shows pause when following and toggles to play on click', () => {
    renderHeader();

    const pauseButton = screen.getByRole('button', { name: 'Pause' });
    expect(pauseButton).toBeInTheDocument();

    fireEvent.click(pauseButton);
    expect(screen.getByRole('button', { name: 'Play' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
  });

  it('shows play when paused and toggles to pause on click', () => {
    renderHeader();

    // First pause to get into paused state
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }));

    const playButton = screen.getByRole('button', { name: 'Play' });
    expect(playButton).toBeInTheDocument();

    fireEvent.click(playButton);
    expect(screen.getByRole('button', { name: 'Pause' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Play' })).toBeNull();
  });

  it('submits grep query and trims whitespace', async () => {
    const { router } = renderHeader({ shouldUseClusterAPI: true });

    const input = screen.getByPlaceholderText('Match string or /regex/...');
    fireEvent.change(input, { target: { value: ' error ' } });
    fireEvent.submit(input.closest('form') as HTMLFormElement);

    await waitFor(() => {
      const params = new URLSearchParams(router.state.location.search);
      expect(params.get('grep')).toBe('error');
    });
  });
});
