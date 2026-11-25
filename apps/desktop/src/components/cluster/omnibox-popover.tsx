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

// A popover hung from the omnibox rather than from the control that opens it, so it
// centres on the omnibox; each popup is at least as wide. The library's `PopoverContent`
// anchors on the trigger alone, so the positioner is base-ui's own. It keeps 8px inside the
// window frame's visible edge, which on Linux is inset from the viewport's: a popup caps
// its width at the `--available-width` that leaves.
import { Popover as PopoverPrimitive } from '@base-ui/react/popover';
import { Popover, PopoverTrigger } from '@kubetail/ui/elements/popover';
import type { ReactElement, RefObject } from 'react';

import { useWindowFrame } from '@/lib/window-frame';

export function OmniboxPopover({
  anchorRef,
  children,
  popup,
  open,
  onOpenChange,
}: {
  anchorRef: RefObject<HTMLElement | null>;
  children: ReactElement;
  popup: ReactElement;
  // Leave both out for a popover that opens and closes on its own.
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
}) {
  const frame = useWindowFrame();
  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger render={children} />
      <PopoverPrimitive.Portal>
        {/* z-50, as the library's popover content has: above the sidebar drawer
            and its scrim. It never flips above the omnibox: its height is capped
            at the room below, which would otherwise trip the flip. */}
        <PopoverPrimitive.Positioner
          anchor={anchorRef}
          align="center"
          side="bottom"
          sideOffset={8}
          collisionAvoidance={{ side: 'none' }}
          collisionBoundary={frame ?? 'clipping-ancestors'}
          collisionPadding={8}
          className="z-50"
        >
          {popup}
        </PopoverPrimitive.Positioner>
      </PopoverPrimitive.Portal>
    </Popover>
  );
}
