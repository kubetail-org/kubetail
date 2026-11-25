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

// Columns of the kinds that extend the API: definitions, aggregated servers and admission.
import { conditionStatus, countColumn, kind, listColumn, statusColumn, textColumn } from './common';
import type { KindColumn, Status } from './common';

type Condition = { type?: string; status?: string; reason?: string };

type CRDBody = {
  metadata?: { deletionTimestamp?: string };
  spec?: {
    group?: string;
    scope?: string;
    names?: { kind?: string; plural?: string; shortNames?: string[] };
    versions?: { name?: string }[];
  };
  status?: { conditions?: Condition[] };
};

// A definition serves its kind once Established; names a sibling already took never will.
function crdStatus(b: CRDBody): Status {
  const conditions = b.status?.conditions;
  if (b.metadata?.deletionTimestamp) return { label: 'Terminating', tone: 'pending' };
  if (conditionStatus(conditions, 'NamesAccepted') === 'False') return { label: 'Names not accepted', tone: 'failing' };
  if (conditionStatus(conditions, 'Established') === 'True') return { label: 'Established', tone: 'ok' };
  return { label: 'Pending', tone: 'pending' };
}

const crdColumns: KindColumn<CRDBody>[] = [
  statusColumn('Status', 160, crdStatus),
  textColumn('Group', 200, (b) => b.spec?.group, { mono: true }),
  textColumn('Kind', 160, (b) => b.spec?.names?.kind),
  textColumn('Scope', 104, (b) => b.spec?.scope),
  listColumn('Versions', 128, (b) => b.spec?.versions?.map((v) => v.name)),
  textColumn('Plural', 160, (b) => b.spec?.names?.plural, { hidden: true }),
  listColumn('Short names', 128, (b) => b.spec?.names?.shortNames, { hidden: true }),
];

type APIServiceBody = {
  spec?: { group?: string; version?: string; service?: { namespace?: string; name?: string } };
  status?: { conditions?: Condition[] };
};

// kubectl's Available: the condition's status, with its reason when it is not True.
function apiServiceStatus(b: APIServiceBody): Status | undefined {
  const c = b.status?.conditions?.find((x) => x.type === 'Available');
  if (!c?.status) return undefined;
  if (c.status === 'True') return { label: 'True', tone: 'ok' };
  const label = c.reason ? `${c.status} (${c.reason})` : c.status;
  return { label, tone: c.status === 'False' ? 'failing' : 'pending' };
}

const apiServiceColumns: KindColumn<APIServiceBody>[] = [
  statusColumn('Available', 224, apiServiceStatus),
  // No service means the kube-apiserver serves the group itself.
  textColumn('Service', 224, (b) => {
    const s = b.spec?.service;
    if (!s) return 'Local';
    return s.namespace ? `${s.namespace}/${s.name ?? ''}` : s.name;
  }),
  textColumn('Group', 200, (b) => b.spec?.group, { hidden: true, mono: true }),
  textColumn('Version', 88, (b) => b.spec?.version, { hidden: true }),
];

type WebhookConfigurationBody = { webhooks?: { name?: string; failurePolicy?: string }[] };

const webhookColumns: KindColumn<WebhookConfigurationBody>[] = [
  countColumn('Webhooks', 88, (b) => b.webhooks?.length ?? 0),
  listColumn('Webhook names', 256, (b) => b.webhooks?.map((w) => w.name), {
    hidden: true,
    mono: true,
  }),
  listColumn('Failure policies', 136, (b) => b.webhooks?.map((w) => w.failurePolicy), {
    hidden: true,
  }),
];

type PolicyBody = {
  spec?: {
    validations?: unknown[];
    paramKind?: { apiVersion?: string; kind?: string };
    failurePolicy?: string;
  };
};

const policyColumns: KindColumn<PolicyBody>[] = [
  countColumn('Validations', 96, (b) => b.spec?.validations?.length ?? 0),
  // kubectl's form: apiVersion/kind.
  textColumn('Param kind', 192, (b) => {
    const p = b.spec?.paramKind;
    return p?.kind && (p.apiVersion ? `${p.apiVersion}/${p.kind}` : p.kind);
  }),
  textColumn('Failure policy', 120, (b) => b.spec?.failurePolicy, { hidden: true }),
];

type PolicyBindingBody = {
  spec?: {
    policyName?: string;
    paramRef?: { namespace?: string; name?: string };
    validationActions?: string[];
  };
};

const policyBindingColumns: KindColumn<PolicyBindingBody>[] = [
  textColumn('Policy', 200, (b) => b.spec?.policyName),
  // A ref by selector has no name; it reads as absent, as kubectl prints it.
  textColumn('Param ref', 192, (b) => {
    const p = b.spec?.paramRef;
    return p?.name && (p.namespace ? `${p.namespace}/${p.name}` : p.name);
  }),
  listColumn('Validation actions', 160, (b) => b.spec?.validationActions, { hidden: true }),
];

const ADMISSION = 'admissionregistration.k8s.io/v1';

export const extensionKinds = [
  kind('apiextensions.k8s.io/v1', 'customresourcedefinitions', crdColumns),
  kind('apiregistration.k8s.io/v1', 'apiservices', apiServiceColumns),
  kind(ADMISSION, 'mutatingwebhookconfigurations', webhookColumns),
  kind(ADMISSION, 'validatingwebhookconfigurations', webhookColumns),
  kind(ADMISSION, 'validatingadmissionpolicies', policyColumns),
  kind(ADMISSION, 'validatingadmissionpolicybindings', policyBindingColumns),
];
