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

// The Settings dialog's Monitoring tab: what monitoring watches, whether the OS
// shows its notifications, and a switch per target. A switch draws the watch's
// `enabled`, so a press moves it only once the target's `Modified` arrives, and a
// change in another window moves it too. Rows that share a label each add their
// cluster's kube context.
import { Switch } from '@kubetail/ui/elements/switch';

import { MonitorTargetStatus } from '@/components/monitoring/target-status';
import { VisibleText } from '@/components/primitives/visible-text';
import { useClusters } from '@/lib/clusters';
import {
  type MonitorTarget,
  useMonitorSignals,
  useMonitorTargetEnabledSet,
  useMonitorTargets,
  useNotificationPermission,
} from '@/lib/incidents';
import { visibleTitle } from '@/lib/visible-text';

import { Intro } from './group';

const list = new Intl.ListFormat('en', { type: 'conjunction' });

export function MonitoringSettings() {
  const targets = useMonitorTargets();
  const signals = useMonitorSignals();
  const { changing, errors, set } = useMonitorTargetEnabledSet();
  const { clusters } = useClusters();
  // Undefined until the host answers, and then the line is not drawn.
  const permission = useNotificationPermission();
  // Nothing before the Bookmarks: a half-listed set must not read as the whole.
  if (targets === null) return null;

  const labels = [...signals.values()].map((s) => s.label);
  const watched = labels.length > 0 ? `: ${list.format(labels)}` : ' failures';
  const labelCounts = new Map<string, number>();
  for (const t of targets) labelCounts.set(t.label, (labelCounts.get(t.label) ?? 0) + 1);
  const contextOf = (target: MonitorTarget) =>
    clusters?.find((c) => c.id === target.clusterID)?.spec.source.kubeconfig?.context;
  return (
    <Intro
      lede={`While Kubetail is running, the tray included, it watches each enabled target for${watched}. A quit app watches nothing.`}
    >
      {permission === 'Denied' && (
        <p role="status" className="text-xs text-destructive">
          Notifications are off for Kubetail, so a new incident raises none. Turn them on in System Settings ›
          Notifications › Kubetail.
        </p>
      )}
      {targets.length > 0 ? (
        <ul className="divide-y rounded-lg border">
          {targets.map((target) => (
            <TargetRow
              key={target.id}
              target={target}
              context={labelCounts.get(target.label)! > 1 ? contextOf(target) : undefined}
              busy={changing.has(target.id)}
              error={errors.get(target.id)}
              onSwitch={(enabled) => void set(target.id, enabled)}
            />
          ))}
        </ul>
      ) : (
        <p className="text-sm text-muted-foreground">No targets to watch.</p>
      )}
    </Intro>
  );
}

function TargetRow({
  target,
  context,
  busy,
  error,
  onSwitch,
}: {
  target: MonitorTarget;
  context: string | undefined;
  busy: boolean;
  error: string | undefined;
  onSwitch: (enabled: boolean) => void;
}) {
  return (
    <li className="flex items-center gap-4 px-4 py-3">
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="truncate text-sm text-popover-foreground">
          <VisibleText text={target.label} />
        </span>
        {context && (
          <span className="truncate text-xs text-muted-foreground">
            <VisibleText text={context} />
          </span>
        )}
        {/* Off is the switch's to say; the status line's own off line would link back here. */}
        {target.enabled && <MonitorTargetStatus target={target} />}
        {error && (
          <p role="alert" className="text-xs text-destructive">
            <VisibleText text={error} />
          </p>
        )}
      </div>
      <Switch
        aria-label={visibleTitle(context ? `${target.label} (${context})` : target.label)}
        checked={target.enabled}
        disabled={busy}
        onCheckedChange={onSwitch}
      />
    </li>
  );
}
