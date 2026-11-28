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

// A target's state, as one line above its incidents and never in place of them. The
// target's label is its own text, drawn through VisibleText.
import { Button } from '@kubetail/ui/elements/button';

import { VisibleText } from '@/components/primitives/visible-text';
import { useDialog } from '@/lib/dialog';
import { READ_ERROR_TEXT, targetStatus, useMonitorSignals } from '@/lib/incidents';
import type { MonitorTarget } from '@/lib/incidents';

const LINE = 'text-xs text-muted-foreground';

// `onOpenSettings` runs before the link opens Settings: a popover holding the line closes.
export function MonitorTargetStatus({
  target,
  onOpenSettings,
}: {
  target: MonitorTarget;
  onOpenSettings?: () => void;
}) {
  const { openDialog } = useDialog();
  const signals = useMonitorSignals();
  const status = targetStatus(target);
  if (!status) return null;

  const label = <VisibleText text={target.label} />;
  switch (status.kind) {
    case 'off':
      return (
        <p className={LINE}>
          Monitoring is off for {label}.{' '}
          <Button
            variant="link"
            size="sm"
            className="h-auto p-0 text-xs"
            onClick={() => {
              onOpenSettings?.();
              openDialog('settings', 'Monitoring');
            }}
          >
            Monitoring settings
          </Button>
        </p>
      );
    case 'failed':
      return (
        <p className={LINE}>
          {label} cannot be read: {status.reasons.map((reason) => READ_ERROR_TEXT[reason]).join(', ')}.
        </p>
      );
    case 'never-read':
      return <p className={LINE}>{label} has not been read yet.</p>;
    case 'partial':
      return (
        <div className={LINE}>
          <p>Some signals on {label} cannot be judged:</p>
          <ul className="mt-0.5 list-disc pl-4">
            {status.unknownSignals.map(({ name, reason }) => (
              <li key={name}>{`${signals.get(name)?.label ?? name}: ${READ_ERROR_TEXT[reason]}`}</li>
            ))}
          </ul>
        </div>
      );
  }
}
