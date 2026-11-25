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

// The Permissions pane's activity: the grants chats hold, and what ran
// without asking. Each list is drawn from its props; the pane passes none
// until the sidecar serves them, and a list given none says Coming soon.
import { Button } from '@kubetail/ui/elements/button';
import type { ReactNode } from 'react';

import type { PermissionEffect } from '@/gql/graphql';

import { Group } from './group';
import { PermissionRow } from './permission-rules';

export type ChatGrant = { id: string; effect: PermissionEffect; line: string; chatTitle: string; when: string };

export type Action = {
  id: string;
  outcome: 'Allowed' | 'Redacted' | 'Refused';
  summary: string;
  when: string;
  reason: string;
};

// Placeholder: awaits one query of every chat's grants with chat id, title and time.
export function ChatGrants({ grants, onRevoke }: { grants?: ChatGrant[]; onRevoke?: (id: string) => void }) {
  return (
    <Group title="Chat grants" lede="Approvals you gave for the rest of one chat. Revoking one asks again next time.">
      <Rows empty="No chat holds a grant.">
        {grants?.map((g) => (
          <PermissionRow key={g.id} badge={g.effect} line={g.line} detail={`${g.chatTitle} · ${g.when}`}>
            <Button type="button" variant="ghost" size="sm" onClick={() => onRevoke?.(g.id)}>
              Revoke
            </Button>
          </PermissionRow>
        ))}
      </Rows>
    </Group>
  );
}

type RanWithoutAskingProps = {
  actions?: Action[];
  /** How many ran in all, for View all. */
  total?: number;
  onOpenChat?: (id: string) => void;
  onViewAll?: () => void;
};

// Placeholder: awaits a query of the last 7 days' allowed, refused and redacted sandbox actions across chats.
export function RanWithoutAsking({ actions, total, onOpenChat, onViewAll }: RanWithoutAskingProps) {
  return (
    <Group
      title="Ran without asking"
      lede="Writes your mode or rules decided with nobody asked. Last 7 days."
      action={
        total ? (
          <Button type="button" variant="outline" size="sm" onClick={onViewAll}>
            View all {total}
          </Button>
        ) : undefined
      }
    >
      <Rows empty="Nothing ran without asking in the last 7 days.">
        {actions?.map((a) => (
          <PermissionRow key={a.id} badge={a.outcome} line={a.summary} detail={`${a.when} · ${a.reason}`}>
            <Button type="button" variant="ghost" size="sm" onClick={() => onOpenChat?.(a.id)}>
              Open chat
            </Button>
          </PermissionRow>
        ))}
      </Rows>
    </Group>
  );
}

// A card of rows, or Coming soon while there is no list: an empty one would
// claim nothing happened.
function Rows({ empty, children }: { empty: string; children: ReactNode[] | undefined }) {
  if (!children) return <p className="text-sm text-muted-foreground">Coming soon</p>;
  return (
    <ul className="divide-y rounded-lg border text-sm">
      {children.length === 0 && <li className="px-4 py-3 text-muted-foreground">{empty}</li>}
      {children}
    </ul>
  );
}
