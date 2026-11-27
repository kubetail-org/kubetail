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

// The Sandbox pane's status card: whether new chats start in the sandbox on
// every cluster without a value of its own, its switch, and the way into the
// console; or, on a machine with none, why. Turning it off is said in a
// dialog first; turning it on is not.
import { Button } from '@kubetail/ui/elements/button';
import { Switch } from '@kubetail/ui/elements/switch';
import { cn } from '@kubetail/ui/lib/utils';
import { Container, type LucideIcon, RotateCw, TriangleAlert } from 'lucide-react';
import { type ReactNode, useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { VisibleText } from '@/components/primitives/visible-text';
import { isLinux, isMacOS } from '@/lib/platform';
import type { Sandbox } from '@/lib/sandbox';

type SandboxCardProps = {
  sandbox: Sandbox;
  /** Whether new chats on every cluster start in the sandbox. Undefined until its watch answers. */
  on: boolean | undefined;
  /** Without one the switch is drawn disabled. */
  onSwitch?: (on: boolean) => void;
  onTry: () => void;
};

export function SandboxCard({ sandbox, on, onSwitch, onTry }: SandboxCardProps) {
  const [confirming, setConfirming] = useState(false);
  const { available, reason, failed, retry } = sandbox;

  if (failed) {
    return (
      <Button variant="outline" size="sm" className="self-start" onClick={retry}>
        <RotateCw aria-hidden />
        Retry
      </Button>
    );
  }
  if (available === undefined) return null;
  if (!available) {
    return (
      <div className="rounded-lg border">
        <Summary icon={TriangleAlert} iconClassName="text-destructive" title="No sandbox on this computer">
          <p>
            Every command asks before it runs, and folder grants don&apos;t apply.
            {isLinux() && ' Install bubblewrap, then restart Kubetail.'}
          </p>
          <p className="font-mono break-all whitespace-pre-wrap" data-testid="reason">
            <VisibleText text={reason} trailing="end" />
          </p>
        </Summary>
      </div>
    );
  }

  if (on === undefined) return null;

  const mechanism = isMacOS() ? "macOS's built-in sandbox (Seatbelt)" : 'bubblewrap (bwrap)';
  return (
    <div className="divide-y rounded-lg border">
      <Summary
        icon={Container}
        title={on ? 'Sandbox is on for new chats' : 'Sandbox is off for new chats'}
        action={
          <Switch
            aria-label="Sandbox for new chats"
            checked={on}
            disabled={!onSwitch}
            onCheckedChange={(next) => (next ? onSwitch?.(true) : setConfirming(true))}
          />
        }
      >
        <p>
          {on
            ? `New chats run their commands in ${mechanism}. They reach only the folders below and the internet when you allow it. Requests to your clusters and cloud platforms are proxied to prevent access to your authentication tokens.`
            : 'New chats run their commands as you, with your files, credentials and network, each asking first.'}{' '}
          Chats that exist keep their own switch, and a cluster can have its own under New chats.
        </p>
      </Summary>
      <div className="flex items-center gap-3 px-4 py-3">
        <p className="flex-1 text-xs text-muted-foreground">Run a command in the sandbox to see what it can reach.</p>
        <Button variant="outline" size="sm" onClick={onTry}>
          Try a command
        </Button>
      </div>
      <Dialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Start new chats outside the sandbox?"
        description={
          'Every command in a chat started from now on, on a cluster without its own setting, will run as you, with your files, your credentials and the network, after you approve it. ' +
          'Chats that exist keep their own switch.'
        }
      >
        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            onClick={() => {
              setConfirming(false);
              onSwitch?.(false);
            }}
          >
            Start outside the sandbox
          </Button>
        </div>
      </Dialog>
    </div>
  );
}

function Summary({
  icon: Icon,
  iconClassName,
  title,
  action,
  children,
}: {
  icon: LucideIcon;
  iconClassName?: string;
  title: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className="flex items-center gap-3 p-4">
      <div className="flex size-8 shrink-0 items-center justify-center self-start rounded-md bg-muted">
        <Icon className={cn('size-4', iconClassName)} aria-hidden />
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="text-sm text-popover-foreground">{title}</span>
        <div className="text-xs text-muted-foreground">{children}</div>
      </div>
      {action}
    </div>
  );
}
