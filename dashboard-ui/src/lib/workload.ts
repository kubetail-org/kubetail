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
