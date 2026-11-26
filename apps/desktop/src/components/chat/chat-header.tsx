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

// The bar at the top of chat mode's main panel: the open chat's title, opening Rename and
// Delete, the same two the chat list's row offers.
import { buttonVariants } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { cn } from '@kubetail/ui/lib/utils';
import { ChevronDown } from 'lucide-react';
import { useRef, useState } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import { chatTitleOf } from '@/lib/chats';
import type { Chat } from '@/lib/chats';

import { ChatDeleteDialog } from './chat-delete-dialog';
import { ChatRenameBox } from './chat-rename-box';

function ChatTitleMenu({ chat }: { chat: Pick<Chat, 'id' | 'title'> }) {
  const [renaming, setRenaming] = useState(false);
  const [confirming, setConfirming] = useState(false);
  // Enter or Escape takes the focused box away, so focus goes back to the title; a blur
  // leaves it wherever the user moved it.
  const refocus = useRef(false);
  const focusIfReturning = (trigger: HTMLButtonElement | null) => {
    if (trigger && refocus.current) trigger.focus();
    refocus.current = false;
  };

  if (renaming) {
    return (
      <ChatRenameBox
        chat={chat}
        onDone={(how) => {
          refocus.current = how === 'keyboard';
          setRenaming(false);
        }}
        className="h-7 max-w-100"
      />
    );
  }
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          ref={focusIfReturning}
          className={cn(
            buttonVariants({ variant: 'ghost', size: 'sm' }),
            // `shrink` undoes the button's `shrink-0`, so a long title ellipsizes and keeps the chevron.
            '-ml-2 min-w-0 shrink gap-1 truncate px-2 text-sm font-medium text-foreground',
          )}
        >
          <span className="truncate">
            <VisibleText text={chatTitleOf(chat.title)} />
          </span>
          <ChevronDown aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" sideOffset={4}>
          <DropdownMenuItem onClick={() => setRenaming(true)}>Rename</DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={() => setConfirming(true)}>
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ChatDeleteDialog chat={confirming ? chat : null} onClose={() => setConfirming(false)} />
    </>
  );
}

export function ChatHeader({ chat }: { chat: Pick<Chat, 'id' | 'title'> }) {
  return (
    <div
      data-testid="chat-header"
      className="flex h-11 shrink-0 items-center justify-between border-b border-border pr-4 pl-6"
    >
      <ChatTitleMenu key={chat.id} chat={chat} />
    </div>
  );
}
