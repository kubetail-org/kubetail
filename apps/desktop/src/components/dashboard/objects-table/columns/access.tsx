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

// Access-control kinds' columns, as kubectl's printers draw them.
import { countColumn, durationColumn, kind, listColumn, selectorText, statusColumn, textColumn } from './common';
import type { KindColumn, LabelSelector, Status } from './common';

type ServiceAccountBody = {
  secrets?: unknown[];
  imagePullSecrets?: { name?: string }[];
  automountServiceAccountToken?: boolean;
};

type RoleBody = {
  rules?: { resources?: string[] }[];
  aggregationRule?: { clusterRoleSelectors?: LabelSelector[] };
};

const roleColumns: KindColumn<RoleBody>[] = [
  countColumn('Rules', 72, (b) => b.rules?.length ?? 0),
  // Each resource once, in the order the rules name them.
  listColumn('Resources', 288, (b) => [...new Set((b.rules ?? []).flatMap((r) => r.resources ?? []))], {
    hidden: true,
    mono: true,
  }),
];

type BindingBody = {
  roleRef?: { kind?: string; name?: string };
  subjects?: { kind?: string; name?: string; namespace?: string }[];
};

function subjects(b: BindingBody, of: string): string[] {
  return (b.subjects ?? []).filter((s) => s.kind === of).map((s) => s.name ?? '');
}

// kubectl's wide columns: users, groups, and service accounts as `namespace:name`.
const bindingColumns: KindColumn<BindingBody>[] = [
  textColumn('Role', 224, (b) => b.roleRef && `${b.roleRef.kind}/${b.roleRef.name}`),
  listColumn('Users', 176, (b) => subjects(b, 'User'), { hidden: true }),
  listColumn('Groups', 176, (b) => subjects(b, 'Group'), { hidden: true }),
  listColumn(
    'Service accounts',
    224,
    (b) =>
      (b.subjects ?? []).filter((s) => s.kind === 'ServiceAccount').map((s) => `${s.namespace ?? ''}:${s.name ?? ''}`),
    { hidden: true },
  ),
];

type CSRBody = {
  spec?: { signerName?: string; username?: string; expirationSeconds?: number };
  status?: { conditions?: { type?: string }[]; certificate?: string };
};

// kubectl's `extractCSRStatus`: Approved, Denied or Pending, then Failed and Issued. Only an
// issued certificate is done; a denial or failure never will be.
function csrStatus(b: CSRBody): Status {
  const types = new Set((b.status?.conditions ?? []).map((c) => c.type));
  const denied = types.has('Denied');
  const failed = types.has('Failed');
  const issued = Boolean(b.status?.certificate);
  let label = 'Pending';
  if (denied) label = 'Denied';
  else if (types.has('Approved')) label = 'Approved';
  if (failed) label += ',Failed';
  if (issued) label += ',Issued';
  if (denied || failed) return { label, tone: 'failing' };
  return { label, tone: issued ? 'ok' : 'pending' };
}

export const accessKinds = [
  kind<ServiceAccountBody>('v1', 'serviceaccounts', [
    countColumn('Secrets', 80, (b) => b.secrets?.length ?? 0),
    listColumn('Image pull secrets', 176, (b) => b.imagePullSecrets?.map((s) => s.name), {
      hidden: true,
    }),
    textColumn('Automount token', 128, (b) => b.automountServiceAccountToken?.toString(), {
      hidden: true,
    }),
  ]),
  kind('rbac.authorization.k8s.io/v1', 'roles', roleColumns),
  kind<RoleBody>('rbac.authorization.k8s.io/v1', 'clusterroles', [
    ...roleColumns,
    // An aggregated role's rules are the controller's copy of the roles these select.
    listColumn(
      'Aggregates',
      224,
      (b) => b.aggregationRule?.clusterRoleSelectors?.map((s) => selectorText(s) ?? '<all>'),
      { hidden: true, mono: true },
    ),
  ]),
  kind('rbac.authorization.k8s.io/v1', 'rolebindings', bindingColumns),
  kind('rbac.authorization.k8s.io/v1', 'clusterrolebindings', bindingColumns),
  kind<CSRBody>('certificates.k8s.io/v1', 'certificatesigningrequests', [
    statusColumn('Status', 160, csrStatus),
    textColumn('Signer', 224, (b) => b.spec?.signerName, { mono: true }),
    textColumn('Requestor', 192, (b) => b.spec?.username),
    durationColumn('Requested duration', 136, (b) => b.spec?.expirationSeconds),
  ]),
];
