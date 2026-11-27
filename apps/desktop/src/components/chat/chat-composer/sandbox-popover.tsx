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

// The composer's sandbox button. It names where the next message's commands
// run: the open chat's switch as the list watch has it, or the default a new
// chat starts with, unless the message's toggle takes it outside. Its popup
// holds the open chat's switch and the toggle, each of which confirms leaving
// the sandbox in its own dialog.
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { ShieldCheck, ShieldOff } from 'lucide-react';

import { MessageSandboxSwitch } from './message-sandbox-switch';
import { SandboxSwitch } from './sandbox-switch';

type SandboxPopoverProps = {
  /**
   * The open chat's switch, or the default a chat that has not started is
   * created with. Undefined until its watch delivers it.
   */
  sandboxDisabled: boolean | undefined;
  /** The open chat's switch; absent for a chat that has not started. */
  chat?: { switching: boolean; onSwitch: (disabled: boolean) => void };
  /** The next message's toggle. */
  thisMessage: boolean;
  onThisMessage: (on: boolean) => void;
  /**
   * What is drawn is a held send's, which Retry resends as it is, so nothing
   * here changes it until Retry or Discard.
   */
  held?: boolean;
};

export function SandboxPopover({
  sandboxDisabled,
  chat,
  thisMessage,
  onThisMessage,
  held = false,
}: SandboxPopoverProps) {
  const outside = sandboxDisabled === true || thisMessage;
  let label = 'Sandbox';
  if (sandboxDisabled === true) label = 'Outside the sandbox';
  else if (thisMessage) label = 'Outside for this message';
  // A held create keeps the default it left with, whatever cluster the window shows by now.
  let newChat = sandboxDisabled
    ? 'New chats on this cluster start outside the sandbox. Settings › Sandbox changes it.'
    : 'New chats on this cluster start in the sandbox. Settings › Sandbox changes it.';
  if (held) {
    newChat = sandboxDisabled
      ? 'This message starts its chat outside the sandbox.'
      : 'This message starts its chat in the sandbox.';
  }
  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="shrink-0 gap-1.5 px-2.5 text-sm font-normal text-muted-foreground"
          />
        }
        disabled={sandboxDisabled === undefined}
      >
        {outside ? (
          <ShieldOff className="size-4" aria-hidden />
        ) : (
          <ShieldCheck className="size-4 text-emerald-500" aria-hidden />
        )}
        {label}
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="flex w-80 flex-col items-start gap-2">
        <p className="text-sm font-medium">Sandbox</p>
        {chat ? (
          <>
            <p className="text-xs text-muted-foreground">Where this chat&apos;s commands run.</p>
            <SandboxSwitch
              sandboxDisabled={sandboxDisabled}
              switching={chat.switching || held}
              onSwitch={chat.onSwitch}
            />
          </>
        ) : (
          <p className="text-xs text-muted-foreground">{newChat}</p>
        )}
        {sandboxDisabled === false && (
          <MessageSandboxSwitch on={thisMessage} disabled={held} onSwitch={onThisMessage} />
        )}
      </PopoverContent>
    </Popover>
  );
}
