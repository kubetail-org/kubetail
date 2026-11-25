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
import { useState } from 'react';
import { describe, expect, it } from 'vitest';

import { useFocusTrap } from './focus-trap';

// Helpers -------------------------------------------------------------

// A page with something focusable outside the panel, so escaping the trap is
// observable rather than theoretical.
function Harness({ startOpen = false }: { startOpen?: boolean }) {
  const [open, setOpen] = useState(startOpen);
  const ref = useFocusTrap<HTMLDivElement>(open);
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>
        open
      </button>
      <button type="button">page</button>
      {open && (
        <div ref={ref} role="dialog" aria-modal aria-label="Panel">
          <button type="button">first</button>
          <button type="button" onClick={() => setOpen(false)}>
            last
          </button>
        </div>
      )}
    </div>
  );
}

const button = (name: string) => screen.getByRole('button', { name });

const tab = (shiftKey = false) => fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Tab', shiftKey });

// Tests ---------------------------------------------------------------

describe('useFocusTrap', () => {
  it('moves focus into the panel when it opens', () => {
    render(<Harness />);
    fireEvent.click(button('open'));
    expect(document.activeElement).toBe(button('first'));
  });

  it('wraps Tab at the end of the panel', () => {
    render(<Harness startOpen />);
    button('last').focus();

    tab();
    expect(document.activeElement).toBe(button('first'));
  });

  it('wraps Shift+Tab at the start of the panel', () => {
    render(<Harness startOpen />);
    button('first').focus();

    tab(true);
    expect(document.activeElement).toBe(button('last'));
  });

  it('leaves Tab alone in the middle of the panel', () => {
    render(<Harness startOpen />);
    button('first').focus();

    // Not handled here: the browser's own order does the work.
    tab();
    expect(document.activeElement).toBe(button('first'));
  });

  it('pulls focus back from the page behind it', () => {
    render(<Harness startOpen />);
    button('page').focus();

    tab();
    expect(document.activeElement).toBe(button('first'));
  });

  it('hands focus back to whatever opened it', () => {
    render(<Harness />);
    const opener = button('open');
    opener.focus();
    fireEvent.click(opener);
    expect(document.activeElement).toBe(button('first'));

    // Closed from inside: focus must not be left on the panel that just went.
    fireEvent.click(button('last'));
    expect(document.activeElement).toBe(opener);
  });

  it('does nothing while inactive', () => {
    render(<Harness />);
    button('page').focus();

    tab();
    expect(document.activeElement).toBe(button('page'));
  });

  // An overlay opening over the panel (a dialog portalled to `body`) marks
  // everything outside itself `aria-hidden` — including the panel. Pulling focus
  // back then would take it out of the dialog the user is actually in.
  it('stands aside once an overlay has hidden the panel it holds', () => {
    render(<Harness startOpen />);
    screen.getByRole('dialog', { name: 'Panel' }).setAttribute('aria-hidden', 'true');
    button('page').focus();

    tab();
    expect(document.activeElement).toBe(button('page'));
  });

  // Nothing in the panel takes focus, so Tab stays where it is rather than
  // walking out onto the page behind.
  it('holds Tab in a panel with nothing to focus', () => {
    function Empty() {
      const ref = useFocusTrap<HTMLDivElement>(true);
      return (
        <div>
          <button type="button">page</button>
          <div ref={ref} role="dialog" aria-modal aria-label="Panel">
            nothing here
          </div>
        </div>
      );
    }
    render(<Empty />);
    button('page').focus();

    expect(fireEvent.keyDown(document.activeElement!, { key: 'Tab' })).toBe(false);
  });
});
