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

// The Sandbox pane's Environment: what a sandboxed command's environment holds,
// and what never passes. Custom is the user's variables for every cluster that
// reach sandboxed commands, read-only here: the Environment tab edits them.
// Each name and value is their text, drawn through VisibleText.
// Placeholder: From your shell, Set by Kubetail and Never passed await a query of
// the run's environment table and NeverEnv, and Import from shell an import mutation.
import { Button } from '@kubetail/ui/elements/button';

import { VisibleText } from '@/components/primitives/visible-text';
import { type EnvVar, useEnvVars } from '@/lib/env-vars';
import { useSelectSettingsTab } from '@/lib/settings-tab';

import { Group, InfoPopover, NoteRow } from './group';

const FROM_SHELL = ['PATH', 'LANG', 'TZ'];
const SET_BY_KUBETAIL = ['HOME', 'TMPDIR', 'KUBECONFIG', 'KUBECACHEDIR', 'TERM=dumb'];
const NEVER_PASSED = [
  'SSH_AUTH_SOCK',
  'GITHUB_TOKEN',
  'GH_TOKEN',
  'GITHUB_ENTERPRISE_TOKEN',
  'GH_ENTERPRISE_TOKEN',
  'DOCKER_HOST',
  'AWS_*',
  'LD_PRELOAD',
  'LD_LIBRARY_PATH',
  'LD_AUDIT',
  'DYLD_INSERT_LIBRARIES',
  'DYLD_LIBRARY_PATH',
  'DYLD_FRAMEWORK_PATH',
  'BASH_ENV',
  'ENV',
  'PROMPT_COMMAND',
];

function Names({ names }: { names: string[] }) {
  return (
    <span className="flex flex-wrap gap-x-3 font-mono text-xs">
      {names.map((name) => (
        <span key={name}>
          <VisibleText text={name} trailing="end" />
        </span>
      ))}
    </span>
  );
}

function CustomVariables({ vars, loadError }: { vars: EnvVar[] | undefined; loadError: string | null }) {
  const sandboxed = vars?.filter((v) => v.target !== 'Outside' && !v.refusal);
  return (
    <div className="flex flex-col gap-1">
      <p className="text-xs font-medium">Custom</p>
      {loadError && (
        <p className="text-xs text-destructive">
          <VisibleText text={loadError} />
        </p>
      )}
      <p className="text-xs text-muted-foreground">
        {sandboxed?.length === 0 ? 'None for every cluster.' : 'For every cluster.'} A chat&apos;s cluster may add its
        own.
      </p>
      <ul className="divide-y" aria-label="Custom">
        {sandboxed?.map((v) => (
          <li key={v.name} className="py-1.5 font-mono text-xs break-all whitespace-pre-wrap">
            <VisibleText text={v.name} trailing="end" />
            <span className="text-muted-foreground">=</span>
            <VisibleText text={v.value} trailing="edges" />
          </li>
        ))}
      </ul>
    </div>
  );
}

export function SandboxEnvironment() {
  const selectTab = useSelectSettingsTab();
  const { vars, loadError } = useEnvVars(null);
  return (
    <Group
      title="Environment"
      lede="What a sandboxed command's environment holds, and what never passes."
      info={
        <InfoPopover label="Never passed">
          <p className="text-muted-foreground">Never passed, even when a command sets it.</p>
          <ul aria-label="Never passed" className="font-mono">
            {NEVER_PASSED.map((name) => (
              <li key={name}>{name}</li>
            ))}
          </ul>
        </InfoPopover>
      }
      action={
        <div className="flex gap-2">
          <Button variant="outline" size="sm" disabled>
            Import from shell
          </Button>
          <Button variant="outline" size="sm" onClick={() => selectTab('Environment')}>
            Edit variables
          </Button>
        </div>
      }
    >
      <ul className="divide-y rounded-lg border">
        <NoteRow label="From your shell">
          <Names names={FROM_SHELL} />
        </NoteRow>
        <NoteRow label="Set by Kubetail">
          <Names names={SET_BY_KUBETAIL} />
        </NoteRow>
      </ul>
      <p className="text-xs text-muted-foreground">Coming soon</p>
      <CustomVariables vars={vars} loadError={loadError} />
    </Group>
  );
}
