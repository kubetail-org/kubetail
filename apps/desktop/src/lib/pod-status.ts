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

// A pod's status as kubectl's `printPod` reads it, and its tone. The Pods table and the
// Overview share this one reading.
import type { StatusTone } from '@/lib/cluster-health';

type ContainerStatus = {
  name?: string;
  ready?: boolean;
  started?: boolean;
  restartCount?: number;
  state?: {
    waiting?: { reason?: string };
    terminated?: { reason?: string; exitCode?: number; signal?: number };
    running?: unknown;
  };
};

export type PodBody = {
  metadata?: { deletionTimestamp?: string };
  spec?: {
    nodeName?: string;
    serviceAccountName?: string;
    priorityClassName?: string;
    containers?: { name?: string; image?: string }[];
    initContainers?: { name?: string; restartPolicy?: string }[];
  };
  status?: {
    phase?: string;
    reason?: string;
    podIP?: string;
    hostIP?: string;
    qosClass?: string;
    startTime?: string;
    conditions?: { type?: string; status?: string; lastTransitionTime?: string }[];
    containerStatuses?: ContainerStatus[];
    initContainerStatuses?: ContainerStatus[];
  };
};

function hasCondition(b: PodBody, type: string): boolean {
  return (b.status?.conditions ?? []).some((c) => c.type === type && c.status === 'True');
}

// A native sidecar (init container with restartPolicy Always) keeps running past
// initialization, so a started one doesn't hold the pod in an Init: state.
function isRestartableInit(b: PodBody, c: ContainerStatus): boolean {
  return (b.spec?.initContainers ?? []).some((s) => s.name === c.name && s.restartPolicy === 'Always');
}

// Mirrors kubectl's `printPod`. The bare `status.phase` misreads a crash-looping
// pod (phase=Running), an initializing one, and a terminating one — the
// actionable status lives in container state.
function podReason(b: PodBody): string {
  const st = b.status;
  let reason = st?.reason || st?.phase || '';

  // Init containers first: the first one not cleanly finished decides the status.
  const initStatuses = st?.initContainerStatuses ?? [];
  const initTotal = b.spec?.initContainers?.length ?? initStatuses.length;
  let initializing = false;
  for (let i = 0; i < initStatuses.length; i += 1) {
    const c = initStatuses[i];
    const term = c.state?.terminated;
    const waiting = c.state?.waiting;
    // Skip init containers that finished cleanly, and sidecars already up.
    const settled = (term && term.exitCode === 0) || (isRestartableInit(b, c) && c.started);
    if (!settled) {
      if (term) {
        if (term.reason) reason = `Init:${term.reason}`;
        else if (term.signal) reason = `Init:Signal:${term.signal}`;
        else reason = `Init:ExitCode:${term.exitCode ?? 0}`;
      } else if (waiting?.reason && waiting.reason !== 'PodInitializing') {
        reason = `Init:${waiting.reason}`;
      } else {
        reason = `Init:${i}/${initTotal}`;
      }
      initializing = true;
      break;
    }
  }

  // Regular containers: last wins, as kubectl scans in reverse.
  if (!initializing || hasCondition(b, 'Initialized')) {
    let hasRunning = false;
    const cs = st?.containerStatuses ?? [];
    for (let i = cs.length - 1; i >= 0; i -= 1) {
      const c = cs[i];
      const term = c.state?.terminated;
      const waiting = c.state?.waiting;
      if (waiting?.reason) reason = waiting.reason;
      else if (term?.reason) reason = term.reason;
      else if (term) reason = term.signal ? `Signal:${term.signal}` : `ExitCode:${term.exitCode ?? 0}`;
      else if (c.ready && c.state?.running) hasRunning = true;
    }
    // A restarted-and-recovered container leaves a stale Completed from its last termination.
    if (reason === 'Completed' && hasRunning) reason = hasCondition(b, 'Ready') ? 'Running' : 'NotReady';
  }

  // Deletion wins over everything except a pod already in a terminal phase.
  if (b.metadata?.deletionTimestamp) {
    if (st?.reason === 'NodeLost') reason = 'Unknown';
    else if (st?.phase !== 'Succeeded' && st?.phase !== 'Failed') reason = 'Terminating';
  }
  return reason;
}

// The statuses that mean a pod is broken, rather than on its way somewhere. An init
// container's carries an `Init:` prefix.
const FAILING = new Set([
  'CrashLoopBackOff',
  'Error',
  'Failed',
  'Evicted',
  'OOMKilled',
  'ImagePullBackOff',
  'ErrImagePull',
  'InvalidImageName',
  'CreateContainerError',
  'CreateContainerConfigError',
  'RunContainerError',
  'ContainerStatusUnknown',
]);

export function podStatusTone(status: string): StatusTone {
  if (status === 'Running' || status === 'Completed' || status === 'Succeeded') return 'ok';
  const reason = status.replace(/^Init:/, '');
  if (FAILING.has(reason) || reason.startsWith('Signal:') || reason.startsWith('ExitCode:')) return 'failing';
  return 'pending';
}

export type PodStatus = { label: string; tone: StatusTone };

// A pod's tone is its status's, except that a running pod is healthy only once it is ready,
// so a pod waiting on its readiness probe, or never passing it, reads as pending.
export function podStatus(b: PodBody): PodStatus | undefined {
  const label = podReason(b);
  if (!label) return undefined;
  const waiting = label === 'Running' && !hasCondition(b, 'Ready');
  return { label, tone: waiting ? 'pending' : podStatusTone(label) };
}
