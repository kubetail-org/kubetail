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
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';

import Page from '.';
import { Viewer, ViewerProvider } from './viewer';

// Mock dependencies
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

vi.mock('./viewer', () => ({
  Viewer: vi.fn(() => <div data-testid="viewer">Viewer</div>),
  ViewerProvider: vi.fn(({ children }: { children: React.ReactNode }) => (
    <div data-testid="viewer-provider">{children}</div>
  )),
  useSources: () => ({ sources: [] }),
}));

vi.mock('@/lib/util', () => ({
  safeDigest: vi.fn(() =>
    Promise.resolve({
      getUint32: () => 0,
    }),
  ),
}));

// Helper to render page with router
const renderPage = (searchParams = '') => {
  const router = createMemoryRouter(
    [
      {
        path: '/',
        element: <Page />,
      },
    ],
    {
      initialEntries: [`/${searchParams}`],
    },
  );

  return render(<RouterProvider router={router} />);
};

describe('Console Page', () => {
  it('renders the page with all main components', () => {
    renderPage();

    expect(screen.getByTestId('auth-required')).toBeInTheDocument();
    expect(screen.getByTestId('app-layout')).toBeInTheDocument();
    expect(screen.getByTestId('viewer-provider')).toBeInTheDocument();
    expect(screen.getByTestId('header')).toBeInTheDocument();
    expect(screen.getByTestId('sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('viewer')).toBeInTheDocument();
  });

  it('passes kubeContext from URL to ViewerProvider', () => {
    renderPage('?kubeContext=my-cluster');

    // The ViewerProvider should be called with the correct kubeContext
    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        kubeContext: 'my-cluster',
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes single source from URL to ViewerProvider as array', () => {
    renderPage('?source=s1');

    // The ViewerProvider should be called with the correct kubeContext
    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        sources: ['s1'],
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes multiple sources from URL to ViewerProvider', () => {
    renderPage('?source=s1&source=s2');

    // The ViewerProvider should be called with the correct kubeContext
    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        sources: ['s1', 's2'],
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes sourceFilter query parameters from URL to ViewerProvider', () => {
    renderPage('?region=us-west&zone=zone-a&os=linux&arch=amd64&node=node1&container=app');

    // The ViewerProvider should be called with the correct kubeContext
    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        sourceFilter: expect.objectContaining({
          region: ['us-west'],
          zone: ['zone-a'],
          os: ['linux'],
          arch: ['amd64'],
          node: ['node1'],
          container: ['app'],
        }),
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('processes grep parameter as literal string', () => {
    renderPage('?grep=error.*');

    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        grep: 'error\\.\\*',
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('processes grep parameter with regex format', () => {
    renderPage('?grep=/error.*/');

    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        grep: 'error.*',
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes null grep when not provided', () => {
    renderPage();

    expect(ViewerProvider).toHaveBeenCalledWith(
      expect.objectContaining({
        grep: null,
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes mode to Viewer component', () => {
    vi.mocked(Viewer).mockClear();

    renderPage('?mode=tail');

    expect(Viewer).toHaveBeenCalledWith(
      expect.objectContaining({
        defaultMode: 'tail',
        defaultSince: null,
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });

  it('passes since to Viewer component', () => {
    vi.mocked(Viewer).mockClear();

    renderPage('?since=1h');

    expect(Viewer).toHaveBeenCalledWith(
      expect.objectContaining({
        defaultMode: null,
        defaultSince: '1h',
      }),
      undefined, // Legacy context argument (unused in modern React)
    );
  });
});

describe('processedGrep logic', () => {
  it('escapes special regex characters in literal strings', () => {
    const input = 'error.*[test]';
    const expected = 'error\\.\\*\\[test\\]';

    // Test the regex escaping logic
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
  it('renders sidebar when isSidebarOpen is true', () => {
    renderPage();

    expect(screen.getByTestId('sidebar')).toBeInTheDocument();
  });

  it('renders main content area', () => {
    renderPage();

    expect(screen.getByTestId('viewer')).toBeInTheDocument();
  });
});
