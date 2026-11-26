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
import { beforeEach, describe, expect, it } from 'vitest';

import { RightSidebarProvider, useRightSidebar } from '@/lib/right-sidebar';
import { mockMatchMedia } from '@/test-utils';

import { RightSidebarToggle } from './right-sidebar-toggle';

function OpenState() {
  const { open } = useRightSidebar();
  const { openMobile, setOpenMobile } = useSidebar();
  return (
    <>
      <span data-testid="open">{String(open)}</span>
      <span data-testid="drawer">{String(openMobile)}</span>
      <button type="button" onClick={() => setOpenMobile(true)}>
        Open drawer
      </button>
    </>
  );
}

// The left sidebar's state: its drawer, in a narrow window, covers the right sidebar.
const renderToggle = () =>
  render(
    <SidebarProvider>
      <RightSidebarProvider mode="chat">
        <RightSidebarToggle />
        <OpenState />
      </RightSidebarProvider>
    </SidebarProvider>,
  );

// The right sidebar's open state persists; each test starts from the default, closed.
beforeEach(() => {
  localStorage.clear();
});

describe('RightSidebarToggle', () => {
  it('opens and closes the right sidebar, saying which it is', () => {
    renderToggle();
    const toggle = screen.getByRole('button', { name: 'Toggle right sidebar' });
    expect(toggle).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(toggle);
    expect(screen.getByTestId('open')).toHaveTextContent('true');
    expect(toggle).toHaveAttribute('aria-pressed', 'true');
  });

  // The drawer is modal and would hide what the press changed.
  it('closes the left sidebar’s drawer first in a narrow window', () => {
    const fireChange = mockMatchMedia();
    Object.defineProperty(window, 'innerWidth', { value: 600, configurable: true });
    renderToggle();
    act(() => fireChange(true));
    fireEvent.click(screen.getByRole('button', { name: 'Open drawer' }));
    expect(screen.getByTestId('drawer')).toHaveTextContent('true');

    fireEvent.click(screen.getByRole('button', { name: 'Toggle right sidebar' }));
    expect(screen.getByTestId('drawer')).toHaveTextContent('false');
    expect(screen.getByTestId('open')).toHaveTextContent('true');
    Object.defineProperty(window, 'innerWidth', { value: 1024, configurable: true });
  });

  it('is a 28px bar button with a muted icon', () => {
    renderToggle();
    expect(screen.getByRole('button', { name: 'Toggle right sidebar' })).toHaveClass('size-7', 'text-muted-foreground');
  });
});
