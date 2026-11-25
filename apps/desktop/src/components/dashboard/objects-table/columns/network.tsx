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

// Network kinds' columns, as kubectl's printers draw them.
import {
  conditionStatus,
  countColumn,
  kind,
  labelsText,
  listColumn,
  readyColumn,
  selectorText,
  statusColumn,
  textColumn,
} from './common';
import type { LabelSelector, Status } from './common';

type LoadBalancerStatus = { ingress?: { ip?: string; hostname?: string }[] };

// The first three, then how many more, as kubectl cuts a long list.
function capped(list: readonly string[]): string | undefined {
  if (list.length === 0) return undefined;
  const shown = list.slice(0, 3).join(',');
  return list.length > 3 ? `${shown} + ${list.length - 3} more...` : shown;
}

function lbAddresses(s: LoadBalancerStatus | undefined): string[] {
  return (s?.ingress ?? []).flatMap((i) => i.ip || i.hostname || []);
}

type ServiceBody = {
  spec?: {
    type?: string;
    clusterIP?: string;
    clusterIPs?: string[];
    externalIPs?: string[];
    externalName?: string;
    selector?: Record<string, string>;
    ports?: { port?: number; nodePort?: number; protocol?: string }[];
  };
  status?: { loadBalancer?: LoadBalancerStatus };
};

// kubectl's `getServiceExternalIP`: a load balancer without an address yet is pending.
function serviceExternalIP(b: ServiceBody): string | undefined {
  const spec = b.spec ?? {};
  const external = spec.externalIPs ?? [];
  switch (spec.type ?? 'ClusterIP') {
    case 'ClusterIP':
    case 'NodePort':
      return external.length ? external.join(',') : undefined;
    case 'LoadBalancer': {
      const all = [...lbAddresses(b.status?.loadBalancer), ...external];
      return all.length ? all.join(',') : '<pending>';
    }
    case 'ExternalName':
      return spec.externalName;
    default:
      return '<unknown>';
  }
}

// `80:30080/TCP`, the node port only where there is one.
function servicePorts(b: ServiceBody): string | undefined {
  const ports = (b.spec?.ports ?? []).map(
    (p) => `${p.port}${p.nodePort ? `:${p.nodePort}` : ''}/${p.protocol ?? 'TCP'}`,
  );
  return ports.length ? ports.join(',') : undefined;
}

type EndpointsBody = {
  subsets?: { addresses?: { ip?: string }[]; ports?: { port?: number }[] }[];
};

// An IPv6 address goes in brackets, or its last group would read as the port.
const hostPort = (ip: string, port: number | undefined) => `${ip.includes(':') ? `[${ip}]` : ip}:${port}`;

// Every address with each port, `ip:port`, or the bare address where no port is named.
function endpointAddresses(b: EndpointsBody): string[] {
  return (b.subsets ?? []).flatMap((s) => {
    const ips = (s.addresses ?? []).map((a) => a.ip ?? '');
    const ports = s.ports ?? [];
    return ports.length ? ports.flatMap((p) => ips.map((ip) => hostPort(ip, p.port))) : ips;
  });
}

type EndpointSliceBody = {
  addressType?: string;
  ports?: { port?: number }[];
  endpoints?: { addresses?: string[]; conditions?: { ready?: boolean } }[];
};

type IngressBody = {
  spec?: { ingressClassName?: string; rules?: { host?: string }[]; tls?: unknown[] };
  status?: { loadBalancer?: LoadBalancerStatus };
};

// kubectl's `formatHosts`: the named hosts, three at most; rules naming none read as `*`.
function ingressHosts(b: IngressBody): string {
  const rules = b.spec?.rules ?? [];
  const hosts = rules
    .slice(0, 3)
    .map((r) => r.host)
    .filter(Boolean);
  if (hosts.length === 0) return '*';
  return rules.length > 3 ? `${hosts.join(',')} + ${rules.length - 3} more...` : hosts.join(',');
}

type IngressClassBody = {
  metadata?: { annotations?: Record<string, string> };
  spec?: { controller?: string; parameters?: { kind?: string; name?: string } };
};

type NetworkPolicyBody = { spec?: { podSelector?: LabelSelector; policyTypes?: string[] } };

type IPAddressBody = {
  spec?: { parentRef?: { group?: string; resource?: string; namespace?: string; name?: string } };
};

type ServiceCIDRBody = {
  metadata?: { deletionTimestamp?: string };
  spec?: { cidrs?: string[] };
  status?: { conditions?: { type?: string; status?: string }[] };
};

// The Ready condition, which the allocator sets once it serves the range.
function serviceCIDRStatus(b: ServiceCIDRBody): Status {
  if (b.metadata?.deletionTimestamp) return { label: 'Terminating', tone: 'pending' };
  const ready = conditionStatus(b.status?.conditions, 'Ready');
  if (ready === 'True') return { label: 'Ready', tone: 'ok' };
  if (ready === 'False') return { label: 'Not ready', tone: 'failing' };
  return { label: 'Pending', tone: 'pending' };
}

export const networkKinds = [
  kind<ServiceBody>('v1', 'services', [
    textColumn('Type', 112, (b) => b.spec?.type ?? 'ClusterIP'),
    textColumn('Cluster IP', 120, (b) => b.spec?.clusterIPs?.[0] ?? b.spec?.clusterIP, { mono: true }),
    textColumn('External IP', 136, serviceExternalIP, { mono: true }),
    textColumn('Ports', 160, servicePorts, { mono: true }),
    textColumn('Selector', 224, (b) => labelsText(b.spec?.selector), { hidden: true, mono: true }),
  ]),
  kind<EndpointsBody>('v1', 'endpoints', [
    textColumn('Endpoints', 288, (b) => capped(endpointAddresses(b)), { mono: true }),
  ]),
  kind<EndpointSliceBody>('discovery.k8s.io/v1', 'endpointslices', [
    textColumn('Address type', 112, (b) => b.addressType),
    // A port with no number is `<unset>`, as kubectl prints it.
    listColumn('Ports', 128, (b) => b.ports?.map((p) => (p.port === undefined ? '<unset>' : String(p.port))), {
      mono: true,
    }),
    textColumn('Endpoints', 224, (b) => capped((b.endpoints ?? []).flatMap((e) => e.addresses ?? [])), {
      mono: true,
    }),
    // An endpoint whose readiness is unknown counts as ready, as the API defines it.
    readyColumn(
      'Ready',
      (b) => {
        const all = b.endpoints ?? [];
        return { ready: all.filter((e) => e.conditions?.ready !== false).length, total: all.length };
      },
      { hidden: true },
    ),
  ]),
  kind<IngressBody>('networking.k8s.io/v1', 'ingresses', [
    textColumn('Class', 120, (b) => b.spec?.ingressClassName),
    textColumn('Hosts', 224, ingressHosts, { mono: true }),
    listColumn('Address', 144, (b) => lbAddresses(b.status?.loadBalancer), { mono: true }),
    textColumn('Ports', 80, (b) => (b.spec?.tls?.length ? '80, 443' : '80')),
    countColumn('Rules', 72, (b) => b.spec?.rules?.length, { hidden: true }),
  ]),
  kind<IngressClassBody>('networking.k8s.io/v1', 'ingressclasses', [
    textColumn('Controller', 224, (b) => b.spec?.controller, { mono: true }),
    textColumn('Parameters', 176, (b) => {
      const p = b.spec?.parameters;
      return p && `${p.kind}/${p.name}`;
    }),
    textColumn(
      'Default',
      80,
      (b) => (b.metadata?.annotations?.['ingressclass.kubernetes.io/is-default-class'] === 'true' ? 'Yes' : undefined),
      { hidden: true },
    ),
  ]),
  kind<NetworkPolicyBody>('networking.k8s.io/v1', 'networkpolicies', [
    // An empty selector picks every pod in the namespace.
    textColumn('Pod selector', 224, (b) => selectorText(b.spec?.podSelector) ?? '<all>', {
      mono: true,
    }),
    listColumn('Policy types', 144, (b) => b.spec?.policyTypes, { hidden: true }),
  ]),
  kind<IPAddressBody>('networking.k8s.io/v1', 'ipaddresses', [
    // kubectl's ParentRef: `resource[.group]/[namespace/]name`.
    textColumn(
      'Parent',
      224,
      (b) => {
        const p = b.spec?.parentRef;
        if (!p?.resource || !p.name) return undefined;
        const resource = p.group ? `${p.resource}.${p.group}` : p.resource;
        return [resource, p.namespace, p.name].filter(Boolean).join('/');
      },
      { mono: true },
    ),
  ]),
  kind<ServiceCIDRBody>('networking.k8s.io/v1', 'servicecidrs', [
    statusColumn('Status', 112, serviceCIDRStatus),
    listColumn('CIDRs', 224, (b) => b.spec?.cidrs, { mono: true }),
  ]),
];
