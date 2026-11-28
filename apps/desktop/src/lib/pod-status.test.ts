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

import { podStatus, podStatusTone } from './pod-status';

describe('podStatusTone', () => {
  it('reads a running or finished pod as ok', () => {
    expect(['Running', 'Completed', 'Succeeded'].map(podStatusTone)).toEqual(['ok', 'ok', 'ok']);
  });

  it('reads a broken pod as failing, an init container’s too', () => {
    const failing = ['CrashLoopBackOff', 'ImagePullBackOff', 'Init:Error', 'Init:Signal:9', 'ExitCode:2', 'Evicted'];
    expect(failing.map(podStatusTone)).toEqual(failing.map(() => 'failing'));
  });

  it('reads a pod on its way somewhere as pending', () => {
    const pending = ['Pending', 'ContainerCreating', 'Init:0/2', 'Terminating', 'NotReady', 'Unknown'];
    expect(pending.map(podStatusTone)).toEqual(pending.map(() => 'pending'));
  });
});

describe('podStatus', () => {
  it('reads a running pod as pending until it is ready', () => {
    const running = (ready: string) => ({
      status: { phase: 'Running', conditions: [{ type: 'Ready', status: ready }] },
    });
    expect(podStatus(running('True'))).toEqual({ label: 'Running', tone: 'ok' });
    expect(podStatus(running('False'))).toEqual({ label: 'Running', tone: 'pending' });
  });

  // A Job past its activeDeadlineSeconds: kubectl names the reason, and the pod is done for.
  it('reads a pod in phase Failed as failing, whatever its reason', () => {
    expect(podStatus({ status: { phase: 'Failed', reason: 'DeadlineExceeded' } })).toEqual({
      label: 'DeadlineExceeded',
      tone: 'failing',
    });
  });

  it('reads nothing off a body with no phase', () => {
    expect(podStatus({})).toBeUndefined();
  });
});
