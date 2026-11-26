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

// The chat's switch between its sandboxed commands reaching the internet and not.
// Turning it on is said in a dialog before it is done; turning it off needs no
// warning. What the chat runs with is the row's, so the button draws the list
// watch's value and never its own. The mutation is `useNetworkSwitch`'s
// (`@/lib/network-switch`).
import { Button } from '@kubetail/ui/elements/button';
import { Globe, GlobeOff } from 'lucide-react';
import { useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';

type NetworkSwitchProps = {
  /** The chat's switch. Undefined until the list watch delivers the chat. */
  networkEnabled: boolean | undefined;
  /** Whether the machine can give a sandboxed command the internet. Undefined until the sidecar says. */
  available: boolean | undefined;
  switching: boolean;
  onSwitch: (enabled: boolean) => void;
};

export function NetworkSwitch({ networkEnabled, available, switching, onSwitch }: NetworkSwitchProps) {
  const [confirming, setConfirming] = useState(false);

  const onClick = () => {
    if (networkEnabled) onSwitch(false);
    else setConfirming(true);
  };

  const confirm = () => {
    setConfirming(false);
    onSwitch(true);
  };

  // Off is always allowed, so a switch left on where network is gone can still go.
  const disabled = networkEnabled === undefined || switching || (!networkEnabled && available !== true);

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        className="shrink-0 rounded-full text-xs font-normal"
        disabled={disabled}
        onClick={onClick}
      >
        {networkEnabled ? <Globe aria-hidden /> : <GlobeOff aria-hidden />}
        {networkEnabled ? 'Network on' : 'No network'}
      </Button>
      <Dialog
        open={confirming}
        onOpenChange={(open) => !open && setConfirming(false)}
        title="Give this chat's commands the internet?"
        description={
          'Commands in this chat can reach any server on the internet, and send it what they read: ' +
          'cluster data and your workspace. They still hold no credential and cannot read your private files.'
        }
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button type="button" onClick={confirm}>
            Turn network on
          </Button>
        </div>
      </Dialog>
    </>
  );
}
