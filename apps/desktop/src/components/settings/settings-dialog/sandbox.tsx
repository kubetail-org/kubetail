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

// The Settings dialog's Sandbox tab: the status card, then Folders, Network,
// PATH, Executables and Environment, each a titled card in a file beside this
// one but Executables. On a machine with no sandbox, the status card alone. An
// executable's path, version, error and refusal are its own text, so they are
// drawn through VisibleText with the trailing spelled.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { useState } from 'react';

import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import { useSandbox } from '@/lib/sandbox';
import { useSandboxExecutables } from '@/lib/sandbox-executables';
import type { SandboxExecutable, SandboxExecutables } from '@/lib/sandbox-executables';
import { useSandboxPath } from '@/lib/sandbox-path';

import { Group } from './group';
import { SandboxCard } from './sandbox-card';
import { SandboxConsole } from './sandbox-console';
import { SandboxEnvironment } from './sandbox-environment';
import { SandboxFolderGroups } from './sandbox-folders';
import { SandboxNetwork } from './sandbox-network';
import { SandboxPathSection } from './sandbox-path';

// A registered executable can share a curated one's name, so a row is keyed
// by both.
function executableKey({ registered, name }: SandboxExecutable): string {
  return `${registered ? 'registered' : 'curated'}:${name}`;
}

function ExecutableRow({ executable }: { executable: SandboxExecutable }) {
  const { name, probed, resolved, shim, target, ok, version, error, refusal } = executable;
  // A refused executable is never probed: its refusal stands in for the result.
  const failure = refusal ?? (probed && resolved && error);
  return (
    <li className="px-4 py-2 text-xs">
      <p className="flex gap-2">
        <span className="font-mono font-medium break-all whitespace-pre-wrap">
          <VisibleText text={name} trailing="end" />
        </span>
        {probed && resolved && <span className="text-muted-foreground">{ok ? 'ok' : 'failed'}</span>}
      </p>
      {!refusal && (!probed || !resolved) && <p className="text-muted-foreground">{error}</p>}
      {resolved && (
        <p className="font-mono break-all whitespace-pre-wrap text-muted-foreground" data-testid="resolved">
          <VisibleText text={resolved} trailing="end" />
        </p>
      )}
      {shim && (
        <p className="text-muted-foreground">
          <span>shim →</span>{' '}
          <span className="font-mono break-all whitespace-pre-wrap" data-testid="target">
            <VisibleText text={target} trailing="end" />
          </span>
        </p>
      )}
      {version && (
        <p className="break-all whitespace-pre-wrap">
          <VisibleText text={version} trailing="end" />
        </p>
      )}
      {failure && (
        <p className="break-all whitespace-pre-wrap text-destructive">
          <VisibleText text={failure} trailing="end" />
        </p>
      )}
    </li>
  );
}

// The user's own executables, each with Remove, and the form that adds one.
function RegisteredExecutables({ executables }: { executables: SandboxExecutables }) {
  const { report, changing, registerError, removeError, register, remove } = executables;
  const [name, setName] = useState('');
  const [invocation, setInvocation] = useState('');
  const registered = report?.filter((executable) => executable.registered) ?? [];

  const add = async () => {
    if (await register(name, invocation)) {
      setName('');
      setInvocation('');
    }
  };

  return (
    <div className="flex flex-col gap-1">
      <p className="text-xs font-medium">Your executables</p>
      <ul className="divide-y" aria-label="Registered executables">
        {registered.map((executable) => (
          <li key={executableKey(executable)} className="flex items-center gap-2 py-1.5 text-xs">
            <span className="min-w-0 flex-1 font-mono break-all whitespace-pre-wrap">
              <VisibleText text={executable.invocation} trailing="end" />
            </span>
            <Button size="sm" variant="ghost" disabled={changing} onClick={() => remove(executable.name)}>
              Remove
            </Button>
          </li>
        ))}
      </ul>
      {removeError && <p className="text-xs text-destructive">{removeError}</p>}
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Executable name"
          className="w-40 font-mono text-xs"
          placeholder="name"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <Input
          aria-label="Invocation"
          className="min-w-48 flex-1 font-mono text-xs"
          placeholder="<name> --version"
          value={invocation}
          onChange={(e) => setInvocation(e.target.value)}
        />
        <Button size="sm" variant="outline" disabled={changing || name === ''} onClick={add}>
          Add executable
        </Button>
      </div>
      {registerError && <p className="text-xs text-destructive">{registerError}</p>}
    </div>
  );
}

// kubectlMissing reports whether the probe ran kubectl and found no binary.
function kubectlMissing(report: SandboxExecutable[] | undefined): boolean {
  const kubectl = report?.find((executable) => executable.name === 'kubectl');
  return !!kubectl?.probed && !kubectl.resolved;
}

function SandboxExecutableSection({ executables }: { executables: SandboxExecutables }) {
  const { report, probing, probeError, probe } = executables;
  return (
    <Group
      title="Executables"
      lede="Kubetail runs each executable once in the sandbox, with no cluster and no network, to see what it needs."
      action={
        <Button size="sm" variant="outline" disabled={probing} onClick={() => probe()}>
          {probing ? (
            <>
              <Spinner size="xs" className="mr-0" />
              Probing…
            </>
          ) : (
            'Probe again'
          )}
        </Button>
      }
    >
      {probeError && <p className="text-xs text-destructive">{probeError}</p>}
      {kubectlMissing(report) && (
        <p className="text-xs text-destructive">
          kubectl was not found on the sandbox&apos;s PATH. Install it, or include its folder above.
        </p>
      )}
      <ul className="divide-y rounded-lg border" aria-label="Executables">
        {report?.map((executable) => (
          <ExecutableRow key={executableKey(executable)} executable={executable} />
        ))}
      </ul>
      <RegisteredExecutables executables={executables} />
    </Group>
  );
}

// The sections below the status card. Folders and PATH share one PATH reader,
// so a refresh holds both; PATH's import probes the executables again.
function SandboxSections({
  networkAvailable,
  networkReason,
}: {
  networkAvailable: boolean | undefined;
  networkReason: string;
}) {
  const path = useSandboxPath();
  const executables = useSandboxExecutables();
  return (
    <>
      <SandboxFolderGroups path={path} />
      <SandboxNetwork networkAvailable={networkAvailable} networkReason={networkReason} />
      <SandboxPathSection path={path} onRefreshed={executables.probe} />
      <SandboxExecutableSection executables={executables} />
      <SandboxEnvironment />
    </>
  );
}

export function SandboxSettings() {
  const sandbox = useSandbox();
  const [consoleOpen, setConsoleOpen] = useState(false);
  return (
    <>
      {/* Placeholder: awaits a global sandbox setting and its mutation, so the switch is on and disabled. */}
      <SandboxCard sandbox={sandbox} on onTry={() => setConsoleOpen(true)} />
      {sandbox.available && (
        <>
          <SandboxSections networkAvailable={sandbox.networkAvailable} networkReason={sandbox.networkReason} />
          <SandboxConsole
            open={consoleOpen}
            onOpenChange={setConsoleOpen}
            networkAvailable={sandbox.networkAvailable}
          />
        </>
      )}
    </>
  );
}
