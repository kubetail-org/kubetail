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

// The composer's sandbox button. It names where the chat's commands run, as the
// list watch has it, and opens a popup that will hold the chat's network options.
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { ShieldCheck, ShieldOff } from 'lucide-react';

type SandboxPopoverProps = {
  /** The chat's switch: false for a chat that has not started, undefined until the list watch delivers it. */
  sandboxDisabled: boolean | undefined;
};

export function SandboxPopover({ sandboxDisabled }: SandboxPopoverProps) {
  const outside = sandboxDisabled === true;
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
        {outside ? 'Outside the sandbox' : 'Sandbox'}
      </PopoverTrigger>
      <PopoverContent align="start" sideOffset={8} className="w-72">
        <p className="text-sm font-medium">Sandbox</p>
        <p className="text-sm text-muted-foreground">Network options are coming soon.</p>
      </PopoverContent>
    </Popover>
  );
}
