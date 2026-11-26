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

// A chat's title as a text box, in place of wherever it was drawn: the chat list's row or
// the chat's title bar. Enter renames, or with a title the sidecar would refuse keeps the
// box and says why; Escape or leaving the box cancels; both keys are left to an IME while
// it composes. The title comes off `chatCollectionWatch`, so a refused rename leaves it as it was
// with nothing local to undo.
import { cn } from '@kubetail/ui/lib/utils';
import { useId, useState } from 'react';
import { useMutation } from 'urql';

import { PROSE_TEXT } from '@/components/primitives/text-entry';
import { graphql } from '@/gql';
import { chatTitleHasLineBreak, chatTitleTooLong, MAX_CHAT_TITLE_LENGTH, normalizeChatTitle } from '@/lib/chats';
import type { Chat } from '@/lib/chats';
import { isImeKey } from '@/lib/ime';

const ChatRenameMutation = graphql(`
  mutation ChatRename($id: ChatID!, $title: String!) {
    chatRename(id: $id, title: $title) {
      id
      title
      updatedAt
    }
  }
`);

// Why the sidecar would refuse the title, or null.
function problemOf(title: string): string | null {
  if (chatTitleTooLong(title)) return `A chat title can be at most ${MAX_CHAT_TITLE_LENGTH} characters.`;
  if (chatTitleHasLineBreak(title)) return 'A chat title must be one line.';
  return null;
}

const scrollIntoView = (el: HTMLElement | null) => el?.scrollIntoView({ block: 'nearest' });

type ChatRenameBoxProps = {
  chat: Pick<Chat, 'id' | 'title'>;
  // Told how the box closed: by Enter or Escape, or by focus leaving it.
  onDone: (how: 'keyboard' | 'blur') => void;
  className?: string;
};

export function ChatRenameBox({ chat, onDone, className }: ChatRenameBoxProps) {
  // The title as editing began.
  const [original] = useState(chat.title);
  const [title, setTitle] = useState(original);
  // Set by an Enter on a title the sidecar would refuse. From then on the box is marked,
  // and says why, whenever the draft would be refused.
  const [problemShown, setProblemShown] = useState(false);
  const problem = problemShown ? problemOf(title) : null;
  const errorId = useId();
  const [, chatRename] = useMutation(ChatRenameMutation);

  const commit = () => {
    // Untouched, the draft writes nothing: another window may have renamed the chat since.
    if (title === original) {
      onDone('keyboard');
      return;
    }
    if (problemOf(title)) {
      setProblemShown(true);
      return;
    }
    const next = normalizeChatTitle(title);
    // An empty draft cancels. One the chat already has writes nothing, since a rename moves
    // the chat up Recents. One that only respaced the original writes nothing once another
    // window has renamed the chat, which would lose that rename; until then it can trim
    // what a derived title's cut left at its end.
    const respaced = next === normalizeChatTitle(original) && chat.title !== original;
    if (next && next !== chat.title && !respaced) {
      void chatRename({ id: chat.id, title: next });
    }
    onDone('keyboard');
  };

  return (
    // `relative` anchors the message under the box.
    <div className="relative w-full">
      <input
        // oxlint-disable-next-line jsx-a11y/no-autofocus -- the title became this input on the user's click
        autoFocus
        {...PROSE_TEXT}
        aria-label="Chat title"
        aria-invalid={!!problem || undefined}
        aria-describedby={problem ? errorId : undefined}
        value={title}
        className={cn(
          'w-full rounded-md border bg-background px-2 py-1 text-sm outline-none focus-visible:border-ring aria-invalid:border-destructive',
          className,
        )}
        onChange={(e) => setTitle(e.target.value)}
        onBlur={() => onDone('blur')}
        onKeyDown={(e) => {
          if (isImeKey(e.nativeEvent)) return;
          if (e.key === 'Enter') {
            // Focus may move during this keydown; unprevented, the Enter would press whatever takes it.
            e.preventDefault();
            commit();
          }
          if (e.key === 'Escape') {
            // The rename takes this Escape whole: the drawer around the list would close on it too.
            e.stopPropagation();
            onDone('keyboard');
          }
        }}
      />
      {/* Over whatever is under the box, so the row and the title bar keep their height; as
          wide as the box, wrapping in the narrow sidebar. Under the list's last row it would
          sit past the scroller's end, so it scrolls itself into view. */}
      {problem && (
        <p
          ref={scrollIntoView}
          id={errorId}
          role="alert"
          className="absolute inset-x-0 top-full z-10 mt-1 rounded-md border bg-popover px-2 py-1 text-xs text-destructive shadow-md"
        >
          {problem}
        </p>
      )}
    </div>
  );
}
