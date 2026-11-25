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

// Keeps Tab inside an overlay while it is open, and hands focus back to whatever
// opened it. A non-modal panel that covers the page is otherwise a trap of the
// other kind: focus walks out behind it, where the user cannot see what they are
// on. Pair it with `aria-modal` so assistive tech ignores the page too.
import { useEffect, useRef } from 'react';

// The tabbable surface of a panel. Disabled controls and `tabindex="-1"` are out;
// nothing else in an overlay of ours is focusable but hidden, so visibility is not
// filtered — `offsetParent` and `getClientRects` both read empty under jsdom, which
// would make any such filter untestable.
const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

export function useFocusTrap<T extends HTMLElement>(active: boolean) {
  const ref = useRef<T>(null);

  useEffect(() => {
    const node = ref.current;
    if (!active || !node) return undefined;

    // Whoever had focus when the overlay opened — the toggle, usually.
    const opener = document.activeElement as HTMLElement | null;
    const tabbable = () => Array.from(node.querySelectorAll<HTMLElement>(FOCUSABLE));

    tabbable()[0]?.focus();

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Tab') return;
      // An overlay opened over this panel — a dialog portalled to `body` — marks
      // everything outside itself aria-hidden, this node included. Its own trap owns
      // Tab now, and pulling focus back would take it out of what the user is in.
      if (node.closest('[aria-hidden="true"]')) return;
      const items = tabbable();
      if (items.length === 0) {
        e.preventDefault();
        return;
      }
      const first = items[0];
      const last = items.at(-1)!;
      const current = document.activeElement;
      // Focus outside (a click on the page behind, or the browser's own start of
      // the cycle) comes back to the panel rather than continuing through it.
      if (!node.contains(current)) {
        e.preventDefault();
        first.focus();
      } else if (e.shiftKey && current === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && current === last) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener('keydown', onKeyDown, true);
    return () => {
      document.removeEventListener('keydown', onKeyDown, true);
      opener?.focus();
    };
  }, [active]);

  return ref;
}
