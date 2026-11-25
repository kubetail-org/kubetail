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

// The hamburger standing in for the menu bar on the frameless platforms. `AppBar`
// mounts it only off macOS, which keeps its native global menu (`src-tauri/app_menu.rs`).
// Owns the app-wide actions and their shortcuts, since with no native menu nothing
// else registers them.
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { invoke } from '@tauri-apps/api/core';
import { Menu } from 'lucide-react';
import { useEffect } from 'react';

/** Host commands the menu drives. Keep in sync with `commands.rs`. */
const NEW_WINDOW_CMD = 'new_window';
const QUIT_CMD = 'quit';

function newWindow() {
  invoke(NEW_WINDOW_CMD).catch(() => {});
}

function quit() {
  invoke(QUIT_CMD).catch(() => {});
}

export function AppMenu() {
  // No native menu here, so this menu owns the accelerators. Window-scoped,
  // matching the macOS menu's CmdOrCtrl semantics.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if (!e.ctrlKey && !e.metaKey) return;
      const key = e.key.toLowerCase();
      if (key === 'n') {
        e.preventDefault();
        newWindow();
      } else if (key === 'q') {
        e.preventDefault();
        quit();
      }
    }
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label="Application menu"
        className="flex h-full items-center rounded px-2 outline-none hover:bg-app-bar-hover focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Menu className="size-4 " aria-hidden />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" sideOffset={4} className="min-w-48">
        <DropdownMenuItem onClick={newWindow}>
          New Window
          <DropdownMenuShortcut>Ctrl+N</DropdownMenuShortcut>
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={quit}>
          Quit
          <DropdownMenuShortcut>Ctrl+Q</DropdownMenuShortcut>
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
