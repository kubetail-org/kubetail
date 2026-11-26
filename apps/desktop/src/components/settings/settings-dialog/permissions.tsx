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

// The Settings dialog's Permissions pane: the modes and the rules a sandboxed
// command's requests are decided by, a section per target. Drawn only on a
// machine with a sandbox, since without one every command asks and nothing
// here applies. A cluster's mode is joined onto the clusters watch by its id;
// a cloud platform's section shows only while the machine offers it. Every
// cluster name, account, context and rule line is user, cluster or cloud
// text, drawn through VisibleText.
import { Button } from '@kubetail/ui/elements/button';
import { Tabs, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';
import { useId, useState } from 'react';
import type { SubmitEvent } from 'react';

import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import type { PermissionMode, PermissionModeSource, PermissionTarget } from '@/gql/graphql';
import { CLOUD_PLATFORM_NAMES } from '@/lib/chats';
import { displayName, useClusters } from '@/lib/clusters';
import type { Cluster } from '@/lib/clusters';
import { usePermissionSettings } from '@/lib/permission-settings';
import type { AccountMode, PermissionSettings as Permissions, TargetModes } from '@/lib/permission-settings';
import { useSandbox } from '@/lib/sandbox';
import { useSandboxCloudAccess } from '@/lib/sandbox-cloud-access';

import { Field, Select } from './field';
import { Group } from './group';
import { ChatGrants, RanWithoutAsking } from './permission-activity';
import { PermissionModeInfo } from './permission-mode-info';
import type { AccountOption } from './permission-rule-form';
import { PermissionRules } from './permission-rules';

// Each mode with what it asks, in the picker's order.
const MODES: { value: PermissionMode; label: string; line: string }[] = [
  {
    value: 'ReadOnly',
    label: 'Read-only',
    line: 'Every change to the cluster is refused. Showing Secret data asks you first.',
  },
  { value: 'Ask', label: 'Ask', line: 'Every change to the cluster, and showing Secret data, asks you first.' },
  {
    value: 'Auto',
    label: 'Auto',
    line: 'Changes run and Secret data is shown without asking, except what always asks.',
  },
];

// Where a cluster's mode comes from.
const SOURCES: Record<PermissionModeSource, string> = {
  Entry: 'Set for this cluster',
  Default: 'Default mode',
};

// Each mode by name alone, for a cloud platform's pickers.
const MODE_NAMES = MODES.map(({ value, label }) => ({ value, label }));

export function PermissionSettings() {
  const { available } = useSandbox();
  if (available === false) {
    return (
      <p className="text-sm text-muted-foreground">
        This machine has no sandbox, so every command asks before it runs. Modes and rules apply only to sandboxed
        commands.
      </p>
    );
  }
  if (available !== true) return null;
  return <PermissionPane />;
}

function PermissionPane() {
  const permissions = usePermissionSettings();
  const { clusters } = useClusters();
  const { platforms } = useSandboxCloudAccess();
  const { settings, error, readError, retry } = permissions;
  if (!settings) {
    if (!readError) return null;
    return (
      <div role="alert" className="flex items-center gap-3 text-sm text-destructive">
        <span>
          Kubetail could not read the permissions: <VisibleText text={readError} />
        </span>
        <Button type="button" variant="outline" size="sm" onClick={retry}>
          Try again
        </Button>
      </div>
    );
  }
  const kube = settings.targets.find((t) => t.target === 'Kubernetes');
  // Every cloud platform the build offers has a section, switched on or not, so
  // its modes and rules can be set before a run reaches it.
  const clouds = settings.targets.filter((t) => t.target !== 'Kubernetes');
  // A mode waits for the clusters watch to deliver its cluster; the default
  // mode and the rules do not.
  const byID = new Map(clusters?.map((c) => [c.id, c]));
  const rows = (kube?.accounts ?? []).flatMap((mode) => {
    const cluster = mode.clusterID ? byID.get(mode.clusterID) : undefined;
    return cluster ? [{ cluster, mode }] : [];
  });
  // Two clusters can name one context, from two kubeconfig files.
  const contexts = [...new Set(rows.flatMap(({ cluster }) => cluster.spec.source.kubeconfig?.context ?? []))];
  const accounts: Partial<Record<PermissionTarget, AccountOption[]>> = {
    Kubernetes: rows.map(({ cluster, mode }) => ({ value: mode.account, label: clusterLabel(cluster) })),
  };
  for (const t of clouds) accounts[t.target] = t.accounts.map((a) => ({ value: a.account, label: a.name }));
  const targets = [...(kube ? ['Kubernetes' as const] : []), ...clouds.map((t) => t.target)];

  return (
    <>
      <div className="flex flex-col gap-1 text-sm">
        <p className="text-muted-foreground">
          Modes and rules decide what sandboxed commands may change. A chat switched outside the sandbox asks for every
          command.
        </p>
        {error && (
          <p role="alert" className="text-destructive">
            {error}
          </p>
        )}
      </div>

      {kube && <DefaultModePicker modes={kube} settings={settings} onSet={permissions.setDefaultMode} />}

      <ClusterModes
        rows={rows}
        onSet={(id, mode) => permissions.setMode('Kubernetes', id, mode)}
        onClear={(id) => permissions.clearMode('Kubernetes', id)}
      />

      {clouds.map((modes) => (
        <CloudModes
          key={modes.target}
          modes={modes}
          permissions={permissions}
          off={platforms !== undefined && !platforms.includes(modes.target)}
        />
      ))}

      <ShowingFilter contexts={contexts} />

      <PermissionRules
        rules={settings.rules}
        targets={targets}
        accounts={accounts}
        contexts={contexts}
        onAdd={permissions.addRule}
        onUpdate={permissions.updateRule}
        onRemove={permissions.removeRule}
      />

      <ChatGrants />
      <RanWithoutAsking />
    </>
  );
}

// The clusters' default mode and its picker.
function DefaultModePicker({
  modes,
  settings,
  onSet,
}: {
  modes: TargetModes;
  settings: NonNullable<Permissions['settings']>;
  onSet: Permissions['setDefaultMode'];
}) {
  const current = MODES.find((m) => m.value === modes.defaultMode);
  return (
    <Group
      title="Default mode"
      info={<PermissionModeInfo alwaysAsks={settings.alwaysAsks} accessControl={settings.accessControl} />}
    >
      <div className="flex items-center gap-3 rounded-lg border px-4 py-3 text-sm">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          {current && (
            <>
              <span>{current.label}</span>
              <span className="text-xs text-muted-foreground">{current.line}</span>
            </>
          )}
        </div>
        <Tabs value={modes.defaultMode} onValueChange={(mode) => onSet('Kubernetes', mode as PermissionMode)}>
          <TabsList aria-label="Default mode">
            {MODES.map(({ value, label }) => (
              <TabsTrigger key={value} value={value}>
                {label}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>
      </div>
    </Group>
  );
}

// A cloud platform's section: its default mode, then each account with a
// mode of its own, and a form that gives one more account its own.
function CloudModes({ modes, permissions, off }: { modes: TargetModes; permissions: Permissions; off: boolean }) {
  const name = CLOUD_PLATFORM_NAMES[modes.target];
  const [account, setAccount] = useState('');
  const [mode, setMode] = useState<PermissionMode>('Ask');
  const add = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (await permissions.setMode(modes.target, account.trim(), mode)) setAccount('');
  };
  return (
    <Group
      title={name}
      lede={
        off
          ? `Off: sandboxed commands do not reach ${name}. Each account follows the default mode unless you set one for it.`
          : `Each ${name} account follows the default mode unless you set one for it.`
      }
    >
      <ul className="divide-y rounded-lg border text-sm">
        <li className="flex items-center gap-3 px-4 py-3">
          <span className="flex-1">Default mode</span>
          <Select
            className="w-36"
            aria-label={`Default mode of ${name}`}
            options={MODE_NAMES}
            value={modes.defaultMode}
            onChange={(value) => permissions.setDefaultMode(modes.target, value)}
          />
        </li>
        {modes.accounts.map((a: AccountMode) => (
          <li key={a.account} aria-label={a.name} className="flex items-center gap-3 px-4 py-3">
            <span className="min-w-0 flex-1 font-mono break-all">
              <VisibleText text={a.name} />
            </span>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => permissions.clearMode(modes.target, a.account)}
            >
              Clear
            </Button>
            <Select
              className="w-36"
              aria-label={`Mode of ${a.name}`}
              options={MODE_NAMES}
              value={a.mode}
              onChange={(value) => permissions.setMode(modes.target, a.account, value)}
            />
          </li>
        ))}
        <li>
          <form
            aria-label={`Add an account to ${name}`}
            className="grid grid-cols-[1fr_auto_auto] items-end gap-3 px-4 py-3"
            onSubmit={add}
          >
            <Field label="Account">
              {(id) => <Input id={id} value={account} onChange={(e) => setAccount(e.target.value)} />}
            </Field>
            <Field label="Mode">
              {(id) => <Select id={id} className="w-36" options={MODE_NAMES} value={mode} onChange={setMode} />}
            </Field>
            <Button type="submit" size="sm" disabled={account.trim() === ''}>
              Add account
            </Button>
          </form>
        </li>
      </ul>
    </Group>
  );
}

// distinctContext is a cluster's kube-context where it differs from its name,
// else ''.
function distinctContext(cluster: Cluster): string {
  const context = cluster.spec.source.kubeconfig?.context ?? '';
  return context === displayName(cluster) ? '' : context;
}

// clusterLabel is a cluster's name, and its context where that differs, since
// two clusters can share a name.
function clusterLabel(cluster: Cluster): string {
  const context = distinctContext(cluster);
  return context ? `${displayName(cluster)} (${context})` : displayName(cluster);
}

// One row per cluster with a kube-context: its name, its mode and where the
// mode comes from, and a picker that sets the cluster's own. Clear returns it
// to the default.
function ClusterModes({
  rows,
  onSet,
  onClear,
}: {
  rows: { cluster: Cluster; mode: AccountMode }[];
  onSet: (clusterID: string, mode: PermissionMode) => void;
  onClear: (clusterID: string) => void;
}) {
  return (
    <Group title="Clusters" lede="A cluster follows the default mode unless you set one for it.">
      <ul className="divide-y rounded-lg border text-sm">
        {rows.map(({ cluster, mode }) => {
          const context = distinctContext(cluster);
          const label = clusterLabel(cluster);
          return (
            <li key={cluster.id} aria-label={label} className="flex items-center gap-3 px-4 py-3">
              <div className="flex min-w-0 flex-1 flex-col gap-0.5 break-all">
                <span className="font-mono">
                  <VisibleText text={displayName(cluster)} />
                </span>
                <span className="text-xs text-muted-foreground">
                  {context && (
                    <>
                      <span className="font-mono">
                        <VisibleText text={context} />
                      </span>
                      {' · '}
                    </>
                  )}
                  {SOURCES[mode.source]}
                </span>
              </div>
              {mode.source === 'Entry' && (
                <Button type="button" variant="ghost" size="sm" onClick={() => onClear(cluster.id)}>
                  Clear
                </Button>
              )}
              <Select
                className="w-36"
                aria-label={`Mode of ${label}`}
                options={MODES}
                value={mode.mode}
                onChange={(value) => onSet(cluster.id, value)}
              />
            </li>
          );
        })}
      </ul>
    </Group>
  );
}

// Placeholder: awaits a sidecar query for what applies to one context;
// filtering by pattern here would reimplement the engine. Held on All clusters.
function ShowingFilter({ contexts }: { contexts: string[] }) {
  const id = useId();
  return (
    <div className="flex items-center gap-2 text-sm">
      <label htmlFor={id} className="text-muted-foreground">
        Showing
      </label>
      <Select
        id={id}
        className="w-52"
        options={[{ value: '', label: 'All clusters' }, ...contexts.map((c) => ({ value: c, label: c }))]}
        disabled
      />
    </div>
  );
}
