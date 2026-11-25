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

import { Popover } from '@base-ui/react/popover';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { dismissPopupsOnWindowDrag, startsWindowDrag } from './window-drag';

// The `composedPath()` of a press on the element with `testId`.
function pressed(testId: string): EventTarget[] {
  const path: EventTarget[] = [];
  for (let el: Element | null = screen.getByTestId(testId); el; el = el.parentElement) path.push(el);
  return path.concat(document, window);
}

describe('startsWindowDrag', () => {
  it('drags from anywhere under a "deep" region', () => {
    render(
      <div data-tauri-drag-region="deep">
        <div>
          <span data-testid="nested" />
        </div>
      </div>,
    );
    expect(startsWindowDrag(pressed('nested'))).toBe(true);
  });

  it('drags from a bare or "true" region only when it is pressed itself', () => {
    render(
      <>
        <div data-testid="bare" data-tauri-drag-region>
          <span data-testid="under-bare" />
        </div>
        <div data-testid="true" data-tauri-drag-region="true">
          <span data-testid="under-true" />
        </div>
      </>,
    );
    expect(startsWindowDrag(pressed('bare'))).toBe(true);
    expect(startsWindowDrag(pressed('true'))).toBe(true);
    expect(startsWindowDrag(pressed('under-bare'))).toBe(false);
    expect(startsWindowDrag(pressed('under-true'))).toBe(false);
  });

  it('is blocked by a clickable element, unless it is a region itself', () => {
    render(
      <div data-tauri-drag-region="deep">
        <button type="button" data-testid="button">
          <span data-testid="label">Go</span>
        </button>
        <div data-testid="role" role="menuitem" aria-label="Item" tabIndex={-1} />
        {/* Tauri does not list `textbox`, so only the tab stop makes this clickable. */}
        <div data-testid="tab-stop" role="textbox" aria-label="Box" tabIndex={0} />
        <div data-testid="editable" contentEditable suppressContentEditableWarning />
        <a href="#x" data-testid="link-region" data-tauri-drag-region>
          x
        </a>
        <div data-testid="no-tab-stop" tabIndex={-1} />
        <div data-testid="not-editable" contentEditable={false} />
      </div>,
    );
    expect(startsWindowDrag(pressed('button'))).toBe(false);
    expect(startsWindowDrag(pressed('label'))).toBe(false);
    expect(startsWindowDrag(pressed('role'))).toBe(false);
    expect(startsWindowDrag(pressed('tab-stop'))).toBe(false);
    expect(startsWindowDrag(pressed('editable'))).toBe(false);
    expect(startsWindowDrag(pressed('link-region'))).toBe(true);
    expect(startsWindowDrag(pressed('no-tab-stop'))).toBe(true);
    expect(startsWindowDrag(pressed('not-editable'))).toBe(true);
  });

  it('is blocked by "false", and walks past a value Tauri does not read', () => {
    render(
      <div data-tauri-drag-region="deep">
        <div data-tauri-drag-region="false">
          <span data-testid="opted-out" />
        </div>
        <div data-tauri-drag-region="other">
          <span data-testid="under-other" />
        </div>
      </div>,
    );
    expect(startsWindowDrag(pressed('opted-out'))).toBe(false);
    expect(startsWindowDrag(pressed('under-other'))).toBe(true);
  });

  it('does not drag outside every region', () => {
    render(<span data-testid="plain" />);
    expect(startsWindowDrag(pressed('plain'))).toBe(false);
    expect(startsWindowDrag([document, window])).toBe(false);
  });
});

function Bar() {
  return (
    <div data-testid="bar" data-tauri-drag-region="deep" onPointerDown={dismissPopupsOnWindowDrag}>
      <Popover.Root>
        <Popover.Trigger>Open</Popover.Trigger>
        <Popover.Portal>
          <Popover.Positioner>
            <Popover.Popup>Popup body</Popover.Popup>
          </Popover.Positioner>
        </Popover.Portal>
      </Popover.Root>
    </div>
  );
}

describe('dismissPopupsOnWindowDrag', () => {
  it('closes an open popover on a press that drags the window', async () => {
    const user = userEvent.setup();
    render(<Bar />);
    await user.click(screen.getByRole('button', { name: 'Open' }));
    await screen.findByText('Popup body');

    // The OS takes the release, so the press is all the webview sees.
    fireEvent.pointerDown(screen.getByTestId('bar'), { button: 0 });
    await waitFor(() => expect(screen.queryByText('Popup body')).not.toBeInTheDocument());
  });

  it('clicks nothing on a press that does not drag', () => {
    render(<Bar />);
    const bar = screen.getByTestId('bar');
    const onClick = vi.fn();
    bar.addEventListener('click', onClick);

    // The button's release clicks it; a second click would toggle it back.
    fireEvent.pointerDown(screen.getByRole('button', { name: 'Open' }), { button: 0 });
    // Only the primary button drags.
    fireEvent.pointerDown(bar, { button: 2 });
    expect(onClick).not.toHaveBeenCalled();
  });
});
