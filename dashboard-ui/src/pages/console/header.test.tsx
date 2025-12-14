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
import { createRef, type Dispatch, type ReactNode, type RefObject, type SetStateAction } from 'react';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { Header } from './header';
import { PageContext } from './shared';
import type { ViewerHandle } from './viewer';

const mockUseViewerMetadata = vi.fn();

vi.mock('@/components/widgets/DateRangeDropdown', () => ({
  DateRangeDropdown: ({ onChange, children }: { onChange: (args: any) => void; children: ReactNode }) => (
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

vi.mock('./viewer', () => ({
  useViewerMetadata: () => mockUseViewerMetadata(),
}));

const defaultViewerMetadata = { isReady: true, isLoading: false, isFollow: true, isSearchEnabled: true };

const buildViewerRef = () => {
  const ref = createRef<ViewerHandle>();
  ref.current = {
    seekHead: vi.fn(() => Promise.resolve()),
    seekTail: vi.fn(() => Promise.resolve()),
    seekTime: vi.fn(() => Promise.resolve()),
    play: vi.fn(),
    pause: vi.fn(),
  };
  return ref;
};

type RenderOptions = {
  search?: string;
  isSidebarOpen?: boolean;
  setIsSidebarOpen?: Dispatch<SetStateAction<boolean>>;
  viewerRef?: RefObject<ViewerHandle>;
};

const renderHeader = ({ search = '', isSidebarOpen = true, setIsSidebarOpen, viewerRef }: RenderOptions = {}) => {
  const store = createStore();
  const ref = viewerRef ?? buildViewerRef();
  const setSidebarOpen: Dispatch<SetStateAction<boolean>> =
    setIsSidebarOpen ?? (vi.fn() as Dispatch<SetStateAction<boolean>>);
  const contextValue = { isSidebarOpen, setIsSidebarOpen: setSidebarOpen };

  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: (
          <Provider store={store}>
            <PageContext.Provider value={contextValue}>
              <Header viewerRef={ref} />
            </PageContext.Provider>
          </Provider>
        ),
      },
    ],
    {
      initialEntries: [`/${search}`],
    },
  );

  return {
    router,
    viewerRef: ref,
    setIsSidebarOpen: contextValue.setIsSidebarOpen,
    ...render(<RouterProvider router={router} />),
  };
};

beforeEach(() => {
  mockUseViewerMetadata.mockReturnValue(defaultViewerMetadata);
});

describe('Header', () => {
  it('renders sidebar toggle when sidebar is closed', () => {
    const setIsSidebarOpen = vi.fn();
    renderHeader({ isSidebarOpen: false, setIsSidebarOpen });

    const toggleButton = screen.getByTitle('Collapse sidebar');
    expect(toggleButton).toBeInTheDocument();

    fireEvent.click(toggleButton);
    expect(setIsSidebarOpen).toHaveBeenCalledWith(true);
  });

  it('renders search input when search is enabled and pre-fills from query', () => {
    renderHeader({ search: '?grep=warning' });

    const input = screen.getByPlaceholderText('Match string or /regex/...');
    if (!(input instanceof HTMLInputElement)) {
      throw new Error('Input element not found');
    }
    expect(input.value).toBe('warning');
  });

  it('does not render search input when search is disabled', () => {
    mockUseViewerMetadata.mockReturnValue({ ...defaultViewerMetadata, isSearchEnabled: false });
    renderHeader();

    expect(screen.queryByPlaceholderText('Match string or /regex/...')).toBeNull();
  });

  it('updates search params and seeks time when date range is selected', async () => {
    const { router, viewerRef } = renderHeader();

    fireEvent.click(screen.getByRole('button', { name: 'Jump to time' }));

    await waitFor(() => {
      expect(viewerRef.current?.seekTime).toHaveBeenCalledWith('2024-01-01T00:00:00.000Z');
    });

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('time');
    expect(params.get('since')).toBe('2024-01-01T00:00:00.000Z');
  });

  it('jumps to the beginning and clears since filter', async () => {
    const { router, viewerRef } = renderHeader({ search: '?mode=tail&since=old' });

    fireEvent.click(screen.getByRole('button', { name: 'Jump to beginning' }));

    await waitFor(() => expect(viewerRef.current?.seekHead).toHaveBeenCalled());

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('head');
    expect(params.get('since')).toBeNull();
  });

  it('jumps to the end and clears since filter', async () => {
    const { router, viewerRef } = renderHeader({ search: '?mode=head&since=old' });

    fireEvent.click(screen.getByRole('button', { name: 'Jump to end' }));

    await waitFor(() => expect(viewerRef.current?.seekTail).toHaveBeenCalled());

    const params = new URLSearchParams(router.state.location.search);
    expect(params.get('mode')).toBe('tail');
    expect(params.get('since')).toBeNull();
  });

  it('shows pause when following and calls pause on click', () => {
    const { viewerRef } = renderHeader();

    const pauseButton = screen.getByRole('button', { name: 'Pause' });
    expect(pauseButton).toBeInTheDocument();

    fireEvent.click(pauseButton);
    expect(viewerRef.current?.pause).toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Play' })).toBeNull();
  });

  it('shows play when paused and calls play on click', () => {
    mockUseViewerMetadata.mockReturnValue({ ...defaultViewerMetadata, isFollow: false });
    const { viewerRef } = renderHeader();

    const playButton = screen.getByRole('button', { name: 'Play' });
    expect(playButton).toBeInTheDocument();

    fireEvent.click(playButton);
    expect(viewerRef.current?.play).toHaveBeenCalled();
    expect(screen.queryByRole('button', { name: 'Pause' })).toBeNull();
  });

  it('submits grep query and trims whitespace', async () => {
    const { router } = renderHeader();

    const input = screen.getByPlaceholderText('Match string or /regex/...');
    fireEvent.change(input, { target: { value: ' error ' } });
    fireEvent.submit(input.closest('form') as HTMLFormElement);

    await waitFor(() => {
      const params = new URLSearchParams(router.state.location.search);
      expect(params.get('grep')).toBe('error');
    });
  });
});
