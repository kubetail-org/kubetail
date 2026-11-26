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

// The composer's "Allowed for this chat" list: the chat's own rules, each in
// the words Settings uses, then the folders granted for the chat, each with
// Remove, and under a folder no run takes the reason. It draws nothing while
// the chat holds neither. A rule's line is the sidecar's, drawn through
// VisibleText, since its context and namespace are cluster text; so is a
// folder's refusal, and its path, with its whitespace kept and its trailing
// whitespace spelled, as Settings draws it.
import { Button } from '@kubetail/ui/elements/button';
import { ShieldCheck } from 'lucide-react';
import { type ReactNode, useState } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import { useChatGrants } from '@/lib/chat-grants';
import { useSandboxFolders } from '@/lib/sandbox-folders';

export function ChatGrants({ chatID }: { chatID: string }) {
  const { rules, removing, error, remove } = useChatGrants(chatID);
  const { chat: folders, revoking, revokeError, revoke } = useSandboxFolders(chatID);
  const [open, setOpen] = useState(false);
  const count = (rules?.length ?? 0) + (folders?.length ?? 0);
  if (count === 0) return null;
  const failure = error ?? revokeError;

  return (
    <div className="relative shrink-0">
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="rounded-full text-xs font-normal"
        aria-expanded={open}
        onClick={() => setOpen((was) => !was)}
      >
        <ShieldCheck aria-hidden />
        {count} allowed
      </Button>
      {open && (
        <div className="absolute bottom-full left-0 z-10 mb-1 w-80 rounded-md border bg-popover p-2 text-xs shadow-md">
          <p className="mb-1 text-muted-foreground">Allowed for this chat</p>
          <ul aria-label="Allowed for this chat" className="flex flex-col gap-1">
            {rules?.map((rule) => (
              <Grant key={rule.id} busy={removing === rule.id} onRemove={() => void remove(rule.id)}>
                <VisibleText text={rule.line} />
              </Grant>
            ))}
            {folders?.map(({ id, path, write, refusal }) => (
              <Grant key={id} busy={revoking.has(id)} onRemove={() => void revoke(id, 'Chat')}>
                {write ? 'Reads and writes of ' : 'Reads of '}
                <span className="break-all whitespace-pre-wrap">
                  <VisibleText text={path} trailing="end" />
                </span>
                {refusal && (
                  <span className="block text-muted-foreground">
                    <VisibleText text={refusal} />
                  </span>
                )}
              </Grant>
            ))}
          </ul>
          {failure && (
            <p role="alert" className="mt-1 text-destructive">
              {failure}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function Grant({ busy, onRemove, children }: { busy: boolean; onRemove: () => void; children: ReactNode }) {
  return (
    <li className="flex items-start justify-between gap-2">
      <span className="min-w-0 wrap-break-word">{children}</span>
      <Button type="button" variant="outline" size="xs" disabled={busy} onClick={onRemove}>
        Remove
      </Button>
    </li>
  );
}
