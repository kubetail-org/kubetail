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

// The Permissions pane's Rules card, and the row and effect badge its other
// lists share.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { cn } from '@kubetail/ui/lib/utils';
import { MoreHorizontal, Pencil, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import type { ReactNode } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import type { PermissionTarget } from '@/gql/graphql';
import type { Rule, RuleInput } from '@/lib/permission-settings';

import { Group } from './group';
import { RuleForm } from './permission-rule-form';
import type { AccountOption } from './permission-rule-form';
import { TestRequest } from './permission-test-request';

const OUTLINED = new Set(['Deny', 'Redacted', 'Refused']);

export function EffectBadge({ effect }: { effect: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 shrink-0 items-center rounded-md px-2 text-xs font-medium',
        OUTLINED.has(effect) ? 'border' : 'bg-secondary text-secondary-foreground',
      )}
    >
      {effect}
    </span>
  );
}

type RowProps = {
  badge: string;
  /** User, model or cluster text, as is detail. */
  line: string;
  detail?: string;
  /** The row's action. */
  children?: ReactNode;
};

export function PermissionRow({ badge, line, detail, children }: RowProps) {
  return (
    <li aria-label={line} className="flex items-center gap-3 px-4 py-3">
      <EffectBadge effect={badge} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5 break-all">
        <VisibleText text={line} />
        {detail && (
          <span className="text-xs text-muted-foreground">
            <VisibleText text={detail} />
          </span>
        )}
      </div>
      {children}
    </li>
  );
}

// Picked field by field: urql's cache adds a __typename the input refuses.
const inputOf = ({ effect, accessLevel, principal, target, scope, service, operation, resource }: Rule): RuleInput => ({
  effect,
  accessLevel,
  principal: principal ?? null,
  target: target ?? null,
  scope,
  service,
  operation,
  resource,
});

const NEW = 'new';

type Props = {
  rules: Rule[];
  /** The targets a rule may name, in order. */
  targets: PermissionTarget[];
  /** The accounts a rule may name, by target. */
  accounts: Partial<Record<PermissionTarget, AccountOption[]>>;
  /** The known contexts, for Test a request. */
  contexts: string[];
  /** Resolves to whether the sidecar took the rule. */
  onAdd: (input: RuleInput) => Promise<boolean>;
  /** Resolves to whether the sidecar took the edit. */
  onUpdate: (id: string, input: RuleInput) => Promise<boolean>;
  onRemove: (id: string) => void;
};

export function PermissionRules({ rules, targets, accounts, contexts, onAdd, onUpdate, onRemove }: Props) {
  // The id of the rule whose form is open in its row's place, or NEW.
  const [editing, setEditing] = useState<string | null>(null);
  const close = () => setEditing(null);

  // A refused write keeps the form open; the pane draws the refusal.
  const submit = async (write: Promise<boolean>) => {
    if (await write) close();
  };

  return (
    <Group
      title="Rules"
      lede="Checked before the mode. Each field matches anything when empty; a deny wins over an ask, and an ask over an allow."
      action={
        <Button type="button" variant="outline" size="sm" onClick={() => setEditing(NEW)}>
          <Plus aria-hidden />
          Add rule
        </Button>
      }
    >
      <ul className="divide-y rounded-lg border text-sm">
        {editing === NEW && (
          <li>
            <RuleForm
              targets={targets}
              accounts={accounts}
              onSubmit={(input) => submit(onAdd(input))}
              onCancel={close}
            />
          </li>
        )}
        {rules.length === 0 && editing !== NEW && <li className="px-4 py-3 text-muted-foreground">No rules yet.</li>}
        {rules.map((rule) =>
          rule.id === editing ? (
            <li key={rule.id}>
              <RuleForm
                targets={targets}
                accounts={accounts}
                initial={inputOf(rule)}
                onSubmit={(input) => submit(onUpdate(rule.id, input))}
                onCancel={close}
              />
            </li>
          ) : (
            // Placeholder: awaits each rule's origin, last match and match count on PermissionRule, for `detail`.
            <PermissionRow key={rule.id} badge={rule.effect} line={rule.line}>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={<Button type="button" variant="ghost" size="icon-sm" />}
                  aria-label={`Actions for ${rule.line}`}
                >
                  <MoreHorizontal className="size-4" aria-hidden />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" sideOffset={4}>
                  <DropdownMenuItem onClick={() => setEditing(rule.id)}>
                    <Pencil className="size-4" aria-hidden />
                    Edit
                  </DropdownMenuItem>
                  <DropdownMenuItem variant="destructive" onClick={() => onRemove(rule.id)}>
                    <Trash2 className="size-4" aria-hidden />
                    Remove
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </PermissionRow>
          ),
        )}
        <li className="flex items-center gap-3 px-4 py-3">
          <p className="flex-1 text-muted-foreground">See what a write would do under your modes and rules.</p>
          <TestRequest contexts={contexts} />
        </li>
      </ul>
    </Group>
  );
}
