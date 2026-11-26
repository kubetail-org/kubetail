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

// Asks before deleting a chat, since deletion is instant, permanent, and disappears from
// every window at once. Open while `chat` is set. The chat goes when `chatCollectionWatch` says so,
// so a refused delete leaves it where it was.
import { Button } from '@kubetail/ui/elements/button';
import { useState } from 'react';
import { useMutation } from 'urql';

import { Dialog, DialogDescription } from '@/components/primitives/dialog';
import { VisibleText } from '@/components/primitives/visible-text';
import { graphql } from '@/gql';
import { chatTitleOf } from '@/lib/chats';
import type { Chat } from '@/lib/chats';

const ChatDeleteMutation = graphql(`
  mutation ChatDelete($id: ChatID!) {
    chatDelete(id: $id)
  }
`);

type ChatDeleteDialogProps = {
  chat: Pick<Chat, 'id' | 'title'> | null;
  onClose: () => void;
};

export function ChatDeleteDialog({ chat, onClose }: ChatDeleteDialogProps) {
  const [, chatDelete] = useMutation(ChatDeleteMutation);
  // The chat last asked about: the dialog fades out after `chat` clears, still naming it.
  const [named, setNamed] = useState(chat);
  if (chat && chat !== named) setNamed(chat);

  const remove = () => {
    /* v8 ignore else -- the dialog that calls this opens only with a chat to confirm */
    if (chat) void chatDelete({ id: chat.id });
    onClose();
  };

  return (
    <Dialog open={!!chat} onOpenChange={(open) => !open && onClose()} title="Delete chat?">
      {/* The description names the chat, so it is announced with the dialog. It sits in the
          body, not the header, so on a short window it scrolls with the buttons and Tab
          brings them into view. */}
      <DialogDescription render={<div />} className="mb-4 flex flex-col gap-2">
        {/* Spelled as the title bar and the list draw it, every character shown: runs of
            spaces keep their width, and an unbroken run (an invisible character spelled out
            grows tenfold) wraps. */}
        <p className="rounded-md bg-muted px-3 py-2 text-foreground wrap-anywhere whitespace-pre-wrap">
          <VisibleText text={chatTitleOf(named?.title ?? '')} />
        </p>
        <p>This chat and its messages go from every window. This cannot be undone.</p>
      </DialogDescription>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onClose}>
          Keep chat
        </Button>
        <Button type="button" variant="destructive" onClick={remove}>
          Delete chat
        </Button>
      </div>
    </Dialog>
  );
}
