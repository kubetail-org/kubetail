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

// Border + shadow supplier for the frameless *transparent* Linux window; macOS and
// Windows are passthroughs (the OS draws their chrome).
// See docs/adr/2026-08-09-per-platform-window-chrome.md
//
// Traps: the gutter (`inset-4`) must be at least as wide as the shadow's reach —
// the OS window rectangle clips anything beyond it into a hard band. `contain:
// paint` makes this the containing block for the app's `position: fixed` chrome
// without a GPU-layer promotion (a `transform` would disturb subpixel text).
// While maximized there's no off-window space, so collapse to full-bleed `inset-0`
// (and `WindowResizeHandles` hides its grips to match).
import type { ReactNode } from 'react';
import { useEffect, useState } from 'react';

import { isLinux } from '@/lib/platform';
import { WindowFrameContext } from '@/lib/window-frame';
import { useWindowMaximized } from '@/lib/window-maximized';

export function WindowFrame({ children }: { children?: ReactNode }) {
  const maximized = useWindowMaximized();
  // Handed down, so a popup portalled outside it can keep within its edge.
  const [frame, setFrame] = useState<HTMLDivElement | null>(null);

  // Mirror maximized onto <html> so chrome portaled outside the frame (the dialog
  // scrim) can match its geometry from CSS. Only meaningful on frameless Linux —
  // the scrim rules key off `html.frameless`.
  useEffect(() => {
    document.documentElement.classList.toggle('frame-maximized', maximized);
  }, [maximized]);

  if (!isLinux()) return <>{children}</>;
  const className = maximized
    ? 'fixed inset-0 overflow-hidden bg-background contain-[paint]'
    : 'fixed inset-4 overflow-hidden rounded-lg border border-black/5 bg-background shadow-[0_6px_16px_rgba(0,0,0,0.35)] contain-[paint] dark:border-white/5';
  return (
    <div ref={setFrame} data-testid="window-frame" className={className}>
      <WindowFrameContext value={frame}>{children}</WindowFrameContext>
    </div>
  );
}
