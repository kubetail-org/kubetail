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

// The Settings dialog's Environment tab: the variables the user gives their
// commands, for every cluster or one, each reaching commands in the sandbox,
// outside it, or both. A name and a value are the user's text, drawn through
// VisibleText.
import { Button } from '@kubetail/ui/elements/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { useState } from 'react';

import { Input, Textarea } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import { EVERY, useScope } from '@/lib/cluster-scope';
import { type EnvVar, type EnvVars, type EnvVarTarget, useEnvVars } from '@/lib/env-vars';

import { Intro } from './group';
import { ScopePicker } from './scope-picker';

const TARGETS: Record<EnvVarTarget, string> = {
  Both: 'All commands',
  Sandbox: 'Sandboxed commands',
  Outside: 'Commands outside the sandbox',
};

export function EnvironmentSettings() {
  const [picked, setPicked] = useState(EVERY);
  const scope = useScope(picked);
  const clusterID = scope.cluster?.id ?? null;
  const env = useEnvVars(clusterID);
  return (
    <Intro lede="Variables set on the commands a chat runs. A cluster's value replaces every cluster's of the same name. Not secrets: any command can print one.">
      <ScopePicker scope={scope} onPick={setPicked} />
      {/* Keyed on the scope, so no open form carries over to another. */}
      <Variables key={clusterID} env={env} />
    </Intro>
  );
}

// The open form's variable: a listed name, ADDING for a new one, null for no
// form. No variable's name is empty.
const ADDING = '';

function Variables({ env }: { env: EnvVars }) {
  const [chosen, setForm] = useState<string | null>(null);
  // A variable removed in another window closes its form.
  const form = chosen === ADDING || env.vars?.some((v) => v.name === chosen) ? chosen : null;
  const close = () => setForm(null);
  return (
    <div className="flex flex-col gap-1">
      {env.loadError && (
        <p className="text-xs text-destructive">
          <VisibleText text={env.loadError} />
        </p>
      )}
      {env.vars?.length === 0 && form === null && <p className="text-xs text-muted-foreground">No variables.</p>}
      <ul className="divide-y" aria-label="Variables">
        {env.vars?.map((v) =>
          form === v.name ? (
            // Keyed on what it holds, so a change from another window reopens the form with it.
            <li key={`${v.name}=${v.value} ${v.target}`} className="py-1.5">
              <VariableForm env={env} variable={v} onDone={close} />
            </li>
          ) : (
            <li key={v.name} className="flex items-center gap-2 py-1.5 text-xs">
              <VariableText variable={v} />
              {/* Held while a form is open, so switching forms drops nothing typed. */}
              <Button
                size="sm"
                variant="ghost"
                disabled={env.changing || form !== null}
                onClick={() => setForm(v.name)}
              >
                Edit
              </Button>
              <Button size="sm" variant="ghost" disabled={env.changing} onClick={() => env.remove(v.name)}>
                Remove
              </Button>
            </li>
          ),
        )}
      </ul>
      {form === ADDING && <VariableForm env={env} onDone={close} />}
      {env.removeError && (
        <p className="text-xs text-destructive">
          <VisibleText text={env.removeError} />
        </p>
      )}
      <Button
        variant="outline"
        size="sm"
        className="self-start"
        disabled={form !== null}
        onClick={() => setForm(ADDING)}
      >
        Add variable
      </Button>
    </div>
  );
}

function VariableText({ variable: v }: { variable: EnvVar }) {
  return (
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="font-mono break-all whitespace-pre-wrap">
        <VisibleText text={v.name} trailing="end" />
        <span className="text-muted-foreground">=</span>
        <VisibleText text={v.value} trailing="edges" />
      </span>
      {v.refusal ? (
        <span className="text-destructive">
          Not given to any command: <VisibleText text={v.refusal} />
        </span>
      ) : (
        <span className="text-muted-foreground">{TARGETS[v.target]}</span>
      )}
    </span>
  );
}

// The form a variable is added or edited in; an edit keeps its name, and an add
// takes no listed one, since a set changes a listed name in place.
function VariableForm({
  env,
  variable,
  onDone,
}: {
  env: EnvVars;
  /** The variable being edited; none to add one. */
  variable?: EnvVar;
  onDone: () => void;
}) {
  const [name, setName] = useState(variable?.name ?? '');
  const [value, setValue] = useState(variable?.value ?? '');
  const [target, setTarget] = useState<EnvVarTarget>(variable?.target ?? 'Both');
  const [refusal, setRefusal] = useState<string | null>(null);
  const listed = variable === undefined && env.vars?.some((v) => v.name === name);

  const save = async () => {
    const refused = await env.set(name, value, target);
    if (refused === null) onDone();
    else setRefusal(refused);
  };

  return (
    <div className="flex flex-col gap-1 rounded-lg border px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Variable name"
          className="w-48 font-mono text-xs"
          placeholder="NAME"
          value={name}
          disabled={variable !== undefined}
          onChange={(e) => setName(e.target.value)}
        />
        {/* A value may span lines, which an input would drop. */}
        <Textarea
          aria-label="Variable value"
          className="min-h-8 min-w-48 flex-1 font-mono text-xs"
          rows={1}
          placeholder="value"
          value={value}
          onChange={(e) => setValue(e.target.value)}
        />
        <Select value={target} onValueChange={(next) => setTarget(next as EnvVarTarget)}>
          <SelectTrigger size="sm" aria-label="Given to" className="w-56">
            <SelectValue>{TARGETS[target]}</SelectValue>
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            {Object.entries(TARGETS).map(([each, label]) => (
              <SelectItem key={each} value={each}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button size="sm" variant="outline" disabled={env.changing || name === '' || listed} onClick={save}>
          Save
        </Button>
        {/* Held while a save is in flight, since its answer closes the form. */}
        <Button size="sm" variant="ghost" disabled={env.changing} onClick={onDone}>
          Cancel
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Not a secret: any command can print it. A command outside the sandbox shows its variables when it asks.
      </p>
      {listed && (
        <p className="text-xs text-destructive">
          <VisibleText text={name} trailing="end" /> is already listed. Edit it instead.
        </p>
      )}
      {refusal && (
        <p className="text-xs text-destructive">
          <VisibleText text={refusal} />
        </p>
      )}
    </div>
  );
}
