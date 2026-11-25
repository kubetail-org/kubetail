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

// The chat's switch between running its commands in the sandbox and outside it.
// Leaving the sandbox is said in a dialog before it is done; coming back needs no
// warning. What the chat runs is the row's, so the button draws the list watch's
// value and never its own. The mutation is `useSandboxSwitch`'s (`@/lib/sandbox-switch`).
import { Button } from '@kubetail/ui/elements/button';
import { ShieldCheck, ShieldOff } from 'lucide-react';
import { useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';

type SandboxSwitchProps = {
  /** The chat's switch. Undefined until the list watch delivers the chat. */
  sandboxDisabled: boolean | undefined;
  switching: boolean;
  onSwitch: (disabled: boolean) => void;
};

export function SandboxSwitch({ sandboxDisabled, switching, onSwitch }: SandboxSwitchProps) {
  const [confirming, setConfirming] = useState(false);

  const onClick = () => {
    if (sandboxDisabled) onSwitch(false);
    else setConfirming(true);
  };

  const confirm = () => {
    setConfirming(false);
    onSwitch(true);
  };

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="shrink-0 rounded-full text-xs font-normal"
        disabled={sandboxDisabled === undefined || switching}
        onClick={onClick}
      >
        {sandboxDisabled ? <ShieldOff aria-hidden /> : <ShieldCheck aria-hidden />}
        {sandboxDisabled ? 'Outside the sandbox' : 'Sandboxed'}
      </Button>
      <Dialog
        open={confirming}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Run this chat's commands outside the sandbox?"
        description={
          'Every command will run as you, with your files, your credentials and the network, after you approve it. ' +
          "Kubetail's sandbox will not confine it. You can switch back at any time; what already started keeps running as it started."
        }
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={confirm}>
            Run outside the sandbox
          </Button>
        </div>
      </Dialog>
    </>
  );
}
