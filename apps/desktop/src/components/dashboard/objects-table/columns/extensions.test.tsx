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

const headers = (columns: ObjectColumn[], hidden: boolean) =>
  columns.filter((c) => Boolean(c.hiddenByDefault) === hidden).map((c) => c.header);
const texts = (columns: ObjectColumn[], rawJSON: unknown) => columns.map((c) => cellText(c, objectWith(rawJSON)));
const admission = (resource: string) => ({ apiVersion: 'admissionregistration.k8s.io/v1', resource });

describe('custom resource definitions', () => {
  const gvr = { apiVersion: 'apiextensions.k8s.io/v1', resource: 'customresourcedefinitions' };
  const columns = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;
  const conditions = (...c: [string, string][]) => ({
    status: { conditions: c.map(([type, status]) => ({ type, status })) },
  });

  it('reads the group, kind, scope and versions, the names hidden', () => {
    expect(headers(columns, false)).toEqual(['Status', 'Group', 'Kind', 'Scope', 'Versions']);
    expect(headers(columns, true)).toEqual(['Plural', 'Short names']);
    const crd = {
      spec: {
        group: 'example.com',
        scope: 'Namespaced',
        names: { kind: 'Widget', plural: 'widgets', shortNames: ['wd'] },
        versions: [{ name: 'v1' }, { name: 'v2' }],
      },
      ...conditions(['Established', 'True']),
    };
    expect(texts(columns, crd)).toEqual([
      'Established',
      'example.com',
      'Widget',
      'Namespaced',
      'v1, v2',
      'widgets',
      'wd',
    ]);
  });

  it('reads rejected names as failing, terminating or unestablished as pending', () => {
    const cases = [
      conditions(['NamesAccepted', 'False'], ['Established', 'True']),
      conditions(['Established', 'True']),
      { metadata: { deletionTimestamp: '2026-01-01T00:00:00Z' }, ...conditions(['Established', 'True']) },
      conditions(['Established', 'False']),
    ];
    expect(cases.map((c) => tone(objectWith(c)))).toEqual(['failing', 'ok', 'pending', 'pending']);
    expect(cases.map((c) => cellText(columns[0], objectWith(c)))).toEqual([
      'Names not accepted',
      'Established',
      'Terminating',
      'Pending',
    ]);
  });
});

describe('API services', () => {
  const gvr = { apiVersion: 'apiregistration.k8s.io/v1', resource: 'apiservices' };
  const columns = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;
  const available = (status: string, reason?: string) => ({
    status: { conditions: [{ type: 'Available', status, reason }] },
  });

  it('names its service, or Local when the API server serves it', () => {
    expect(headers(columns, false)).toEqual(['Available', 'Service']);
    expect(headers(columns, true)).toEqual(['Group', 'Version']);
    const remote = {
      spec: { group: 'metrics.k8s.io', version: 'v1beta1', service: { namespace: 'kube-system', name: 'metrics' } },
    };
    expect(texts(columns, { ...remote, ...available('True') })).toEqual([
      'True',
      'kube-system/metrics',
      'metrics.k8s.io',
      'v1beta1',
    ]);
    expect(cellText(columns[1], objectWith({}))).toBe('Local');
    expect(cellText(columns[1], objectWith({ spec: { service: { name: 'm' } } }))).toBe('m');
    expect(cellText(columns[1], objectWith({ spec: { service: { namespace: 'ns' } } }))).toBe('ns/');
  });

  it('reads the Available condition, with its reason when not True', () => {
    const cases = [available('True'), available('False', 'MissingEndpoints'), available('Unknown'), {}];
    expect(cases.map((c) => cellText(columns[0], objectWith(c)))).toEqual([
      'True',
      'False (MissingEndpoints)',
      'Unknown',
      '—',
    ]);
    expect(cases.map((c) => tone(objectWith(c)))).toEqual(['ok', 'failing', 'pending', 'pending']);
    expect(pillTone(columns[0], objectWith(available('False')))).toBe('failing');
  });
});

describe('webhook configurations', () => {
  it('counts the webhooks, their names and policies hidden', () => {
    for (const resource of ['mutatingwebhookconfigurations', 'validatingwebhookconfigurations']) {
      const columns = columnsForKind(admission(resource));
      expect(headers(columns, true)).toEqual(['Webhook names', 'Failure policies']);
      const config = {
        webhooks: [
          { name: 'a.example.com', failurePolicy: 'Fail' },
          { name: 'b.example.com', failurePolicy: 'Ignore' },
        ],
      };
      expect(texts(columns, config)).toEqual(['2', 'a.example.com, b.example.com', 'Fail, Ignore']);
      expect(texts(columns, {})).toEqual(['0', '—', '—']);
    }
  });
});

describe('validating admission policies', () => {
  const columns = columnsForKind(admission('validatingadmissionpolicies'));

  it('counts validations and names the param kind', () => {
    expect(headers(columns, true)).toEqual(['Failure policy']);
    const policy = {
      spec: { validations: [{}, {}], paramKind: { apiVersion: 'v1', kind: 'ConfigMap' }, failurePolicy: 'Fail' },
    };
    expect(texts(columns, policy)).toEqual(['2', 'v1/ConfigMap', 'Fail']);
    expect(texts(columns, { spec: { paramKind: { kind: 'ConfigMap' } } })).toEqual(['0', 'ConfigMap', '—']);
  });
});

describe('validating admission policy bindings', () => {
  const columns = columnsForKind(admission('validatingadmissionpolicybindings'));

  it('names the policy and param ref, the actions hidden', () => {
    expect(headers(columns, true)).toEqual(['Validation actions']);
    const binding = {
      spec: { policyName: 'p', paramRef: { namespace: 'ns', name: 'cfg' }, validationActions: ['Deny', 'Audit'] },
    };
    expect(texts(columns, binding)).toEqual(['p', 'ns/cfg', 'Deny, Audit']);
    expect(texts(columns, { spec: { paramRef: { name: 'cfg' } } })).toEqual(['—', 'cfg', '—']);
    expect(texts(columns, { spec: { paramRef: { selector: {} } } })).toEqual(['—', '—', '—']);
  });
});
