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

// The workload kinds' columns: kubectl's, its `-o wide` ones hidden, and a status for the
// kinds whose objects converge on a desired count.
import { Elapsed } from '@/components/primitives/elapsed';
import { ownerOf } from '@/lib/object-owner';

import {
  ageColumn,
  conditionStatus,
  countColumn,
  durationColumn,
  kind,
  labelsText,
  listColumn,
  readyColumn,
  selectorText,
  statusColumn,
  textColumn,
} from './common';
import type { KindColumn, LabelSelector, Status } from './common';

type Conditions = { type?: string; status?: string; reason?: string }[];

type PodTemplate = {
  metadata?: { labels?: Record<string, string> };
  spec?: { containers?: { name?: string; image?: string }[]; nodeSelector?: Record<string, string> };
};

// kubectl's `-o wide` trio, read off the pod template.
function templateColumns<T>(
  templateOf: (b: T) => PodTemplate | undefined,
  selector?: (b: T) => string | undefined,
): KindColumn<T>[] {
  return [
    listColumn('Containers', 160, (b) => templateOf(b)?.spec?.containers?.map((c) => c.name), { hidden: true }),
    listColumn('Images', 224, (b) => templateOf(b)?.spec?.containers?.map((c) => c.image), {
      hidden: true,
      mono: true,
    }),
    ...(selector ? [textColumn('Selector', 200, selector, { hidden: true, mono: true })] : []),
  ];
}

// A set of replicas converging on a desired count: ready once all are, scaled to zero when
// none are wanted.
function replicaStatus(desired: number, ready: number, { failed = false, zeroLabel = 'Scaled to zero' } = {}): Status {
  if (failed) return { label: 'Failed', tone: 'failing' };
  if (desired === 0) return { label: zeroLabel, tone: 'ok' };
  if (ready >= desired) return { label: 'Ready', tone: 'ok' };
  return { label: 'Not ready', tone: 'pending' };
}

// ---- Deployment

type DeploymentBody = {
  metadata?: { generation?: number };
  spec?: { replicas?: number; paused?: boolean; selector?: LabelSelector; template?: PodTemplate };
  status?: {
    observedGeneration?: number;
    replicas?: number;
    readyReplicas?: number;
    updatedReplicas?: number;
    availableReplicas?: number;
    conditions?: Conditions;
  };
};

// `kubectl rollout status`'s reading. Until the controller has seen the current spec, the
// status describes the one before, an earlier rollout's failure included. Failed is what
// kubectl gives up on. Done is every desired replica updated, no old one left, and every
// updated one available: `availableReplicas` alone also counts the old pods.
function deploymentStatus(b: DeploymentBody): Status {
  const s = b.status ?? {};
  if ((s.observedGeneration ?? 0) < (b.metadata?.generation ?? 0)) return { label: 'Progressing', tone: 'pending' };
  const conditions = s.conditions;
  const stalled = conditions?.some((c) => c.type === 'Progressing' && c.status === 'False');
  if (stalled || conditionStatus(conditions, 'ReplicaFailure') === 'True') return { label: 'Failed', tone: 'failing' };
  const desired = b.spec?.replicas ?? 0;
  if (b.spec?.paused) return { label: 'Paused', tone: 'pending' };
  if (desired === 0) return { label: 'Scaled to zero', tone: 'ok' };
  const updated = s.updatedReplicas ?? 0;
  const done = updated >= desired && (s.replicas ?? 0) <= updated && (s.availableReplicas ?? 0) >= updated;
  return done ? { label: 'Available', tone: 'ok' } : { label: 'Progressing', tone: 'pending' };
}

const deploymentColumns: KindColumn<DeploymentBody>[] = [
  statusColumn('Status', 120, deploymentStatus),
  readyColumn('Ready', (b) => ({ ready: b.status?.readyReplicas ?? 0, total: b.spec?.replicas ?? 0 })),
  countColumn('Up-to-date', 96, (b) => b.status?.updatedReplicas ?? 0),
  countColumn('Available', 88, (b) => b.status?.availableReplicas ?? 0),
  ...templateColumns<DeploymentBody>(
    (b) => b.spec?.template,
    (b) => selectorText(b.spec?.selector),
  ),
];

// ---- StatefulSet

type StatefulSetBody = {
  spec?: { replicas?: number; template?: PodTemplate; serviceName?: string; podManagementPolicy?: string };
  status?: { readyReplicas?: number; currentReplicas?: number; updatedReplicas?: number };
};

const statefulSetStatus = (b: StatefulSetBody) => replicaStatus(b.spec?.replicas ?? 0, b.status?.readyReplicas ?? 0);

const statefulSetColumns: KindColumn<StatefulSetBody>[] = [
  statusColumn('Status', 120, statefulSetStatus),
  readyColumn('Ready', (b) => ({ ready: b.status?.readyReplicas ?? 0, total: b.spec?.replicas ?? 0 })),
  ...templateColumns<StatefulSetBody>((b) => b.spec?.template),
  textColumn('Service', 144, (b) => b.spec?.serviceName, { hidden: true }),
  countColumn('Up-to-date', 96, (b) => b.status?.updatedReplicas ?? 0, { hidden: true }),
];

// ---- DaemonSet (no spec.replicas: the controller decides how many nodes want one)

type DaemonSetBody = {
  spec?: { selector?: LabelSelector; template?: PodTemplate };
  status?: {
    desiredNumberScheduled?: number;
    currentNumberScheduled?: number;
    numberReady?: number;
    updatedNumberScheduled?: number;
    numberAvailable?: number;
  };
};

const daemonSetStatus = (b: DaemonSetBody) =>
  replicaStatus(b.status?.desiredNumberScheduled ?? 0, b.status?.numberReady ?? 0, { zeroLabel: 'No nodes' });

const daemonSetColumns: KindColumn<DaemonSetBody>[] = [
  statusColumn('Status', 120, daemonSetStatus),
  countColumn('Desired', 80, (b) => b.status?.desiredNumberScheduled ?? 0),
  countColumn('Current', 80, (b) => b.status?.currentNumberScheduled ?? 0),
  countColumn('Ready', 72, (b) => b.status?.numberReady ?? 0),
  countColumn('Up-to-date', 96, (b) => b.status?.updatedNumberScheduled ?? 0),
  countColumn('Available', 88, (b) => b.status?.numberAvailable ?? 0),
  textColumn('Node selector', 176, (b) => labelsText(b.spec?.template?.spec?.nodeSelector), {
    mono: true,
  }),
  ...templateColumns<DaemonSetBody>(
    (b) => b.spec?.template,
    (b) => selectorText(b.spec?.selector),
  ),
];

// ---- ReplicaSet and ReplicationController: one shape, but an RC's selector is a plain map

type ReplicasBody = {
  spec?: { replicas?: number; selector?: unknown; template?: PodTemplate };
  status?: { replicas?: number; readyReplicas?: number; conditions?: Conditions };
};

// ReplicaFailure is the controller failing to create pods, such as over a quota.
function replicasStatus(b: ReplicasBody): Status {
  const failed = conditionStatus(b.status?.conditions, 'ReplicaFailure') === 'True';
  return replicaStatus(b.spec?.replicas ?? 0, b.status?.readyReplicas ?? 0, { failed });
}

function replicasColumns(selector: (s: unknown) => string | undefined): KindColumn<ReplicasBody>[] {
  return [
    statusColumn('Status', 120, replicasStatus),
    countColumn('Desired', 80, (b) => b.spec?.replicas ?? 0),
    countColumn('Current', 80, (b) => b.status?.replicas ?? 0),
    countColumn('Ready', 72, (b) => b.status?.readyReplicas ?? 0),
    ...templateColumns<ReplicasBody>(
      (b) => b.spec?.template,
      (b) => selector(b.spec?.selector),
    ),
  ];
}

// ---- Job

type JobBody = {
  metadata?: { deletionTimestamp?: string };
  spec?: { completions?: number; parallelism?: number; selector?: LabelSelector; template?: PodTemplate };
  status?: {
    succeeded?: number;
    failed?: number;
    active?: number;
    startTime?: string;
    completionTime?: string;
    conditions?: Conditions;
  };
};

// kubectl's order: a finished job's condition wins over its deletion, which wins over the
// conditions a job holds on its way to finishing.
function jobStatus(b: JobBody): Status {
  const is = (type: string) => conditionStatus(b.status?.conditions, type) === 'True';
  if (is('Complete')) return { label: 'Complete', tone: 'ok' };
  if (is('Failed')) return { label: 'Failed', tone: 'failing' };
  if (b.metadata?.deletionTimestamp) return { label: 'Terminating', tone: 'pending' };
  if (is('FailureTarget')) return { label: 'FailureTarget', tone: 'failing' };
  if (is('SuccessCriteriaMet')) return { label: 'SuccessCriteriaMet', tone: 'ok' };
  if (is('Suspended')) return { label: 'Suspended', tone: 'pending' };
  return { label: 'Running', tone: 'pending' };
}

// kubectl's: `succeeded/completions`, and a work queue's `succeeded/1 of parallelism`.
function jobCompletions(b: JobBody): string {
  const succeeded = b.status?.succeeded ?? 0;
  if (b.spec?.completions !== undefined) return `${succeeded}/${b.spec.completions}`;
  const parallelism = b.spec?.parallelism ?? 1;
  return parallelism > 1 ? `${succeeded}/1 of ${parallelism}` : `${succeeded}/1`;
}

function jobStart(b: JobBody): number {
  return b.status?.startTime ? Date.parse(b.status.startTime) : Number.NaN;
}

// A running job's duration grows until it completes.
function jobDurationSeconds(b: JobBody): number | undefined {
  const start = jobStart(b);
  if (Number.isNaN(start)) return undefined;
  const end = b.status?.completionTime ? Date.parse(b.status.completionTime) : Date.now();
  return Number.isNaN(end) ? undefined : (end - start) / 1000;
}

const jobDuration = durationColumn('Duration', 88, jobDurationSeconds);

const jobColumns: KindColumn<JobBody>[] = [
  statusColumn('Status', 136, jobStatus),
  {
    key: 'Completions',
    header: 'Completions',
    className: 'tabular-nums',
    width: 104,
    // Least done first.
    sortValue: (b) => (b.status?.succeeded ?? 0) / (b.spec?.completions ?? 1),
    cell: jobCompletions,
  },
  {
    ...jobDuration,
    sortsByClock: true,
    cell: (b) => {
      const start = jobStart(b);
      const running = !b.status?.completionTime && !Number.isNaN(start);
      return running ? <Elapsed since={start} /> : jobDuration.cell(b);
    },
  },
  countColumn('Active', 72, (b) => b.status?.active ?? 0, { hidden: true }),
  countColumn('Failed', 72, (b) => b.status?.failed ?? 0, { hidden: true }),
  ...templateColumns<JobBody>(
    (b) => b.spec?.template,
    (b) => selectorText(b.spec?.selector),
  ),
];

// ---- CronJob

type CronJobBody = {
  spec?: {
    schedule?: string;
    timeZone?: string;
    suspend?: boolean;
    jobTemplate?: { spec?: { selector?: LabelSelector; template?: PodTemplate } };
  };
  status?: { active?: unknown[]; lastScheduleTime?: string; lastSuccessfulTime?: string };
};

const cronJobColumns: KindColumn<CronJobBody>[] = [
  textColumn('Schedule', 128, (b) => b.spec?.schedule, { mono: true }),
  textColumn('Suspend', 80, (b) => (b.spec?.suspend ? 'True' : 'False')),
  countColumn('Active', 72, (b) => b.status?.active?.length ?? 0),
  ageColumn('Last schedule', 112, (b) => b.status?.lastScheduleTime),
  textColumn('Time zone', 128, (b) => b.spec?.timeZone, { hidden: true }),
  ageColumn('Last success', 112, (b) => b.status?.lastSuccessfulTime, { hidden: true }),
  ...templateColumns<CronJobBody>(
    (b) => b.spec?.jobTemplate?.spec?.template,
    (b) => selectorText(b.spec?.jobTemplate?.spec?.selector),
  ),
];

// ---- HorizontalPodAutoscaler

type MetricTarget = { type?: string; averageUtilization?: number; averageValue?: string; value?: string };
type MetricValue = { averageUtilization?: number; averageValue?: string; value?: string };
type MetricSource = {
  name?: string;
  metric?: { name?: string };
  target?: MetricTarget;
  current?: MetricValue;
};
type Metric = {
  type?: string;
  resource?: MetricSource;
  containerResource?: MetricSource;
  pods?: MetricSource;
  object?: MetricSource;
  external?: MetricSource;
};

type HPABody = {
  spec?: {
    scaleTargetRef?: { kind?: string; name?: string };
    minReplicas?: number;
    maxReplicas?: number;
    metrics?: Metric[];
  };
  status?: { currentReplicas?: number; desiredReplicas?: number; currentMetrics?: Metric[] };
};

function sourceOf(m: Metric | undefined): MetricSource | undefined {
  return m?.resource ?? m?.containerResource ?? m?.pods ?? m?.object ?? m?.external;
}

function metricValue(v: MetricValue | undefined, utilization: boolean): string | undefined {
  if (utilization) return v?.averageUtilization === undefined ? undefined : `${v.averageUtilization}%`;
  return v?.averageValue ?? v?.value;
}

// kubectl's TARGETS: `current/target` per metric, by position in the spec; `<unknown>` until
// the controller has read one.
function hpaTargets(b: HPABody): string | undefined {
  const targets = (b.spec?.metrics ?? []).map((m, i) => {
    const spec = sourceOf(m);
    const utilization = spec?.target?.type === 'Utilization';
    const name = spec?.name ?? spec?.metric?.name;
    const current = metricValue(sourceOf(b.status?.currentMetrics?.[i])?.current, utilization) ?? '<unknown>';
    const target = metricValue(spec?.target, utilization) ?? '<unknown>';
    return `${name ? `${name}: ` : ''}${current}/${target}`;
  });
  return targets.join(', ') || undefined;
}

const hpaColumns: KindColumn<HPABody>[] = [
  textColumn('Reference', 176, (b) => {
    const ref = b.spec?.scaleTargetRef;
    return ref?.kind && ref.name ? `${ref.kind}/${ref.name}` : undefined;
  }),
  textColumn('Targets', 200, hpaTargets),
  countColumn('Min pods', 88, (b) => b.spec?.minReplicas ?? 1),
  countColumn('Max pods', 88, (b) => b.spec?.maxReplicas),
  countColumn('Replicas', 88, (b) => b.status?.currentReplicas),
  countColumn('Desired', 80, (b) => b.status?.desiredReplicas, { hidden: true }),
];

// ---- PodDisruptionBudget

type PDBBody = {
  spec?: { minAvailable?: number | string; maxUnavailable?: number | string; selector?: LabelSelector };
  status?: { disruptionsAllowed?: number; currentHealthy?: number; desiredHealthy?: number };
};

const intOrString = (v: number | string | undefined) => (v === undefined ? undefined : String(v));

const pdbColumns: KindColumn<PDBBody>[] = [
  textColumn('Min available', 112, (b) => intOrString(b.spec?.minAvailable)),
  textColumn('Max unavailable', 128, (b) => intOrString(b.spec?.maxUnavailable)),
  countColumn('Allowed disruptions', 152, (b) => b.status?.disruptionsAllowed ?? 0),
  readyColumn('Healthy', (b) => ({ ready: b.status?.currentHealthy ?? 0, total: b.status?.desiredHealthy ?? 0 }), {
    hidden: true,
  }),
  textColumn('Selector', 200, (b) => selectorText(b.spec?.selector), { hidden: true, mono: true }),
];

// ---- ControllerRevision

type RevisionBody = { revision?: number };

const controllerRevisionColumns: KindColumn<RevisionBody>[] = [
  textColumn('Controller', 176, ownerOf),
  countColumn('Revision', 88, (b) => b.revision),
];

// ---- PodTemplate

type PodTemplateBody = { template?: PodTemplate };

const podTemplateColumns: KindColumn<PodTemplateBody>[] = [
  listColumn('Containers', 160, (b) => b.template?.spec?.containers?.map((c) => c.name)),
  listColumn('Images', 224, (b) => b.template?.spec?.containers?.map((c) => c.image), {
    mono: true,
  }),
  textColumn('Pod labels', 200, (b) => labelsText(b.template?.metadata?.labels), { mono: true }),
];

export const workloadKinds = [
  kind('apps/v1', 'deployments', deploymentColumns),
  kind('apps/v1', 'statefulsets', statefulSetColumns),
  kind('apps/v1', 'daemonsets', daemonSetColumns),
  kind(
    'apps/v1',
    'replicasets',
    replicasColumns((s) => selectorText(s as LabelSelector | undefined)),
  ),
  kind(
    'v1',
    'replicationcontrollers',
    replicasColumns((s) => labelsText(s as Record<string, string> | undefined)),
  ),
  kind('batch/v1', 'jobs', jobColumns),
  kind('batch/v1', 'cronjobs', cronJobColumns),
  kind('autoscaling/v2', 'horizontalpodautoscalers', hpaColumns),
  kind('policy/v1', 'poddisruptionbudgets', pdbColumns),
  kind('apps/v1', 'controllerrevisions', controllerRevisionColumns),
  kind('v1', 'podtemplates', podTemplateColumns),
];
