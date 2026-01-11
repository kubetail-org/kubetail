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

import CronJobGlyphIcon from '@/assets/k8s-icons/glyph/cronjob.svg?react';
import DaemonSetGlyphIcon from '@/assets/k8s-icons/glyph/daemonset.svg?react';
import DeploymentGlyphIcon from '@/assets/k8s-icons/glyph/deployment.svg?react';
import JobGlyphIcon from '@/assets/k8s-icons/glyph/job.svg?react';
import PodGlyphIcon from '@/assets/k8s-icons/glyph/pod.svg?react';
import ReplicaSetGlyphIcon from '@/assets/k8s-icons/glyph/replicaset.svg?react';
import StatefulSetGlyphIcon from '@/assets/k8s-icons/glyph/statefulset.svg?react';

import CronJobKnockoutIcon from '@/assets/k8s-icons/knockout/cronjob.svg?react';
import DaemonSetKnockoutIcon from '@/assets/k8s-icons/knockout/daemonset.svg?react';
import DeploymentKnockoutIcon from '@/assets/k8s-icons/knockout/deployment.svg?react';
import JobKnockoutIcon from '@/assets/k8s-icons/knockout/job.svg?react';
import PodKnockoutIcon from '@/assets/k8s-icons/knockout/pod.svg?react';
import ReplicaSetKnockoutIcon from '@/assets/k8s-icons/knockout/replicaset.svg?react';
import StatefulSetKnockoutIcon from '@/assets/k8s-icons/knockout/statefulset.svg?react';

export enum WorkloadKind {
  CRONJOBS = 'cronjobs',
  DAEMONSETS = 'daemonsets',
  DEPLOYMENTS = 'deployments',
  JOBS = 'jobs',
  PODS = 'pods',
  REPLICASETS = 'replicasets',
  STATEFULSETS = 'statefulsets',
}

export const ALL_WORKLOAD_KINDS = [
  WorkloadKind.CRONJOBS,
  WorkloadKind.DAEMONSETS,
  WorkloadKind.DEPLOYMENTS,
  WorkloadKind.JOBS,
  WorkloadKind.PODS,
  WorkloadKind.REPLICASETS,
  WorkloadKind.STATEFULSETS,
];

export const GLYPH_ICON_MAP = {
  [WorkloadKind.CRONJOBS]: CronJobGlyphIcon,
  [WorkloadKind.DAEMONSETS]: DaemonSetGlyphIcon,
  [WorkloadKind.DEPLOYMENTS]: DeploymentGlyphIcon,
  [WorkloadKind.JOBS]: JobGlyphIcon,
  [WorkloadKind.PODS]: PodGlyphIcon,
  [WorkloadKind.REPLICASETS]: ReplicaSetGlyphIcon,
  [WorkloadKind.STATEFULSETS]: StatefulSetGlyphIcon,
};

export const KNOCKOUT_ICON_MAP = {
  [WorkloadKind.CRONJOBS]: CronJobKnockoutIcon,
  [WorkloadKind.DAEMONSETS]: DaemonSetKnockoutIcon,
  [WorkloadKind.DEPLOYMENTS]: DeploymentKnockoutIcon,
  [WorkloadKind.JOBS]: JobKnockoutIcon,
  [WorkloadKind.PODS]: PodKnockoutIcon,
  [WorkloadKind.REPLICASETS]: ReplicaSetKnockoutIcon,
  [WorkloadKind.STATEFULSETS]: StatefulSetKnockoutIcon,
};

export const PLURAL_LABEL_MAP = {
  [WorkloadKind.CRONJOBS]: 'Cron Jobs',
  [WorkloadKind.DAEMONSETS]: 'Daemon Sets',
  [WorkloadKind.DEPLOYMENTS]: 'Deployments',
  [WorkloadKind.JOBS]: 'Jobs',
  [WorkloadKind.PODS]: 'Pods',
  [WorkloadKind.REPLICASETS]: 'Replica Sets',
  [WorkloadKind.STATEFULSETS]: 'Stateful Sets',
};

export const TYPENAME_TO_KIND_MAP: Record<string, WorkloadKind> = {
  AppsV1DaemonSet: WorkloadKind.DAEMONSETS,
  AppsV1Deployment: WorkloadKind.DEPLOYMENTS,
  AppsV1ReplicaSet: WorkloadKind.REPLICASETS,
  AppsV1StatefulSet: WorkloadKind.STATEFULSETS,
  BatchV1CronJob: WorkloadKind.CRONJOBS,
  BatchV1Job: WorkloadKind.JOBS,
  CoreV1Pod: WorkloadKind.PODS,
};
