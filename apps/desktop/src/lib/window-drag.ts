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

import type { PointerEvent } from 'react';

// `isClickable` and `startsWindowDrag` mirror the hit test in Tauri's injected
// drag script (`crates/tauri/src/window/scripts/drag.js`, 2.12.5). Diff them
// against it on a Tauri upgrade.

const DRAG_REGION_ATTR = 'data-tauri-drag-region';
const CLICKABLE_TAGS = new Set(['A', 'BUTTON', 'INPUT', 'SELECT', 'TEXTAREA', 'LABEL', 'SUMMARY']);
const INTERACTIVE_ROLES = new Set(['button', 'link', 'menuitem', 'tab', 'checkbox', 'radio', 'switch', 'option']);

function isClickable(el: HTMLElement): boolean {
  return (
    CLICKABLE_TAGS.has(el.tagName) ||
    (el.hasAttribute('contenteditable') && el.getAttribute('contenteditable') !== 'false') ||
    (el.hasAttribute('tabindex') && el.getAttribute('tabindex') !== '-1') ||
    INTERACTIVE_ROLES.has(el.getAttribute('role') ?? '')
  );
}

// The values Tauri reads; any other value is walked past as if absent.
const REGION_VALUES = new Set(['', 'true', 'deep', 'false']);

// Whether `node` settles the hit test: it carries a value Tauri reads, or it is
// clickable and carries none.
function settlesDrag(node: EventTarget): node is HTMLElement {
  if (!(node instanceof HTMLElement)) return false;
  const attr = node.getAttribute(DRAG_REGION_ATTR);
  return attr === null ? isClickable(node) : REGION_VALUES.has(attr);
}

/**
 * True when Tauri starts a window drag for a press whose `composedPath()` is
 * `path`. The nearest settling element decides: `"deep"` drags, a bare or
 * `"true"` region drags only when pressed itself, and `"false"` or a clickable
 * element blocks the drag.
 */
export function startsWindowDrag(path: EventTarget[]): boolean {
  const el = path.find(settlesDrag);
  const attr = el?.getAttribute(DRAG_REGION_ATTR);
  if (attr === 'deep') return true;
  if (attr === '' || attr === 'true') return el === path[0];
  return false;
}

/**
 * `onPointerDown` for a drag region. Tauri hands a drag press to the OS, so the
 * webview never sees its `click`, and Base UI's popovers dismiss on an outside
 * click. Clicking the pressed element here closes them; a click with no press
 * behind it is one Base UI accepts as outside.
 */
export function dismissPopupsOnWindowDrag(event: PointerEvent) {
  if (event.button !== 0) return;
  if (!startsWindowDrag(event.nativeEvent.composedPath())) return;
  event.target.dispatchEvent(new MouseEvent('click', { bubbles: true }));
}
