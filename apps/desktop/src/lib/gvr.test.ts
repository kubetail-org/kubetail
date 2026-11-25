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

import { gvrKey } from './gvr';

describe('gvrKey', () => {
  it('is stable and distinguishes kinds that share an apiVersion', () => {
    // The dashboard's workloads group — same apiVersion, different resource — must key apart.
    expect(gvrKey({ apiVersion: 'apps/v1', resource: 'deployments' })).toBe('apps/v1/deployments');
    expect(gvrKey({ apiVersion: 'apps/v1', resource: 'daemonsets' })).toBe('apps/v1/daemonsets');
    expect(gvrKey({ apiVersion: 'apps/v1', resource: 'deployments' })).not.toBe(
      gvrKey({ apiVersion: 'apps/v1', resource: 'daemonsets' }),
    );
  });

  it('distinguishes a resource plural shared across api groups', () => {
    // `events` exists in both the core group and events.k8s.io — the apiVersion disambiguates.
    expect(gvrKey({ apiVersion: 'v1', resource: 'events' })).not.toBe(
      gvrKey({ apiVersion: 'events.k8s.io/v1', resource: 'events' }),
    );
  });
});
