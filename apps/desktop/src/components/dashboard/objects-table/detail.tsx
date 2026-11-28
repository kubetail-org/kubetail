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

// An open object in full, in the column beside the list: a header that stays put over the
// kind's cards, which scroll.
import { Button } from '@kubetail/ui/elements/button';
import { ScrollText } from 'lucide-react';

import { ActionsMenu } from '@/components/dashboard/actions-menu';
import { Age, timestampMs } from '@/components/dashboard/age';
import { DetailPane } from '@/components/dashboard/detail-pane';
import { StatusPill } from '@/components/dashboard/status-pill';
import { VisibleText } from '@/components/primitives/visible-text';
import { useClusterCachedDataEvents } from '@/lib/cluster-cached-data-events';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { useHeldRow } from '@/lib/graphql/use-held-row';
import type { GVR } from '@/lib/gvr';
import { logsSourceKind } from '@/lib/object-actions';
import type { ObjectAction } from '@/lib/object-actions';
import { ownerOf } from '@/lib/object-owner';
import { visibleTitle } from '@/lib/visible-text';

import { ObjectCards } from './cards';
import type { EventsWatch } from './cards';
import type { Status } from './columns';

type ObjectDetailProps = {
  // The open object's uid.
  uid: string;
  gvr: GVR;
  // The kind's name, for the pane's label and the subtitle.
  kind: string;
  namespaced: boolean;
  // The open object as the rows hold it now: absent once it is gone, or while a reconnect
  // masks the rows.
  object: ClusterCachedDataObject | undefined;
  // The rows are the cache's whole present, after its snapshot's Bookmark: an absent object
  // is gone.
  settled: boolean;
  // The kind's status, for a kind with a status column.
  statusOf?: (o: ClusterCachedDataObject) => Status | undefined;
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void;
  onClose: () => void;
  className?: string;
};

// One events watch serves every object opened while the detail shows, so moving between
// objects reads no fresh snapshot. The pane under it is keyed on the uid, so what it holds is
// that object's.
export function ObjectDetail({ uid, ...props }: ObjectDetailProps) {
  const { events, phase } = useClusterCachedDataEvents({ unshared: true });
  return <OpenObject key={uid} {...props} events={{ events, phase }} />;
}

function OpenObject({
  gvr,
  kind,
  namespaced,
  object,
  settled,
  statusOf,
  onAction,
  onClose,
  className,
  events,
}: Omit<ObjectDetailProps, 'uid'> & { events: EventsWatch }) {
  const { shown, gone } = useHeldRow(object, settled);
  return (
    <DetailPane
      label={kind}
      title={shown && <Title kind={kind} object={shown} status={statusOf?.(shown)} />}
      // An object gone may come back under the same name as another, which a log view would follow.
      actions={shown && !gone && <Actions gvr={gvr} object={shown} onAction={onAction} />}
      closeLabel={shown ? `Close ${visibleTitle(shown.name)}` : 'Close'}
      note={gone ? 'This object no longer exists.' : undefined}
      onClose={onClose}
      className={className}
    >
      {shown && !gone && <ObjectCards gvr={gvr} object={shown} namespaced={namespaced} events={events} />}
    </DetailPane>
  );
}

function Title({ kind, object, status }: { kind: string; object: ClusterCachedDataObject; status?: Status }) {
  const owner = ownerOf(object.rawJSON);
  const created = timestampMs(object.creationTimestamp);
  return (
    <>
      <div className="flex min-w-0 items-center gap-2">
        <h2 className="truncate font-mono text-base/5.5 font-medium text-foreground" title={visibleTitle(object.name)}>
          <VisibleText text={object.name} />
        </h2>
        {status && <StatusPill label={status.label} tone={status.tone} />}
      </div>
      <p className="mt-0.5 truncate text-xs/4.5 text-muted-foreground">
        <VisibleText text={kind} />
        {owner && (
          <>
            {' · '}
            <VisibleText text={owner} />
          </>
        )}
        {!Number.isNaN(created) && (
          <>
            {' · created '}
            <Age ms={created} className="" /> ago
          </>
        )}
      </p>
    </>
  );
}

function Actions({
  gvr,
  object,
  onAction,
}: {
  gvr: GVR;
  object: ClusterCachedDataObject;
  onAction: (action: ObjectAction, object: ClusterCachedDataObject) => void;
}) {
  return (
    <>
      {logsSourceKind(gvr) && (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`Logs for ${visibleTitle(object.name)}`}
          onClick={() => onAction('logs', object)}
        >
          <ScrollText className="size-4" aria-hidden />
        </Button>
      )}
      <ActionsMenu gvr={gvr} name={object.name} onAction={(action) => onAction(action, object)} />
      <div aria-hidden className="h-4.5 w-px bg-border" />
    </>
  );
}
