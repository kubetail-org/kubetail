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

import { fireEvent, render, renderHook, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it } from 'vitest';

import type { AppMode } from '@/lib/app-mode';

import { RightSidebarProvider, useRightSidebar } from './right-sidebar';

// Helpers -------------------------------------------------------------

// The layout names the mode, so a test drives it the same way: by re-rendering the
// provider under a different one.
function Probe() {
  const { mode, open, toggle, show, close } = useRightSidebar();
  return (
    <div>
      <span data-testid="state">{`${mode}:${open}`}</span>
      <button type="button" onClick={toggle}>
        toggle
      </button>
      <button type="button" onClick={show}>
        show
      </button>
      <button type="button" onClick={close}>
        close
      </button>
    </div>
  );
}

const harness = (mode: AppMode) => (
  <RightSidebarProvider mode={mode}>
    <Probe />
  </RightSidebarProvider>
);

const state = () => screen.getByTestId('state').textContent;
const click = (name: string) => fireEvent.click(screen.getByRole('button', { name }));

// The state is persisted app-wide, so each case starts from a pristine store.
beforeEach(() => {
  localStorage.clear();
});

// Tests ---------------------------------------------------------------

describe('useRightSidebar', () => {
  it('opens closed until it has been opened once, so a first window shows the page alone', () => {
    render(harness('chat'));
    expect(state()).toBe('chat:false');
  });

  it('holds the state across a mount, so a new window opens as the last was left', () => {
    const { unmount } = render(harness('dashboard'));
    click('toggle');
    unmount();

    render(harness('dashboard'));
    expect(state()).toBe('dashboard:true');
  });

  it('toggles both ways', () => {
    render(harness('chat'));
    click('toggle');
    expect(state()).toBe('chat:true');

    click('toggle');
    expect(state()).toBe('chat:false');
  });

  it('shows whatever it was', () => {
    render(harness('chat'));
    click('show');
    expect(state()).toBe('chat:true');

    click('show');
    expect(state()).toBe('chat:true');
  });

  it('closes whatever it was', () => {
    render(harness('chat'));
    click('close');
    expect(state()).toBe('chat:false');

    click('toggle');
    click('close');
    expect(state()).toBe('chat:false');
  });

  it('keeps each mode’s state to itself', () => {
    const { rerender } = render(harness('chat'));
    click('toggle');
    expect(state()).toBe('chat:true');

    // Opening the chat's panel says nothing about wanting the dashboard's...
    rerender(harness('dashboard'));
    expect(state()).toBe('dashboard:false');

    // ...and coming back finds the chat's as it was left.
    rerender(harness('chat'));
    expect(state()).toBe('chat:true');
  });

  it('closes only the mode it is answering for', () => {
    const { rerender } = render(harness('chat'));
    click('toggle');

    rerender(harness('dashboard'));
    click('toggle');
    click('close');
    expect(state()).toBe('dashboard:false');

    rerender(harness('chat'));
    expect(state()).toBe('chat:true');
  });

  it('refuses to work outside a provider, which would silently drop the toggle', () => {
    expect(() => renderHook(() => useRightSidebar())).toThrow(/within a RightSidebarProvider/);
  });
});
