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

// The problem pods: failing before pending, the longest-standing first. Each row's menu is
// the Pods table's. In a narrow card the Namespace and Since columns give way, so the
// problem and the menu stay in view, and the namespace joins the owner line.
import { ActionsMenu } from '@/components/dashboard/actions-menu';
import { StatusPill } from '@/components/dashboard/status-pill';
import { Elapsed } from '@/components/primitives/elapsed';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ObjectAction } from '@/lib/object-actions';
import { useObjectView } from '@/lib/object-view';
import { PODS, podOwnerLine } from '@/lib/overview';
import type { ProblemPod } from '@/lib/overview';
import { visibleTitle } from '@/lib/visible-text';

import { OverviewCard } from './card';
import type { CardColumn } from './card';

// The fixed columns need 432px before Object has any, so they show from the `@xl` (576px)
// card up.
const WIDE_ONLY = 'hidden @xl:block';
const WIDE_HIDDEN = '@xl:hidden';

function columns(onAction: (action: ObjectAction, pod: ProblemPod) => void): CardColumn<ProblemPod>[] {
  return [
    {
      key: 'object',
      header: 'Object',
      cell: ({ object }) => {
        const owner = podOwnerLine(object);
        return (
          <div className="flex flex-col gap-0.5">
            <div className="truncate font-mono text-primary" title={visibleTitle(object.name)}>
              <VisibleText text={object.name} />
            </div>
            <div className="truncate text-muted-foreground" title={visibleTitle(owner)}>
              <span className={WIDE_HIDDEN}>
                <VisibleText text={object.namespace} />
                {' · '}
              </span>
              <VisibleText text={owner} />
            </div>
          </div>
        );
      },
    },
    {
      key: 'namespace',
      header: 'Namespace',
      className: WIDE_ONLY,
      cell: ({ object }) => (
        <span title={visibleTitle(object.namespace)}>
          <VisibleText text={object.namespace} />
        </span>
      ),
    },
    { key: 'problem', header: 'Problem', cell: ({ status }) => <StatusPill label={status.label} tone={status.tone} /> },
    {
      key: 'since',
      header: 'Since',
      className: WIDE_ONLY,
      cell: ({ sinceMs }) => (Number.isNaN(sinceMs) ? '—' : <Elapsed since={sinceMs} />),
    },
    {
      key: 'actions',
      header: '',
      cell: (pod) => <ActionsMenu gvr={PODS} name={pod.object.name} onAction={(action) => onAction(action, pod)} />,
    },
  ];
}

type NeedsAttentionProps = { problems?: readonly ProblemPod[]; syncing?: boolean };

export function NeedsAttention({ problems, syncing = false }: NeedsAttentionProps) {
  const { set: focusObject } = useObjectView();
  const onAction = (action: ObjectAction, { object: { uid, namespace, name } }: ProblemPod) =>
    focusObject({ ...PODS, kind: 'Pod', uid, namespace, name }, action);
  const n = problems?.length ?? 0;
  return (
    <OverviewCard
      title="Needs attention"
      count={n}
      grid="grid-cols-[minmax(0,1fr)_120px_28px] @xl:grid-cols-[minmax(0,1fr)_130px_170px_56px_28px]"
      columns={columns(onAction)}
      rows={problems}
      rowKey={(p) => p.object.uid}
      empty={syncing ? 'No problem pods found yet' : 'Nothing needs attention'}
      link={{ label: 'Open Pods', resource: 'pods' }}
    />
  );
}
