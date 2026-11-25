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

import { describe, expect, it } from 'vitest';

import type { ObjectColumn } from './common';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith, pillTone } from './testutil';

const core = (resource: string) => ({ apiVersion: 'v1', resource });
const storage = (resource: string) => ({ apiVersion: 'storage.k8s.io/v1', resource });
const headers = (columns: ObjectColumn[], hidden: boolean) =>
  columns.filter((c) => Boolean(c.hiddenByDefault) === hidden).map((c) => c.header);
const texts = (columns: ObjectColumn[], rawJSON: unknown) => columns.map((c) => cellText(c, objectWith(rawJSON)));

describe('persistent volumes', () => {
  const columns = columnsForKind(core('persistentvolumes'));
  const tone = toneForKind(core('persistentvolumes'))!;
  const pv = {
    spec: {
      capacity: { storage: '10Gi' },
      accessModes: ['ReadWriteOnce', 'ReadWriteOncePod', 'Custom'],
      persistentVolumeReclaimPolicy: 'Retain',
      claimRef: { namespace: 'shop', name: 'data' },
      storageClassName: 'fast',
      volumeMode: 'Filesystem',
    },
    status: { phase: 'Bound', reason: 'Ok' },
  };

  it('draws kubectl’s columns, the reason and mode hidden', () => {
    expect(headers(columns, false)).toEqual([
      'Status',
      'Capacity',
      'Access modes',
      'Reclaim policy',
      'Claim',
      'Storage class',
    ]);
    expect(headers(columns, true)).toEqual(['Reason', 'Volume mode']);
    expect(texts(columns, pv)).toEqual([
      'Bound',
      '10Gi',
      'RWO, RWOP, Custom',
      'Retain',
      'shop/data',
      'fast',
      'Ok',
      'Filesystem',
    ]);
  });

  it('sorts capacity by size, largest first', () => {
    const capacity = columns[1];
    expect(capacity.sortValue?.(objectWith(pv))).toBe(10 * 2 ** 30);
    expect(capacity.descFirst).toBe(true);
  });

  it('names a claim without a namespace by its name', () => {
    expect(cellText(columns[4], objectWith({ spec: { claimRef: { name: 'data' } } }))).toBe('data');
  });

  it('reads one being deleted as terminating, whatever its phase', () => {
    const o = objectWith({ metadata: { deletionTimestamp: '2026-01-01T00:00:00Z' }, status: { phase: 'Bound' } });
    expect(cellText(columns[0], o)).toBe('Terminating');
    expect(tone(o)).toBe('pending');
  });

  it('tones each phase', () => {
    const phases = ['Bound', 'Available', 'Released', 'Pending', 'Failed', 'Odd'];
    expect(phases.map((phase) => tone(objectWith({ status: { phase } })))).toEqual([
      'ok',
      'ok',
      'pending',
      'pending',
      'failing',
      'pending',
    ]);
    expect(pillTone(columns[0], objectWith({ status: { phase: 'Failed' } }))).toBe('failing');
  });

  it('dashes an empty body', () => {
    expect(texts(columns, {})).toEqual(columns.map(() => '—'));
    expect(tone(objectWith({}))).toBe('pending');
  });
});

describe('persistent volume claims', () => {
  const columns = columnsForKind(core('persistentvolumeclaims'));
  const tone = toneForKind(core('persistentvolumeclaims'))!;

  it('draws kubectl’s columns, the request and mode hidden', () => {
    expect(headers(columns, false)).toEqual(['Status', 'Volume', 'Capacity', 'Access modes', 'Storage class']);
    expect(headers(columns, true)).toEqual(['Requested', 'Volume mode']);
    const pvc = {
      spec: {
        volumeName: 'pvc-1',
        storageClassName: 'fast',
        volumeMode: 'Block',
        resources: { requests: { storage: '5Gi' } },
      },
      status: { phase: 'Bound', capacity: { storage: '8Gi' }, accessModes: ['ReadWriteMany', 'ReadOnlyMany'] },
    };
    expect(texts(columns, pvc)).toEqual(['Bound', 'pvc-1', '8Gi', 'RWX, ROX', 'fast', '5Gi', 'Block']);
    expect(columns[5].sortValue?.(objectWith(pvc))).toBe(5 * 2 ** 30);
  });

  it('reads one being deleted as terminating, whatever its phase', () => {
    const o = objectWith({ metadata: { deletionTimestamp: '2026-01-01T00:00:00Z' }, status: { phase: 'Bound' } });
    expect(cellText(columns[0], o)).toBe('Terminating');
    expect(tone(o)).toBe('pending');
  });

  it('tones each phase', () => {
    expect(['Bound', 'Pending', 'Lost'].map((phase) => tone(objectWith({ status: { phase } })))).toEqual([
      'ok',
      'pending',
      'failing',
    ]);
  });
});

describe('storage classes', () => {
  const columns = columnsForKind(storage('storageclasses'));

  it('marks the default and reads each field', () => {
    expect(headers(columns, false)).toEqual(['Default', 'Provisioner', 'Reclaim policy', 'Binding mode', 'Expansion']);
    const sc = {
      metadata: { annotations: { 'storageclass.kubernetes.io/is-default-class': 'true' } },
      provisioner: 'ebs.csi.aws.com',
      reclaimPolicy: 'Retain',
      volumeBindingMode: 'WaitForFirstConsumer',
      allowVolumeExpansion: true,
    };
    expect(texts(columns, sc)).toEqual(['Yes', 'ebs.csi.aws.com', 'Retain', 'WaitForFirstConsumer', 'true']);
  });

  it('reads the beta annotation, and unset fields as the API’s defaults', () => {
    const beta = { metadata: { annotations: { 'storageclass.beta.kubernetes.io/is-default-class': 'true' } } };
    expect(texts(columns, beta)).toEqual(['Yes', '—', 'Delete', 'Immediate', 'false']);
    expect(cellText(columns[0], objectWith({}))).toBe('—');
  });
});

describe('volume attachments', () => {
  const columns = columnsForKind(storage('volumeattachments'));
  const tone = toneForKind(storage('volumeattachments'))!;
  const spec = { attacher: 'csi.x', nodeName: 'node-1', source: { persistentVolumeName: 'pv-1' } };

  it('reads the attacher, volume and node, the error hidden', () => {
    expect(headers(columns, false)).toEqual(['Status', 'Attacher', 'PV', 'Node']);
    expect(headers(columns, true)).toEqual(['Error']);
    expect(texts(columns, { spec, status: { attached: true } })).toEqual(['Attached', 'csi.x', 'pv-1', 'node-1', '—']);
  });

  it('reads either error as failing, else attached or on its way', () => {
    const cases = [
      { attachError: { message: 'boom' } },
      { attached: true, detachError: { message: 'stuck' } },
      { attached: true },
      {},
    ];
    expect(cases.map((status) => tone(objectWith({ status })))).toEqual(['failing', 'failing', 'ok', 'pending']);
    expect(cases.map((status) => cellText(columns[0], objectWith({ status })))).toEqual([
      'Attach failed',
      'Detach failed',
      'Attached',
      'Attaching',
    ]);
    expect(cases.slice(0, 2).map((status) => cellText(columns[4], objectWith({ status })))).toEqual(['boom', 'stuck']);
  });
});

describe('CSI drivers', () => {
  const columns = columnsForKind(storage('csidrivers'));

  it('reads each field, unset ones as the API’s defaults', () => {
    expect(headers(columns, false)).toEqual(['Attach required', 'Pod info on mount', 'Storage capacity', 'Modes']);
    expect(headers(columns, true)).toEqual(['Token requests', 'Requires republish']);
    expect(texts(columns, {})).toEqual(['true', 'false', 'false', 'Persistent', '—', 'false']);
    const driver = {
      spec: {
        attachRequired: false,
        podInfoOnMount: true,
        storageCapacity: true,
        requiresRepublish: true,
        tokenRequests: [{ audience: 'vault' }],
        volumeLifecycleModes: ['Persistent', 'Ephemeral'],
      },
    };
    expect(texts(columns, driver)).toEqual(['false', 'true', 'true', 'Persistent, Ephemeral', 'vault', 'true']);
  });
});

describe('CSI nodes', () => {
  const columns = columnsForKind(storage('csinodes'));

  it('counts its drivers, their names hidden', () => {
    expect(headers(columns, true)).toEqual(['Driver names']);
    expect(texts(columns, { spec: { drivers: [{ name: 'a' }, { name: 'b' }] } })).toEqual(['2', 'a, b']);
    expect(texts(columns, {})).toEqual(['0', '—']);
  });
});

describe('CSI storage capacities', () => {
  const columns = columnsForKind(storage('csistoragecapacities'));

  it('reads its top-level fields', () => {
    expect(headers(columns, true)).toEqual(['Maximum volume size']);
    expect(texts(columns, { storageClassName: 'fast', capacity: '100Gi', maximumVolumeSize: '10Gi' })).toEqual([
      'fast',
      '100Gi',
      '10Gi',
    ]);
  });
});

describe('volume attributes classes', () => {
  it('draws its driver, and its parameters hidden', () => {
    const columns = columnsForKind(storage('volumeattributesclasses'));
    expect(headers(columns, true)).toEqual(['Parameters']);
    expect(texts(columns, { driverName: 'pd.csi.storage.gke.io', parameters: { iops: '3000' } })).toEqual([
      'pd.csi.storage.gke.io',
      'iops=3000',
    ]);
    expect(texts(columns, {})).toEqual(['—', '—']);
  });
});
