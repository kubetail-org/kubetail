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

// The toggle that runs one message's turn outside the sandbox, its agents'
// commands included. The next message runs where the chat does. Turning it
// on is said in a dialog before it is done; turning it off needs no warning.
// The toggle is the outbox entry's (`sandboxDisabledThisTurn`).
import { Button } from '@kubetail/ui/elements/button';
import { ShieldCheck, ShieldOff } from 'lucide-react';
import { useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';

type MessageSandboxSwitchProps = {
  on: boolean;
  disabled?: boolean;
  onSwitch: (on: boolean) => void;
};

export function MessageSandboxSwitch({ on, disabled = false, onSwitch }: MessageSandboxSwitchProps) {
  const [confirming, setConfirming] = useState(false);

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
        disabled={disabled}
        onClick={() => (on ? onSwitch(false) : setConfirming(true))}
      >
        {on ? <ShieldCheck aria-hidden /> : <ShieldOff aria-hidden />}
        {on ? 'Keep this message in the sandbox' : 'Run this message outside the sandbox'}
      </Button>
      <Dialog
        open={confirming}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Run this message's commands outside the sandbox?"
        description={
          "Every command this message's answer runs, its agents' included, will run as you, with your files, your credentials and the network, after you approve it. " +
          'The next message runs in the sandbox again.'
        }
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={confirm}>
            Run outside for this message
          </Button>
        </div>
      </Dialog>
    </>
  );
}
