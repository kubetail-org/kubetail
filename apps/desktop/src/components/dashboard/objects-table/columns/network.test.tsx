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
import { cellText, objectWith } from './testutil';

function columns(apiVersion: string, resource: string) {
  const all = columnsForKind({ apiVersion, resource });
  const byHeader = (h: string) => all.find((c) => c.header === h)!;
  const text = (h: string, o: ClusterCachedDataObject) => cellText(byHeader(h), o);
  return { all, byHeader, text };
}

const shown = (cs: ReturnType<typeof columns>) => cs.all.filter((c) => !c.hiddenByDefault).map((c) => c.header);
const hidden = (cs: ReturnType<typeof columns>) => cs.all.filter((c) => c.hiddenByDefault).map((c) => c.header);

describe('service columns', () => {
  const svc = columns('v1', 'services');
  const of = (spec: Record<string, unknown>, status?: unknown) => objectWith({ spec, status });

  it('shows kubectl’s columns, the selector hidden', () => {
    expect(shown(svc)).toEqual(['Type', 'Cluster IP', 'External IP', 'Ports']);
    expect(hidden(svc)).toEqual(['Selector']);
  });

  it('reads a cluster IP service', () => {
    const o = of({
      clusterIPs: ['10.0.0.1'],
      selector: { app: 'web', tier: 'front' },
      ports: [{ port: 80, protocol: 'UDP' }, { port: 443 }],
    });
    expect(['Type', 'Cluster IP', 'External IP', 'Ports', 'Selector'].map((h) => svc.text(h, o))).toEqual([
      'ClusterIP',
      '10.0.0.1',
      '—',
      '80/UDP,443/TCP',
      'app=web,tier=front',
    ]);
    expect(svc.text('Cluster IP', of({ clusterIP: 'None' }))).toBe('None');
  });

  it('names a node port and an external IP', () => {
    const o = of({ type: 'NodePort', externalIPs: ['1.2.3.4', '5.6.7.8'], ports: [{ port: 80, nodePort: 30080 }] });
    expect(svc.text('External IP', o)).toBe('1.2.3.4,5.6.7.8');
    expect(svc.text('Ports', o)).toBe('80:30080/TCP');
  });

  it('reads a load balancer without an address as pending', () => {
    expect(svc.text('External IP', of({ type: 'LoadBalancer' }))).toBe('<pending>');
    const ready = of(
      { type: 'LoadBalancer', externalIPs: ['9.9.9.9'] },
      { loadBalancer: { ingress: [{ ip: '1.1.1.1' }, { hostname: 'lb.example.com' }, {}] } },
    );
    expect(svc.text('External IP', ready)).toBe('1.1.1.1,lb.example.com,9.9.9.9');
  });

  it('reads an external name, and an unknown type', () => {
    expect(svc.text('External IP', of({ type: 'ExternalName', externalName: 'db.example.com' }))).toBe(
      'db.example.com',
    );
    expect(svc.text('External IP', of({ type: 'Weird' }))).toBe('<unknown>');
  });

  it('dashes a body with no spec', () => {
    const o = objectWith({});
    expect(['External IP', 'Ports', 'Selector'].map((h) => svc.text(h, o))).toEqual(['—', '—', '—']);
  });
});

describe('endpoints columns', () => {
  const ep = columns('v1', 'endpoints');

  it('lists each address with each port, cut after three', () => {
    const o = objectWith({
      subsets: [
        { addresses: [{ ip: '10.0.0.1' }, { ip: '10.0.0.2' }], ports: [{ port: 80 }, { port: 443 }] },
        { addresses: [{ ip: '10.0.0.3' }] },
        { ports: [{ port: 8080 }] },
      ],
    });
    expect(shown(ep)).toEqual(['Endpoints']);
    expect(ep.text('Endpoints', o)).toBe('10.0.0.1:80,10.0.0.2:80,10.0.0.1:443 + 2 more...');
  });

  it('brackets an IPv6 address before its port, and leaves a bare one alone', () => {
    const subset = (ports?: object[]) => objectWith({ subsets: [{ addresses: [{ ip: '2001:db8::1' }], ports }] });
    expect(ep.text('Endpoints', subset([{ port: 443 }]))).toBe('[2001:db8::1]:443');
    expect(ep.text('Endpoints', subset())).toBe('2001:db8::1');
  });

  it('lists a short list whole, and dashes none', () => {
    expect(ep.text('Endpoints', objectWith({ subsets: [{ addresses: [{ ip: '10.0.0.1' }] }] }))).toBe('10.0.0.1');
    expect(ep.text('Endpoints', objectWith({ subsets: [{ addresses: [{}], ports: [{ port: 80 }] }] }))).toBe(':80');
    expect(ep.text('Endpoints', objectWith({}))).toBe('—');
  });
});

describe('endpoint slice columns', () => {
  const es = columns('discovery.k8s.io/v1', 'endpointslices');

  it('reads the address type, ports, endpoints and how many are ready', () => {
    const o = objectWith({
      addressType: 'IPv4',
      ports: [{ port: 80 }, {}],
      endpoints: [
        { addresses: ['10.0.0.1'], conditions: { ready: true } },
        { addresses: ['10.0.0.2'], conditions: { ready: false } },
        { addresses: ['10.0.0.3'] },
        {},
      ],
    });
    expect(shown(es)).toEqual(['Address type', 'Ports', 'Endpoints']);
    expect(hidden(es)).toEqual(['Ready']);
    expect(['Address type', 'Ports', 'Endpoints', 'Ready'].map((h) => es.text(h, o))).toEqual([
      'IPv4',
      '80, <unset>',
      '10.0.0.1,10.0.0.2,10.0.0.3',
      '3/4',
    ]);
  });

  it('dashes a slice with nothing in it', () => {
    const o = objectWith({});
    expect(['Address type', 'Ports', 'Endpoints', 'Ready'].map((h) => es.text(h, o))).toEqual(['—', '—', '—', '0/0']);
  });
});

describe('ingress columns', () => {
  const ing = columns('networking.k8s.io/v1', 'ingresses');

  it('reads the class, hosts, address and ports', () => {
    const o = objectWith({
      spec: { ingressClassName: 'nginx', rules: [{ host: 'a.example.com' }, {}], tls: [{}] },
      status: { loadBalancer: { ingress: [{ ip: '1.1.1.1' }] } },
    });
    expect(shown(ing)).toEqual(['Class', 'Hosts', 'Address', 'Ports']);
    expect(hidden(ing)).toEqual(['Rules']);
    expect(['Class', 'Hosts', 'Address', 'Ports', 'Rules'].map((h) => ing.text(h, o))).toEqual([
      'nginx',
      'a.example.com',
      '1.1.1.1',
      '80, 443',
      '2',
    ]);
  });

  it('reads rules naming no host as any, and cuts after three', () => {
    expect(ing.text('Hosts', objectWith({ spec: { rules: [{}] } }))).toBe('*');
    expect(ing.text('Hosts', objectWith({}))).toBe('*');
    const many = objectWith({ spec: { rules: ['a', 'b', 'c', 'd', 'e'].map((host) => ({ host })) } });
    expect(ing.text('Hosts', many)).toBe('a,b,c + 2 more...');
  });

  it('serves plain HTTP without TLS', () => {
    const o = objectWith({});
    expect(['Class', 'Address', 'Ports'].map((h) => ing.text(h, o))).toEqual(['—', '—', '80']);
  });
});

describe('ingress class columns', () => {
  const ic = columns('networking.k8s.io/v1', 'ingressclasses');

  it('reads the controller, parameters and whether it is the default', () => {
    const o = objectWith({
      metadata: { annotations: { 'ingressclass.kubernetes.io/is-default-class': 'true' } },
      spec: { controller: 'k8s.io/ingress-nginx', parameters: { kind: 'IngressParameters', name: 'external' } },
    });
    expect(shown(ic)).toEqual(['Controller', 'Parameters']);
    expect(hidden(ic)).toEqual(['Default']);
    expect(['Controller', 'Parameters', 'Default'].map((h) => ic.text(h, o))).toEqual([
      'k8s.io/ingress-nginx',
      'IngressParameters/external',
      'Yes',
    ]);
    expect(['Parameters', 'Default'].map((h) => ic.text(h, objectWith({})))).toEqual(['—', '—']);
  });
});

describe('network policy columns', () => {
  const np = columns('networking.k8s.io/v1', 'networkpolicies');

  it('reads the pod selector, an empty one as every pod', () => {
    const o = objectWith({
      spec: { podSelector: { matchLabels: { app: 'web' } }, policyTypes: ['Ingress', 'Egress'] },
    });
    expect(shown(np)).toEqual(['Pod selector']);
    expect(hidden(np)).toEqual(['Policy types']);
    expect(['Pod selector', 'Policy types'].map((h) => np.text(h, o))).toEqual(['app=web', 'Ingress, Egress']);
    expect(np.text('Pod selector', objectWith({ spec: { podSelector: {} } }))).toBe('<all>');
  });
});

describe('ip addresses', () => {
  const [parent] = columnsForKind({ apiVersion: 'networking.k8s.io/v1', resource: 'ipaddresses' });
  const at = (parentRef: unknown) => cellText(parent, objectWith({ spec: { parentRef } }));

  it('names its parent as kubectl does', () => {
    expect(at({ resource: 'services', namespace: 'default', name: 'kubernetes' })).toBe('services/default/kubernetes');
    expect(at({ group: 'example.com', resource: 'gateways', name: 'edge' })).toBe('gateways.example.com/edge');
    expect(at({ resource: 'services' })).toBe('—');
    expect(at(undefined)).toBe('—');
  });
});

describe('service CIDRs', () => {
  const gvr = { apiVersion: 'networking.k8s.io/v1', resource: 'servicecidrs' };
  const [status, cidrs] = columnsForKind(gvr);
  const tone = toneForKind(gvr)!;
  const withReady = (ready?: string, deleting = false) =>
    objectWith({
      metadata: deleting ? { deletionTimestamp: 'now' } : {},
      spec: { cidrs: ['10.96.0.0/16', 'fd00::/108'] },
      status: { conditions: ready ? [{ type: 'Ready', status: ready }] : [] },
    });

  it('draws its ranges', () => {
    expect(cellText(cidrs, withReady('True'))).toBe('10.96.0.0/16, fd00::/108');
  });

  it('reads its Ready condition', () => {
    const cases = [withReady('True'), withReady('False'), withReady(), withReady('True', true)];
    expect(cases.map((o) => [cellText(status, o), tone(o)])).toEqual([
      ['Ready', 'ok'],
      ['Not ready', 'failing'],
      ['Pending', 'pending'],
      ['Terminating', 'pending'],
    ]);
  });
});
