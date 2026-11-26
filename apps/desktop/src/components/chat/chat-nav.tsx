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

// The recent chats. Rename edits in place and delete asks first; the chat's title bar
// offers both too, through the same `ChatRenameBox` and `ChatDeleteDialog`.
//
// A chat belongs to one cluster, so the list is the window's cluster's: switching
// cluster switches the rows.
//
// The mode decides two things at once, which is why it is one prop: which chats are
// listed, and where a row points. In chat mode a chat is a route; on the dashboard it
// is the panel's search param. No row is ever marked active on the dashboard — the
// panel shows the list or the open chat, never both.
import { buttonVariants } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { cn } from '@kubetail/ui/lib/utils';
import { Link } from '@tanstack/react-router';
import { MessageCircle, MoreHorizontal, SquarePen } from 'lucide-react';
import { useLayoutEffect, useRef, useState } from 'react';
import type { RefCallback, RefObject } from 'react';

import { SectionHeading } from '@/components/primitives/section-heading';
import { SearchBox } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import { useActiveCluster } from '@/lib/active-cluster';
import type { AppMode } from '@/lib/app-mode';
import { chatModeOf, chatTitleOf, useChats } from '@/lib/chats';
import type { Chat } from '@/lib/chats';
import { SIDEBAR_CONTROL_CLASS } from '@/lib/control-style';
import { useLastSettled } from '@/lib/graphql/use-last-settled';
import { visibleTitle } from '@/lib/visible-text';

import { ChatDeleteDialog } from './chat-delete-dialog';
import { ChatRenameBox } from './chat-rename-box';

// The highlight sits on the row, not the link, so it runs under the actions button.
// Hover is half the active row's tint, and leaves the active row alone.
const ROW =
  'group/row flex items-center rounded-md text-muted-foreground transition-colors ' +
  'hover:text-foreground not-has-data-[status=active]:hover:bg-sidebar-accent/50 ' +
  'has-data-[status=active]:bg-sidebar-accent has-data-[status=active]:text-foreground';
const ROW_LINK = 'flex min-w-0 flex-1 items-center gap-1 py-1.5 pr-1 pl-2 text-left text-sm';
const NOTE = 'px-2 py-1 text-xs text-muted-foreground';

// Fills the sidebar below New chat, lifted above center so it reads under the button.
function NoChats() {
  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 pb-26 text-center">
      <div className="mb-3 flex size-10 items-center justify-center rounded-lg bg-muted">
        <MessageCircle className="size-5 text-muted-foreground" aria-hidden />
      </div>
      <p className="text-sm font-medium text-sidebar-foreground">No chats yet</p>
      <p className="max-w-50 text-xs text-muted-foreground">
        Ask about your cluster with New chat. Your chats will show up here.
      </p>
    </div>
  );
}

// A row's title, and a dot while a request in the chat waits on the user: an
// agent can wait in a chat the user is not looking at.
function RowLabel({ chat }: { chat: Chat }) {
  return (
    <>
      <span className="truncate">
        <VisibleText text={chatTitleOf(chat.title)} />
      </span>
      {chat.awaitingApproval && (
        <span role="img" aria-label="Waiting on you" className="ml-auto size-2 shrink-0 rounded-full bg-primary" />
      )}
    </>
  );
}

type ChatRowProps = {
  chat: Chat;
  onDashboard: boolean;
  actionsRef: RefCallback<HTMLButtonElement>;
  onRename: () => void;
  onDelete: () => void;
};

// A chat's row: its link, and its actions behind a ⋯.
function ChatRow({ chat, onDashboard, actionsRef, onRename, onDelete }: ChatRowProps) {
  return (
    <div className={ROW}>
      {onDashboard ? (
        <Link to="/dashboard" search={(prev) => ({ ...prev, chat: chat.id })} className={ROW_LINK} data-row-link>
          <RowLabel chat={chat} />
        </Link>
      ) : (
        <Link to="/chat/$chatId" params={{ chatId: chat.id }} className={ROW_LINK} data-row-link>
          <RowLabel chat={chat} />
        </Link>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger
          ref={actionsRef}
          aria-label={`Actions for ${visibleTitle(chatTitleOf(chat.title))}`}
          className="mr-1 rounded p-1 text-muted-foreground opacity-0 outline-none hover:bg-foreground/10 group-hover/row:opacity-100 group-has-data-[status=active]/row:opacity-100 focus-visible:opacity-100"
        >
          <MoreHorizontal className="size-4" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" sideOffset={4}>
          <DropdownMenuItem onClick={onRename}>Rename</DropdownMenuItem>
          <DropdownMenuItem variant="destructive" onClick={onDelete}>
            Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

type ChatNavProps = {
  mode: AppMode;
  // Where focus goes when the list leaves nothing to hold it: the dashboard's composer.
  focusFallback?: RefObject<HTMLElement | null>;
};

export function ChatNav({ mode, focusFallback }: ChatNavProps) {
  const { chats: all, phase } = useChats();
  const { clusterID, phase: clusterPhase } = useActiveCluster();
  const [renaming, setRenaming] = useState<string | null>(null);
  // An id, not the chat: the confirm names the chat as the watch has it now.
  const [confirming, setConfirming] = useState<string | null>(null);
  const [query, setQuery] = useState('');
  // Enter or Escape takes the focused rename box away, so focus goes to its row's actions
  // button, where the drawer's trap would otherwise lose it; a blur leaves it wherever
  // the user moved it.
  const refocus = useRef<string | null>(null);
  // The list position of a row, or the search box, that left holding focus, read once the
  // list has redrawn.
  const orphanedFocus = useRef<number | null>(null);
  const navRef = useRef<HTMLElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const newChatRef = useRef<HTMLAnchorElement>(null);

  const onDashboard = mode === 'dashboard';
  // Both watches: the rows are filtered by the window's cluster, so a chats snapshot
  // that lands first has every row filtered out until the clusters watch names one.
  // The spinner gates on the unfiltered phases; the empty state then covers an empty
  // cluster and no cluster alike. `useChats` holds its rows through a reconnect, and the
  // cluster is held through one of the clusters watch, so an open rename or confirm rides
  // either out; a settled answer naming another cluster re-filters at once.
  const connecting = phase === 'connecting' || clusterPhase === 'connecting';
  const scope = useLastSettled(clusterID, clusterPhase !== 'connecting');
  const chats = all.filter((chat) => chat.mode === chatModeOf(mode) && chat.clusterID === scope);
  const needle = query.trim().toLowerCase();
  // Chat mode's list has a search box over Recents; the heading then sits its 8px below
  // it, as New chat sits above it.
  const searchShown = !onDashboard && chats.length > 0;
  // A title matches as stored or as the row spells it, so text copied off a row finds it.
  const matches = (title: string) =>
    title.toLowerCase().includes(needle) || visibleTitle(chatTitleOf(title)).toLowerCase().includes(needle);
  const shown = needle ? chats.filter((chat) => matches(chat.title)) : chats;
  // A rename ends once its row leaves the list, as a rename elsewhere can make it: held, its
  // autofocused box would come back with the row and take focus.
  if (renaming !== null && !shown.some((chat) => chat.id === renaming)) setRenaming(null);
  // A chat deleted elsewhere, or now out of the list, takes its confirm with it.
  const confirmingChat = chats.find((chat) => chat.id === confirming) ?? null;
  if (confirming !== null && !confirmingChat) setConfirming(null);

  // A row that leaves the list holding focus (a rename or delete, here or in another window,
  // or a search no longer matching) drops it on the body, where the drawer's trap starts
  // over. Focus goes to the row now in its place, the last row, the search box, New chat,
  // or the caller's fallback.
  useLayoutEffect(() => {
    const index = orphanedFocus.current;
    orphanedFocus.current = null;
    if (index === null || (document.activeElement && document.activeElement !== document.body)) return;
    const links = navRef.current!.querySelectorAll<HTMLElement>('[data-row-link]');
    const target =
      links[Math.min(index, links.length - 1)] ?? searchRef.current ?? newChatRef.current ?? focusFallback?.current;
    target?.focus();
  });

  return (
    <nav ref={navRef} aria-label="Chats" className="flex flex-1 flex-col gap-1">
      {/* Writes nothing: the first send from `/chat` creates the chat, so a stray
          click leaves no empty row behind. The panel has no such link — typing in its
          composer is how a dashboard chat starts. `cn` merges away the button base's
          `border-transparent`, which would otherwise win over the outline's color. */}
      {!onDashboard && (
        <Link
          ref={newChatRef}
          to="/chat"
          className={cn(buttonVariants({ variant: 'outline' }), 'mb-2 w-full', SIDEBAR_CONTROL_CLASS)}
        >
          <SquarePen aria-hidden />
          New chat
        </Link>
      )}

      {/* Sticky, so the rows it filters scroll under it. */}
      {searchShown && (
        <div
          className="sticky top-0 z-10 bg-sidebar pb-2"
          // The last chat going takes the search box with it.
          ref={(search) => () => {
            if (search?.contains(document.activeElement)) orphanedFocus.current = 0;
          }}
        >
          <SearchBox
            ref={searchRef}
            aria-label="Search chats"
            placeholder="Search"
            containerClassName="max-w-none"
            className={SIDEBAR_CONTROL_CLASS}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onClear={() => setQuery('')}
          />
        </div>
      )}

      {connecting && chats.length === 0 && <Spinner size="sm" className="mt-2 self-center" />}

      {!connecting && chats.length === 0 && (onDashboard ? <p className={NOTE}>No chats yet.</p> : <NoChats />)}

      {chats.length > 0 &&
        (shown.length > 0 ? (
          <SectionHeading label="Recents" className={searchShown ? 'mt-0' : undefined} />
        ) : (
          <p className={NOTE}>No chats match.</p>
        ))}

      {shown.map((chat, index) => (
        // One element per chat, holding its row or its rename box. The ref's cleanup runs
        // before React removes it, while it still holds focus.
        <div
          key={chat.id}
          ref={(item) => () => {
            if (item?.contains(document.activeElement)) orphanedFocus.current = index;
          }}
        >
          {renaming === chat.id ? (
            <ChatRenameBox
              chat={chat}
              onDone={(how) => {
                refocus.current = how === 'keyboard' ? chat.id : null;
                setRenaming(null);
              }}
            />
          ) : (
            <ChatRow
              chat={chat}
              onDashboard={onDashboard}
              actionsRef={(trigger) => {
                if (trigger && refocus.current === chat.id) {
                  refocus.current = null;
                  trigger.focus();
                }
              }}
              onRename={() => setRenaming(chat.id)}
              onDelete={() => setConfirming(chat.id)}
            />
          )}
        </div>
      ))}

      <ChatDeleteDialog chat={confirmingChat} onClose={() => setConfirming(null)} />
    </nav>
  );
}
