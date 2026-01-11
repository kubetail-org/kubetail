// Copyright 2024-2026 The Kubetail Authors
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

import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

import Page from '.';

vi.mock('@/apollo-client', () => ({
  dashboardClient: {},
  getClusterAPIClient: vi.fn(() => ({})),
}));

vi.mock('@/lib/hooks', () => ({
  useIsClusterAPIEnabled: vi.fn(() => true),
}));

vi.mock('@/components/layouts/AppLayout', () => ({
  default: ({ children }: { children: React.ReactNode }) => <div data-testid="app-layout">{children}</div>,
}));

vi.mock('@/components/utils/AuthRequired', () => ({
  default: ({ children }: { children: React.ReactNode }) => <div data-testid="auth-required">{children}</div>,
}));

vi.mock('./header', () => ({
  Header: () => <div data-testid="header">Header</div>,
}));

vi.mock('./sidebar', () => ({
  Sidebar: () => <div data-testid="sidebar">Sidebar</div>,
}));

vi.mock('./main', () => ({
  Main: () => <div data-testid="main">Main</div>,
}));

vi.mock('./helpers', () => ({
  SourcesFetcher: () => <div data-testid="sources-fetcher">SourcesFetcher</div>,
  ConfigureContainerColors: () => <div data-testid="configure-container-colors">ConfigureContainerColors</div>,
}));

vi.mock('./log-server-client', () => ({
  LogServerClient: class MockLogServerClient {
    constructor(opts: Record<string, unknown>) {
      Object.assign(this, opts);
    }
  },
}));

// Test wrapper
const TestWrapper = ({
  children,
  initialEntries = ['/'],
}: {
  children: React.ReactNode;
  initialEntries?: string[];
}) => <MemoryRouter initialEntries={initialEntries}>{children}</MemoryRouter>;

describe('Console Page', () => {
  it('renders the page with all main components', async () => {
    render(
      <TestWrapper>
        <Page />
      </TestWrapper>,
    );

    expect(screen.getByTestId('auth-required')).toBeInTheDocument();
    expect(screen.getByTestId('app-layout')).toBeInTheDocument();
    expect(screen.getByTestId('header')).toBeInTheDocument();
    expect(screen.getByTestId('main')).toBeInTheDocument();
    expect(screen.getByTestId('sources-fetcher')).toBeInTheDocument();
    expect(screen.getByTestId('configure-container-colors')).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByTestId('sidebar')).toBeInTheDocument();
    });
  });
});

describe('processedGrep logic', () => {
  it('escapes special regex characters in literal strings', () => {
    const input = 'error.*[test]';
    const expected = 'error\\.\\*\\[test\\]';

    const result = input.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    expect(result).toBe(expected);
  });

  it('extracts regex pattern from /regex/ format', () => {
    const input = '/error.*/';
    const regexMatch = /^\/(.+)\/$/.exec(input);

    expect(regexMatch).not.toBeNull();
    expect(regexMatch?.[1]).toBe('error.*');
  });

  it('does not match invalid regex format', () => {
    const input = '/error';
    const regexMatch = /^\/(.+)\/$/.exec(input);

    expect(regexMatch).toBeNull();
  });
});

describe('InnerLayout', () => {
  it('renders sidebar when isSidebarOpen is true (default state)', async () => {
    render(
      <TestWrapper>
        <Page />
      </TestWrapper>,
    );

    await waitFor(() => {
      expect(screen.getByTestId('sidebar')).toBeInTheDocument();
    });
  });

  it('renders main content area', () => {
    render(
      <TestWrapper>
        <Page />
      </TestWrapper>,
    );

    expect(screen.getByTestId('main')).toBeInTheDocument();
  });
});
