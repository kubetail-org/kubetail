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

import { SidebarProvider, useSidebar } from '@kubetail/ui/elements/sidebar';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { mockMatchMedia } from '@/test-utils';

import { LeftSidebarToggle } from './left-sidebar-toggle';

function OpenState() {
  const { open } = useSidebar();
  return <span data-testid="open">{String(open)}</span>;
}

describe('LeftSidebarToggle', () => {
  it('flips the sidebar the library’s provider holds', () => {
    render(
      <SidebarProvider>
        <LeftSidebarToggle />
        <OpenState />
      </SidebarProvider>,
    );
    expect(screen.getByTestId('open')).toHaveTextContent('true');
    fireEvent.click(screen.getByRole('button', { name: 'Toggle sidebar' }));
    expect(screen.getByTestId('open')).toHaveTextContent('false');
  });

  // Focus stays on the button after a press, so it says what the press did.
  it('says whether the sidebar is open', () => {
    render(
      <SidebarProvider>
        <LeftSidebarToggle />
      </SidebarProvider>,
    );
    const toggle = screen.getByRole('button', { name: 'Toggle sidebar' });
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  // Narrow, the sidebar leaves the row and the toggle opens a drawer instead.
  it('says whether the drawer is open in a narrow window', () => {
    const fireChange = mockMatchMedia();
    Object.defineProperty(window, 'innerWidth', { value: 600, configurable: true });
    render(
      <SidebarProvider>
        <LeftSidebarToggle />
      </SidebarProvider>,
    );
    act(() => fireChange(true));
    const toggle = screen.getByRole('button', { name: 'Toggle sidebar' });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    Object.defineProperty(window, 'innerWidth', { value: 1024, configurable: true });
  });

  // The library's `ghost` would paint the open sidebar's toggle as pressed.
  it('keeps its resting look and hover while expanded', () => {
    render(
      <SidebarProvider>
        <LeftSidebarToggle />
      </SidebarProvider>,
    );
    const toggle = screen.getByRole('button', { name: 'Toggle sidebar' });
    expect(toggle).toHaveClass(
      'aria-expanded:bg-transparent',
      'aria-expanded:text-muted-foreground',
      'aria-expanded:hover:bg-app-bar-hover',
      'aria-expanded:hover:text-foreground',
    );
    expect(toggle).not.toHaveClass('aria-expanded:bg-muted', 'aria-expanded:text-foreground');
  });

  it('is a 28px bar button with a muted icon', () => {
    render(
      <SidebarProvider>
        <LeftSidebarToggle />
      </SidebarProvider>,
    );
    expect(screen.getByRole('button', { name: 'Toggle sidebar' })).toHaveClass('size-7', 'text-muted-foreground');
  });
});
