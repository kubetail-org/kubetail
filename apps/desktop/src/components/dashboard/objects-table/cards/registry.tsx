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

// Which cards an open object's detail draws: a kind's own, keyed by `gvrKey` as the columns
// are, else the generic ones every kind gets.
import type { ReactNode } from 'react';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import { gvrKey } from '@/lib/gvr';
import type { GVR } from '@/lib/gvr';

import { EventsCard } from './events';
import type { EventsWatch } from './events';
import { ConditionsCard, DetailsCard, LabelsCard } from './generic';
import { CapacityCard, NodeDetailsCard } from './nodes';
import { ContainersCard, PodDetailsCard, ResourcesCard } from './pods';

export type ObjectCardsProps = {
  gvr: GVR;
  object: ClusterCachedDataObject;
  namespaced: boolean;
  // The cache's events, which the Events card filters for the object.
  events: EventsWatch;
};

// The cards a kind draws in place of the generic Details card.
const KINDS = new Map<string, (object: ClusterCachedDataObject) => ReactNode>([
  [
    'v1/pods',
    (object) => (
      <>
        <PodDetailsCard object={object} />
        <ContainersCard object={object} />
        <ResourcesCard object={object} />
      </>
    ),
  ],
  [
    'v1/nodes',
    (object) => (
      <>
        <NodeDetailsCard object={object} />
        <CapacityCard object={object} />
      </>
    ),
  ],
]);

export function ObjectCards({ gvr, object, namespaced, events }: ObjectCardsProps) {
  const kindCards = KINDS.get(gvrKey(gvr));
  return (
    <>
      {kindCards ? kindCards(object) : <DetailsCard object={object} namespaced={namespaced} />}
      <ConditionsCard object={object} />
      <EventsCard kind={object.kind} namespace={object.namespace} name={object.name} {...events} />
      <LabelsCard object={object} />
    </>
  );
}
