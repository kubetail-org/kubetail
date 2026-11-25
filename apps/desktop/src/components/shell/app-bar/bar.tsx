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

// The app bar: the layout's top band and the window's title bar on every platform.
// The drag strip is common; only the ends differ, and this is the one place that
// decides which. macOS reserves its native traffic lights and keeps the OS menu bar
// and caption buttons; the frameless platforms get `AppMenu` on the left and
// `WindowControls` on the right.
// See docs/adr/2026-08-09-per-platform-window-chrome.md
import { useLayoutEffect, useRef, useState } from 'react';

import { TrafficLightGutter } from '@/components/primitives/traffic-light-gutter';
import { WindowControls } from '@/components/primitives/window-controls';
import { ContextOmnibox } from '@/components/shell/context-omnibox';
import { isMacOS } from '@/lib/platform';
import { dismissPopupsOnWindowDrag } from '@/lib/window-drag';

import { AppBarCapsule } from './button';
import { HistoryNav } from './history-nav';
import { LeftSidebarToggle } from './left-sidebar-toggle';
import { AppMenu } from './menu';
import { NotificationButton } from './notification-button';
import { RightSidebarToggle } from './right-sidebar-toggle';
import { SettingsButton } from './settings-button';

// The narrowest the omnibox's column goes before the omnibox leaves the centre line: the
// pill with an empty name and icon-only tabs. Its 152px are the pill's 8px of padding, the
// chip's 48px (its 6px of padding, the status button, a 2px gap and the trigger's 12px of
// padding), the 12px gap and the tab track's 84px. Under it the shorter end gives up its
// spare space.
const OMNIBOX_MIN_WIDTH = '9.5rem';

// An end's column: at least its own content, and at least the other end's while that
// leaves the omnibox `OMNIBOX_MIN_WIDTH`, so equal ends centre the omnibox when there is
// room for it.
function endColumn(own: number, other: number) {
  return `minmax(max(${own}px, min(${other}px, 100% - ${other}px - ${OMNIBOX_MIN_WIDTH})), 1fr)`;
}

export function AppBar() {
  // `AppMenu` registers the `Ctrl/Cmd+N`/`Q` accelerators while it is mounted, so
  // this branch is what keeps them off macOS, where the native menu owns them.
  const mac = isMacOS();
  // Each end's content width; each end sits at it to be measured.
  const startRef = useRef<HTMLDivElement>(null);
  const endRef = useRef<HTMLDivElement>(null);
  const [ends, setEnds] = useState({ start: 0, end: 0 });
  useLayoutEffect(() => {
    const measure = () => setEnds({ start: startRef.current!.offsetWidth, end: endRef.current!.offsetWidth });
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(startRef.current!);
    observer.observe(endRef.current!);
    return () => observer.disconnect();
  }, []);
  const columns = `${endColumn(ends.start, ends.end)} minmax(0, 420px) ${endColumn(ends.end, ends.start)}`;
  return (
    // 56px tall; `window_manager.rs` mirrors the number to center the traffic lights
    // in it. No padding of its own: the close button has to reach the window's
    // corner, and the gutter supplies the macOS inset.
    //
    // Three columns: the two ends share what the omnibox leaves equally, so the omnibox
    // sits on the window's center line, and neither goes under the wider end's content,
    // so the omnibox's column is what gives way in a narrow window, down to
    // `OMNIBOX_MIN_WIDTH`; past that the omnibox moves off the line rather than shrink.
    //
    // `deep` makes every pixel of the bar a drag handle, spacing and margins
    // included, rather than only the elements that carry the attribute. Tauri's
    // hit test walks up from what was pressed and stops at the first clickable
    // element, so the buttons in here still click; the omnibox opts out.
    // A press that drags never ends in a click, so `dismissPopupsOnWindowDrag`
    // clicks for it, closing any open popup.
    //
    // `transform-gpu` pins the band to a retained compositor layer — without it
    // WebKitGTK re-rasters during an interactive resize and the transparent Linux
    // window flashes through to the desktop. It costs subpixel text rendering
    // (`window-frame.tsx`), acceptable only while the bar carries no body text.
    <div
      data-testid="app-bar"
      data-tauri-drag-region="deep"
      onPointerDown={dismissPopupsOnWindowDrag}
      className="grid h-14 shrink-0 transform-gpu bg-app-bar"
      style={{ gridTemplateColumns: columns }}
    >
      <div ref={startRef} data-testid="app-bar-start" className="flex items-stretch gap-2 justify-self-start">
        {mac ? <TrafficLightGutter /> : <AppMenu />}
        <div data-testid="app-bar-controls" className="flex items-center gap-2">
          <LeftSidebarToggle />
          <HistoryNav />
        </div>
      </div>
      <div className="flex min-w-0 items-center">
        <ContextOmnibox />
      </div>
      <div ref={endRef} data-testid="app-bar-end" className="flex items-stretch justify-self-end">
        {/* Accounts are not part of the MVP, so `AccountAvatar` is left out. */}
        {/* The panel toggle stands apart from the capsule; its inset mirrors the traffic lights'. */}
        <div data-testid="app-bar-actions" className="flex items-center gap-3 pr-3.5">
          <AppBarCapsule data-testid="app-bar-global">
            <NotificationButton />
            <SettingsButton />
          </AppBarCapsule>
          <RightSidebarToggle />
        </div>
        {!mac && <WindowControls />}
      </div>
    </div>
  );
}
