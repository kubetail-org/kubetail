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

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith, pillTone } from './testutil';

const RBAC = 'rbac.authorization.k8s.io/v1';

function columns(apiVersion: string, resource: string) {
  const all = columnsForKind({ apiVersion, resource });
  const byHeader = (h: string) => all.find((c) => c.header === h)!;
  const text = (h: string, o: ClusterCachedDataObject) => cellText(byHeader(h), o);
  return { all, byHeader, text };
}

const shown = (cs: ReturnType<typeof columns>) => cs.all.filter((c) => !c.hiddenByDefault).map((c) => c.header);
const hidden = (cs: ReturnType<typeof columns>) => cs.all.filter((c) => c.hiddenByDefault).map((c) => c.header);

describe('service account columns', () => {
  const sa = columns('v1', 'serviceaccounts');

  it('counts its secrets, its pull secrets and token mount hidden', () => {
    const o = objectWith({
      secrets: [{}, {}],
      imagePullSecrets: [{ name: 'regcred' }, { name: 'mirror' }],
      automountServiceAccountToken: false,
    });
    expect(shown(sa)).toEqual(['Secrets']);
    expect(hidden(sa)).toEqual(['Image pull secrets', 'Automount token']);
    expect(['Secrets', 'Image pull secrets', 'Automount token'].map((h) => sa.text(h, o))).toEqual([
      '2',
      'regcred, mirror',
      'false',
    ]);
    expect(['Secrets', 'Image pull secrets', 'Automount token'].map((h) => sa.text(h, objectWith({})))).toEqual([
      '0',
      '—',
      '—',
    ]);
  });
});

describe('role columns', () => {
  const rules = [{ resources: ['pods', 'services'] }, { resources: ['pods'] }, {}];

  it('counts rules and lists each resource once', () => {
    const role = columns(RBAC, 'roles');
    expect(shown(role)).toEqual(['Rules']);
    expect(hidden(role)).toEqual(['Resources']);
    const o = objectWith({ rules });
    expect(['Rules', 'Resources'].map((h) => role.text(h, o))).toEqual(['3', 'pods, services']);
    expect(['Rules', 'Resources'].map((h) => role.text(h, objectWith({})))).toEqual(['0', '—']);
  });

  it('names what an aggregated cluster role selects', () => {
    const cr = columns(RBAC, 'clusterroles');
    expect(hidden(cr)).toEqual(['Resources', 'Aggregates']);
    const o = objectWith({
      aggregationRule: { clusterRoleSelectors: [{ matchLabels: { 'rbac.example.com/aggregate': 'true' } }, {}] },
    });
    expect(cr.text('Aggregates', o)).toBe('rbac.example.com/aggregate=true, <all>');
    expect(cr.text('Aggregates', objectWith({ rules }))).toBe('—');
  });
});

describe('binding columns', () => {
  const o = objectWith({
    roleRef: { kind: 'ClusterRole', name: 'view' },
    subjects: [
      { kind: 'User', name: 'jane' },
      { kind: 'Group', name: 'devs' },
      { kind: 'ServiceAccount', name: 'api', namespace: 'shop' },
      { kind: 'ServiceAccount' },
      { kind: 'User' },
    ],
  });

  it('names the role, its subjects hidden as kubectl’s wide columns', () => {
    for (const resource of ['rolebindings', 'clusterrolebindings']) {
      const rb = columns(RBAC, resource);
      expect(shown(rb)).toEqual(['Role']);
      expect(hidden(rb)).toEqual(['Users', 'Groups', 'Service accounts']);
      expect(['Role', 'Users', 'Groups', 'Service accounts'].map((h) => rb.text(h, o))).toEqual([
        'ClusterRole/view',
        'jane',
        'devs',
        'shop:api, :',
      ]);
    }
  });

  it('dashes a binding with no role or subjects', () => {
    const rb = columns(RBAC, 'rolebindings');
    expect(['Role', 'Users', 'Service accounts'].map((h) => rb.text(h, objectWith({})))).toEqual(['—', '—', '—']);
  });
});

describe('certificate signing request columns', () => {
  const GVR = { apiVersion: 'certificates.k8s.io/v1', resource: 'certificatesigningrequests' };
  const csr = columns(GVR.apiVersion, GVR.resource);
  const tone = toneForKind(GVR)!;
  const status = csr.byHeader('Status');
  const withConditions = (types: string[], certificate?: string) =>
    objectWith({ status: { conditions: types.map((type) => ({ type })), certificate } });

  it('shows status, signer, requestor and requested duration', () => {
    expect(shown(csr)).toEqual(['Status', 'Signer', 'Requestor', 'Requested duration']);
    const o = objectWith({
      spec: { signerName: 'kubernetes.io/kube-apiserver-client', username: 'jane', expirationSeconds: 86_400 },
    });
    expect(['Signer', 'Requestor', 'Requested duration'].map((h) => csr.text(h, o))).toEqual([
      'kubernetes.io/kube-apiserver-client',
      'jane',
      '24h',
    ]);
    expect(csr.byHeader('Requested duration').sortValue?.(o)).toBe(86_400);
  });

  it('prints a duration as kubectl does', () => {
    const d = (expirationSeconds?: number) =>
      csr.text('Requested duration', objectWith({ spec: { expirationSeconds } }));
    expect([d(7200), d(600), d(90), d()]).toEqual(['120m', '10m', '90s', '—']);
  });

  it('reads an issued certificate as done', () => {
    const o = withConditions(['Approved'], 'LS0t');
    expect(cellText(status, o)).toBe('Approved,Issued');
    expect([pillTone(status, o), tone(o)]).toEqual(['ok', 'ok']);
  });

  it('reads one awaiting approval or issue as pending', () => {
    expect([withConditions([]), withConditions(['Approved'])].map((o) => [cellText(status, o), tone(o)])).toEqual([
      ['Pending', 'pending'],
      ['Approved', 'pending'],
    ]);
    expect(tone(objectWith({}))).toBe('pending');
  });

  it('reads a denied or failed request as failing', () => {
    expect(
      [withConditions(['Denied']), withConditions(['Approved', 'Failed'])].map((o) => [cellText(status, o), tone(o)]),
    ).toEqual([
      ['Denied', 'failing'],
      ['Approved,Failed', 'failing'],
    ]);
  });
});
