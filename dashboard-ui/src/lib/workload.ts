// Copyright 2024-2025 Andres Morey
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

import CronJobIcon from '@/assets/k8s-icons/cronjob.svg?react';
import DaemonSetIcon from '@/assets/k8s-icons/daemonset.svg?react';
import DeploymentIcon from '@/assets/k8s-icons/deployment.svg?react';
import JobIcon from '@/assets/k8s-icons/job.svg?react';
import PodIcon from '@/assets/k8s-icons/pod.svg?react';
import ReplicaSetIcon from '@/assets/k8s-icons/replicaset.svg?react';
import StatefulSetIcon from '@/assets/k8s-icons/statefulset.svg?react';

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

export enum Workload {
  CRONJOBS = 'cronjobs',
  DAEMONSETS = 'daemonsets',
  DEPLOYMENTS = 'deployments',
  JOBS = 'jobs',
  PODS = 'pods',
  REPLICASETS = 'replicasets',
  STATEFULSETS = 'statefulsets',
}

export const allWorkloads = [
  Workload.CRONJOBS,
  Workload.DAEMONSETS,
  Workload.DEPLOYMENTS,
  Workload.JOBS,
  Workload.PODS,
  Workload.REPLICASETS,
  Workload.STATEFULSETS,
];

export const iconMap = {
  [Workload.CRONJOBS]: CronJobIcon,
  [Workload.DAEMONSETS]: DaemonSetIcon,
  [Workload.DEPLOYMENTS]: DeploymentIcon,
  [Workload.JOBS]: JobIcon,
  [Workload.PODS]: PodIcon,
  [Workload.REPLICASETS]: ReplicaSetIcon,
  [Workload.STATEFULSETS]: StatefulSetIcon,
};

export const glyphIconMap = {
  [Workload.CRONJOBS]: CronJobGlyphIcon,
  [Workload.DAEMONSETS]: DaemonSetGlyphIcon,
  [Workload.DEPLOYMENTS]: DeploymentGlyphIcon,
  [Workload.JOBS]: JobGlyphIcon,
  [Workload.PODS]: PodGlyphIcon,
  [Workload.REPLICASETS]: ReplicaSetGlyphIcon,
  [Workload.STATEFULSETS]: StatefulSetGlyphIcon,
};

export const knockoutIconMap = {
  [Workload.CRONJOBS]: CronJobKnockoutIcon,
  [Workload.DAEMONSETS]: DaemonSetKnockoutIcon,
  [Workload.DEPLOYMENTS]: DeploymentKnockoutIcon,
  [Workload.JOBS]: JobKnockoutIcon,
  [Workload.PODS]: PodKnockoutIcon,
  [Workload.REPLICASETS]: ReplicaSetKnockoutIcon,
  [Workload.STATEFULSETS]: StatefulSetKnockoutIcon,
};

export const labelsPMap = {
  [Workload.CRONJOBS]: 'Cron Jobs',
  [Workload.DAEMONSETS]: 'Daemon Sets',
  [Workload.DEPLOYMENTS]: 'Deployments',
  [Workload.JOBS]: 'Jobs',
  [Workload.PODS]: 'Pods',
  [Workload.REPLICASETS]: 'Replica Sets',
  [Workload.STATEFULSETS]: 'Stateful Sets',
};

export const typenameMap: Record<string, Workload> = {
  AppsV1DaemonSet: Workload.DAEMONSETS,
  AppsV1Deployment: Workload.DEPLOYMENTS,
  AppsV1ReplicaSet: Workload.REPLICASETS,
  AppsV1StatefulSet: Workload.STATEFULSETS,
  BatchV1CronJob: Workload.CRONJOBS,
  BatchV1Job: Workload.JOBS,
  CoreV1Pod: Workload.PODS,
};
