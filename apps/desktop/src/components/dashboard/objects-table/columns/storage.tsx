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

// Storage kinds' columns: kubectl's, with a phase pill where the kind has one.
import type { StatusTone } from '@/lib/cluster-health';

import { countColumn, kind, labelsText, listColumn, quantityColumn, statusColumn, textColumn } from './common';
import type { KindColumn, Status } from './common';

// kubectl's abbreviations; a mode it lacks is drawn as written.
const ACCESS_MODE: Record<string, string> = {
  ReadWriteOnce: 'RWO',
  ReadOnlyMany: 'ROX',
  ReadWriteMany: 'RWX',
  ReadWriteOncePod: 'RWOP',
};

function accessModes(modes: readonly string[] | undefined): string[] | undefined {
  return modes?.map((m) => (Object.hasOwn(ACCESS_MODE, m) ? ACCESS_MODE[m] : m));
}

type Phased = { metadata?: { deletionTimestamp?: string }; status?: { phase?: string } };

// A phase and its tone, or nothing when the object has none yet. Deletion wins, as in
// kubectl: a claim held by its protection finalizer keeps the phase `Bound`.
function phaseStatus(b: Phased, tones: Record<string, StatusTone>): Status | undefined {
  if (b.metadata?.deletionTimestamp) return { label: 'Terminating', tone: 'pending' };
  const phase = b.status?.phase;
  if (!phase) return undefined;
  return { label: phase, tone: Object.hasOwn(tones, phase) ? tones[phase] : 'pending' };
}

type PVBody = Phased & {
  spec?: {
    capacity?: { storage?: string };
    accessModes?: string[];
    persistentVolumeReclaimPolicy?: string;
    claimRef?: { namespace?: string; name?: string };
    storageClassName?: string;
    volumeMode?: string;
  };
  status?: { phase?: string; reason?: string };
};

const pvStatus = (b: PVBody) =>
  phaseStatus(b, {
    Bound: 'ok',
    Available: 'ok',
    Released: 'pending',
    Pending: 'pending',
    Failed: 'failing',
  });

const pvColumns: KindColumn<PVBody>[] = [
  statusColumn('Status', 112, pvStatus),
  quantityColumn('Capacity', 88, (b) => b.spec?.capacity?.storage),
  listColumn('Access modes', 112, (b) => accessModes(b.spec?.accessModes)),
  textColumn('Reclaim policy', 120, (b) => b.spec?.persistentVolumeReclaimPolicy),
  textColumn('Claim', 192, (b) => {
    const c = b.spec?.claimRef;
    return c?.name && (c.namespace ? `${c.namespace}/${c.name}` : c.name);
  }),
  textColumn('Storage class', 136, (b) => b.spec?.storageClassName),
  textColumn('Reason', 136, (b) => b.status?.reason, { hidden: true }),
  textColumn('Volume mode', 112, (b) => b.spec?.volumeMode, { hidden: true }),
];

type PVCBody = Phased & {
  spec?: {
    volumeName?: string;
    storageClassName?: string;
    volumeMode?: string;
    resources?: { requests?: { storage?: string } };
  };
  status?: { phase?: string; capacity?: { storage?: string }; accessModes?: string[] };
};

const pvcStatus = (b: PVCBody) => phaseStatus(b, { Bound: 'ok', Pending: 'pending', Lost: 'failing' });

const pvcColumns: KindColumn<PVCBody>[] = [
  statusColumn('Status', 112, pvcStatus),
  textColumn('Volume', 192, (b) => b.spec?.volumeName, { mono: true }),
  // What the claim holds; kubectl prints it blank until bound.
  quantityColumn('Capacity', 88, (b) => b.status?.capacity?.storage),
  listColumn('Access modes', 112, (b) => accessModes(b.status?.accessModes)),
  textColumn('Storage class', 136, (b) => b.spec?.storageClassName),
  quantityColumn('Requested', 96, (b) => b.spec?.resources?.requests?.storage, { hidden: true }),
  textColumn('Volume mode', 112, (b) => b.spec?.volumeMode, { hidden: true }),
];

type StorageClassBody = {
  metadata?: { annotations?: Record<string, string> };
  provisioner?: string;
  reclaimPolicy?: string;
  volumeBindingMode?: string;
  allowVolumeExpansion?: boolean;
};

// Both annotations mark the default; the beta one is what older clusters set.
function isDefaultClass(b: StorageClassBody): boolean {
  const a = b.metadata?.annotations;
  return (
    a?.['storageclass.kubernetes.io/is-default-class'] === 'true' ||
    a?.['storageclass.beta.kubernetes.io/is-default-class'] === 'true'
  );
}

// Unset fields read as the API's defaults, as kubectl prints them.
const storageClassColumns: KindColumn<StorageClassBody>[] = [
  textColumn('Default', 72, (b) => (isDefaultClass(b) ? 'Yes' : undefined)),
  textColumn('Provisioner', 200, (b) => b.provisioner, { mono: true }),
  textColumn('Reclaim policy', 120, (b) => b.reclaimPolicy ?? 'Delete'),
  textColumn('Binding mode', 160, (b) => b.volumeBindingMode ?? 'Immediate'),
  textColumn('Expansion', 88, (b) => String(b.allowVolumeExpansion ?? false)),
];

type VolumeAttachmentBody = {
  spec?: { attacher?: string; nodeName?: string; source?: { persistentVolumeName?: string } };
  status?: { attached?: boolean; attachError?: { message?: string }; detachError?: { message?: string } };
};

// An error on either side is the attacher giving up for now; it retries.
function attachmentStatus(b: VolumeAttachmentBody): Status {
  if (b.status?.attachError) return { label: 'Attach failed', tone: 'failing' };
  if (b.status?.detachError) return { label: 'Detach failed', tone: 'failing' };
  return b.status?.attached ? { label: 'Attached', tone: 'ok' } : { label: 'Attaching', tone: 'pending' };
}

const volumeAttachmentColumns: KindColumn<VolumeAttachmentBody>[] = [
  statusColumn('Status', 128, attachmentStatus),
  textColumn('Attacher', 200, (b) => b.spec?.attacher, { mono: true }),
  textColumn('PV', 192, (b) => b.spec?.source?.persistentVolumeName, { mono: true }),
  textColumn('Node', 128, (b) => b.spec?.nodeName, { mono: true }),
  textColumn('Error', 224, (b) => b.status?.attachError?.message ?? b.status?.detachError?.message, { hidden: true }),
];

type CSIDriverBody = {
  spec?: {
    attachRequired?: boolean;
    podInfoOnMount?: boolean;
    storageCapacity?: boolean;
    requiresRepublish?: boolean;
    tokenRequests?: { audience?: string }[];
    volumeLifecycleModes?: string[];
  };
};

const csiDriverColumns: KindColumn<CSIDriverBody>[] = [
  textColumn('Attach required', 120, (b) => String(b.spec?.attachRequired ?? true)),
  textColumn('Pod info on mount', 136, (b) => String(b.spec?.podInfoOnMount ?? false)),
  textColumn('Storage capacity', 128, (b) => String(b.spec?.storageCapacity ?? false)),
  listColumn('Modes', 160, (b) => b.spec?.volumeLifecycleModes ?? ['Persistent']),
  listColumn('Token requests', 160, (b) => b.spec?.tokenRequests?.map((t) => t.audience), {
    hidden: true,
  }),
  textColumn('Requires republish', 136, (b) => String(b.spec?.requiresRepublish ?? false), {
    hidden: true,
  }),
];

type CSINodeBody = { spec?: { drivers?: { name?: string }[] } };

const csiNodeColumns: KindColumn<CSINodeBody>[] = [
  countColumn('Drivers', 72, (b) => b.spec?.drivers?.length ?? 0),
  listColumn('Driver names', 224, (b) => b.spec?.drivers?.map((d) => d.name), {
    hidden: true,
    mono: true,
  }),
];

// Its fields sit at the top level, with no spec.
type CSIStorageCapacityBody = { storageClassName?: string; capacity?: string; maximumVolumeSize?: string };

const csiStorageCapacityColumns: KindColumn<CSIStorageCapacityBody>[] = [
  textColumn('Storage class', 136, (b) => b.storageClassName),
  quantityColumn('Capacity', 88, (b) => b.capacity),
  quantityColumn('Maximum volume size', 152, (b) => b.maximumVolumeSize, { hidden: true }),
];

type VolumeAttributesClassBody = { driverName?: string; parameters?: Record<string, string> };

const volumeAttributesClassColumns: KindColumn<VolumeAttributesClassBody>[] = [
  textColumn('Driver', 192, (b) => b.driverName, { mono: true }),
  textColumn('Parameters', 224, (b) => labelsText(b.parameters), {
    hidden: true,
    mono: true,
  }),
];

const STORAGE = 'storage.k8s.io/v1';

export const storageKinds = [
  kind('v1', 'persistentvolumes', pvColumns),
  kind('v1', 'persistentvolumeclaims', pvcColumns),
  kind(STORAGE, 'storageclasses', storageClassColumns),
  kind(STORAGE, 'volumeattachments', volumeAttachmentColumns),
  kind(STORAGE, 'csidrivers', csiDriverColumns),
  kind(STORAGE, 'csinodes', csiNodeColumns),
  kind(STORAGE, 'csistoragecapacities', csiStorageCapacityColumns),
  kind(STORAGE, 'volumeattributesclasses', volumeAttributesClassColumns),
];
