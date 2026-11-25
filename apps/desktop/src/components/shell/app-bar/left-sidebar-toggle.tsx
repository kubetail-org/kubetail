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

// Opens and closes the left sidebar. Drives the same `@kubetail/ui` state as the built-in
// `Cmd/Ctrl+B`, so both stay in sync; while the window is narrow, it opens the drawer, and
// says whether that is open.
import { useSidebar } from '@kubetail/ui/elements/sidebar';
import { PanelLeft } from 'lucide-react';

import { AppBarButton } from './button';

// `ghost` paints an expanded button as a popup's open trigger, over the bar's hover wash. The
// sidebar is open most of the time, so the toggle keeps its resting look and its hover.
const EXPANDED_AT_REST =
  'aria-expanded:bg-transparent aria-expanded:text-muted-foreground aria-expanded:hover:bg-app-bar-hover aria-expanded:hover:text-foreground';

export function LeftSidebarToggle() {
  const { open, isMobile, openMobile, toggleSidebar } = useSidebar();
  return (
    <AppBarButton
      aria-label="Toggle sidebar"
      aria-expanded={isMobile ? openMobile : open}
      className={EXPANDED_AT_REST}
      onClick={toggleSidebar}
    >
      <PanelLeft aria-hidden />
    </AppBarButton>
  );
}
