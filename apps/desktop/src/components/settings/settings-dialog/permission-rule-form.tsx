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

// The form a rule is added or edited by: its effect, what it applies to, who
// sent it and what it reaches, then its account and scope and the operation,
// each optional and empty for anything. The scope is built from the account
// and the place under it by permission-pattern.ts, the account always a
// literal.
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { useState } from 'react';
import type { SubmitEvent } from 'react';

import { Input } from '@/components/primitives/text-entry';
import type { PermissionAccessLevel, PermissionEffect, PermissionPrincipal, PermissionTarget } from '@/gql/graphql';
import { CLOUD_PLATFORM_NAMES } from '@/lib/chats';
import { parseScope, scopeFor } from '@/lib/permission-pattern';
import type { RuleInput } from '@/lib/permission-settings';

import { Field, Select } from './field';

const EFFECTS = (['Allow', 'Deny', 'Ask'] satisfies PermissionEffect[]).map((value) => ({ value, label: value }));

// The levels a rule may name. A destructive change is never allowed by a
// rule, so Allow does not offer it.
export const LEVELS: { value: PermissionAccessLevel; label: string }[] = [
  { value: 'Write', label: 'Writes' },
  { value: 'DataRead', label: 'Data reads' },
  { value: 'AccessControl', label: 'Access control changes' },
  { value: 'Destructive', label: 'Destructive changes' },
];

// The principals a rule may name; the boundary alone decides the monitor and
// the console. An Allow for a subagent would never win over its parent's
// verdict, so Allow does not offer it.
const PRINCIPALS: { value: '' | PermissionPrincipal; label: string }[] = [
  { value: '', label: 'Anyone' },
  { value: 'Chat', label: 'A chat' },
  { value: 'Subagent', label: 'A subagent' },
];

// What the place under an account is called, by target.
const PLACES: Record<PermissionTarget, string> = {
  Kubernetes: 'Namespace',
  AWS: 'Region',
  Azure: 'Resource group',
  GCP: 'Location',
  GitHub: 'Repository',
};

// What a scope of its account alone reaches, by target: the checkbox's label.
// An AWS action's scope always names a region, global included.
const ACCOUNT_ONLY: Partial<Record<PermissionTarget, string>> = {
  Kubernetes: 'Cluster-scoped objects only, instead of a namespace',
  Azure: 'The subscription itself only, instead of a resource group',
  GCP: 'The project itself only, instead of a location',
  GitHub: 'The owner itself only, instead of a repository',
};

// The select value for every one: a null principal, target or account.
const EVERY = '';

export type AccountOption = { value: string; label: string };

// A rule as the form edits it. Each text field is empty for any; an account
// of null is every account. rawScope holds a scope the form cannot draw, kept
// as it was.
type Draft = {
  effect: PermissionEffect;
  accessLevel: PermissionAccessLevel;
  principal: PermissionPrincipal | null;
  target: PermissionTarget | null;
  account: string | null;
  place: string;
  accountOnly: boolean;
  rawScope: string | null;
  // An empty service off Kubernetes, which no text in the field can say: an
  // Azure action with no provider namespace, say. It stays until edited.
  emptyService: boolean;
  service: string;
  operation: string;
  resource: string;
};

const EMPTY_DRAFT: Draft = {
  effect: 'Allow',
  accessLevel: 'Write',
  principal: null,
  target: 'Kubernetes',
  account: null,
  place: '',
  accountOnly: false,
  rawScope: null,
  emptyService: false,
  service: '',
  operation: '',
  resource: '',
};

// A pattern field as the form shows it: `*` is empty. Kubernetes' core group
// is the empty service, which the form spells `core`.
// An empty service is Kubernetes' core group, which a rule for every target
// can name too.
const namesCoreGroup = (target: PermissionTarget | null) => target === null || target === 'Kubernetes';

function shown(pattern: string, coreGroup = false): string {
  if (pattern === '*') return '';
  if (coreGroup && pattern === '') return 'core';
  return pattern;
}

function sent(text: string, coreGroup = false): string {
  if (text === '') return '*';
  if (coreGroup && text === 'core') return '';
  return text;
}

// The account an Allow names: the one picked, else the first listed, since
// an Allow never names every account. A scope the form cannot draw keeps its
// own account.
function accountOf(draft: Draft, listed: AccountOption[], allow: boolean): string | null {
  if (draft.target === null) return null;
  if (allow && draft.account === null && draft.rawScope === null) return listed[0]?.value ?? null;
  return draft.account;
}

function draftOf(rule: RuleInput): Draft {
  const coreGroup = namesCoreGroup(rule.target);
  // A rule for every target names an account no target lists, so its scope is
  // kept as it was, as one the form cannot draw.
  const parsed = parseScope(rule.scope);
  const choice = rule.target === null && parsed?.account != null ? null : parsed;
  return {
    effect: rule.effect,
    accessLevel: rule.accessLevel,
    principal: rule.principal,
    target: rule.target,
    account: choice?.account ?? null,
    place: choice?.place ?? '',
    accountOnly: choice?.accountOnly ?? false,
    rawScope: choice ? null : rule.scope,
    emptyService: !coreGroup && rule.service === '',
    service: shown(rule.service, coreGroup),
    operation: shown(rule.operation),
    resource: shown(rule.resource),
  };
}

function inputOf(d: Draft): RuleInput {
  const coreGroup = namesCoreGroup(d.target);
  return {
    effect: d.effect,
    accessLevel: d.accessLevel,
    principal: d.principal,
    target: d.target,
    scope: d.rawScope ?? scopeFor({ account: d.account, place: d.place, accountOnly: d.accountOnly }),
    service: d.emptyService ? '' : sent(d.service, coreGroup),
    operation: sent(d.operation),
    resource: sent(d.resource),
  };
}

const sameRule = (a: RuleInput, b: RuleInput) =>
  (Object.keys(a) as (keyof RuleInput)[]).every((key) => a[key] === b[key]);

type Props = {
  /** The targets a rule may name, in order. */
  targets: PermissionTarget[];
  /** The accounts a rule may name, by target: Kubernetes' clusters by id, each cloud platform's listed accounts. */
  accounts: Partial<Record<PermissionTarget, AccountOption[]>>;
  /** The rule being edited; the form adds a new one when unset. */
  initial?: RuleInput;
  onSubmit: (input: RuleInput) => void;
  onCancel: () => void;
};

export function RuleForm({ targets, accounts, initial, onSubmit, onCancel }: Props) {
  const [draft, setDraft] = useState(() => (initial ? draftOf(initial) : EMPTY_DRAFT));
  const set = (over: Partial<Draft>) => setDraft((d) => ({ ...d, ...over }));
  const allow = draft.effect === 'Allow';
  const rawScope = draft.rawScope;

  // What the effect no longer offers falls back to the first it does.
  const levels = LEVELS.filter((l) => !(allow && l.value === 'Destructive'));
  const accessLevel = levels.some((l) => l.value === draft.accessLevel) ? draft.accessLevel : levels[0].value;
  const principals = PRINCIPALS.filter((p) => !(allow && p.value === 'Subagent'));
  const principal = allow && draft.principal === 'Subagent' ? null : draft.principal;
  // An Allow names one target and one account, since a rule for every account
  // would reach one added later; Deny and Ask may name every one.
  const targetOptions = [
    ...(allow ? [] : [{ value: EVERY, label: 'Every target' }]),
    ...targets.map((t) => ({ value: t as string, label: t === 'Kubernetes' ? 'Clusters' : CLOUD_PLATFORM_NAMES[t] })),
  ];
  const target = allow && draft.target === null ? (targets[0] ?? null) : draft.target;
  const kube = target === 'Kubernetes';
  const known = target ? (accounts[target] ?? []) : [];
  // A rule's own account stays on offer though no mode lists it: an Always
  // answer can name an account on its target's default.
  const listed =
    draft.account !== null && !known.some((a) => a.value === draft.account)
      ? [...known, { value: draft.account, label: draft.account }]
      : known;
  const accountOptions = [
    ...(allow ? [] : [{ value: EVERY, label: kube ? 'Every cluster' : 'Every account' }]),
    ...listed,
  ];
  const account = accountOf({ ...draft, target }, listed, allow);
  const fixed = { ...draft, accessLevel, principal, target, account };
  const rule = inputOf(fixed);

  // A target names its own accounts and places, so picking one clears them.
  const pickTarget = (value: string) =>
    set({
      target: value === EVERY ? null : (value as PermissionTarget),
      account: null,
      place: '',
      accountOnly: false,
      rawScope: null,
      emptyService: false,
    });

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    onSubmit(rule);
  };

  const text = (name: 'place' | 'service' | 'operation' | 'resource', label: string, shut = false) => (
    <Field label={label}>
      {(id) => (
        <Input
          id={id}
          placeholder={name === 'service' && draft.emptyService ? 'None' : 'Any'}
          value={shut ? '' : fixed[name]}
          disabled={shut}
          onChange={(e) =>
            set({
              [name]: e.target.value,
              rawScope: name === 'place' ? null : rawScope,
              emptyService: name === 'service' ? false : draft.emptyService,
            })
          }
        />
      )}
    </Field>
  );

  return (
    <form aria-label={initial ? 'Edit rule' : 'Add a rule'} className="flex flex-col gap-3 p-4" onSubmit={submit}>
      <div className="grid grid-cols-2 gap-3">
        <Field label="Effect">
          {(id) => <Select id={id} options={EFFECTS} value={draft.effect} onChange={(effect) => set({ effect })} />}
        </Field>
        <Field label="Applies to">
          {(id) => (
            <Select id={id} options={levels} value={accessLevel} onChange={(value) => set({ accessLevel: value })} />
          )}
        </Field>
        <Field label="Sent by">
          {(id) => (
            <Select
              id={id}
              options={principals}
              value={principal ?? EVERY}
              onChange={(value) => set({ principal: value === EVERY ? null : value })}
            />
          )}
        </Field>
        <Field label="Reaches">
          {(id) => <Select id={id} options={targetOptions} value={target ?? EVERY} onChange={pickTarget} />}
        </Field>
        <Field label={kube ? 'Cluster' : 'Account'}>
          {(id) => (
            <Select
              id={id}
              options={accountOptions}
              value={account ?? EVERY}
              disabled={target === null}
              onChange={(value) =>
                value === EVERY
                  ? set({ account: null, accountOnly: false, rawScope: null })
                  : set({ account: value, rawScope: null })
              }
            />
          )}
        </Field>
        {rawScope !== null ? (
          <Field label="Scope">{(id) => <Input id={id} value={rawScope} disabled />}</Field>
        ) : (
          text('place', target ? PLACES[target] : 'Namespace or place', fixed.accountOnly)
        )}
      </div>
      <div className="grid grid-cols-3 gap-3">
        {text('operation', kube ? 'Verb' : 'Operation')}
        {text('service', kube ? 'Group' : 'Service')}
        {text('resource', 'Resource')}
      </div>
      {target !== null && ACCOUNT_ONLY[target] && account !== null && rawScope === null && (
        <label className="flex items-center gap-2">
          <Checkbox
            checked={fixed.accountOnly}
            onCheckedChange={(checked) => set({ accountOnly: checked, place: '' })}
          />
          {ACCOUNT_ONLY[target]}
        </label>
      )}
      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
          Cancel
        </Button>
        {/* Adding a rule the always rules hold writes nothing, so an unchanged edit would only remove the original. */}
        <Button
          type="submit"
          size="sm"
          disabled={
            (initial && sameRule(rule, initial)) ||
            (allow && (target === null || (account === null && rawScope === null)))
          }
        >
          {initial ? 'Save' : 'Add'}
        </Button>
      </div>
    </form>
  );
}
