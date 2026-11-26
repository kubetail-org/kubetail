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

import { readPath } from './jsonpath';

const body = {
  metadata: {
    name: 'widget-1',
    labels: { 'app.kubernetes.io/name': 'widget' },
    creationTimestamp: '2026-08-30T12:00:00Z',
  },
  spec: { replicas: 3, paused: false },
  status: { conditions: [{ type: 'Ready', status: 'True' }], phase: 'Running' },
};

describe('readPath', () => {
  it('reads a dotted path', () => {
    expect(readPath(body, '.spec.replicas')).toBe(3);
    expect(readPath(body, '.status.phase')).toBe('Running');
  });

  it('reads through an array index', () => {
    expect(readPath(body, '.status.conditions[0].type')).toBe('Ready');
  });

  it('reads a bracketed quoted key, which is how a dotted label name is written', () => {
    expect(readPath(body, ".metadata.labels['app.kubernetes.io/name']")).toBe('widget');
    expect(readPath(body, '.metadata.labels["app.kubernetes.io/name"]')).toBe('widget');
  });

  it('keeps falsy scalars, which are values rather than absence', () => {
    expect(readPath(body, '.spec.paused')).toBe(false);
  });

  it('returns undefined for a path that resolves to nothing', () => {
    expect(readPath(body, '.spec.missing')).toBeUndefined();
    expect(readPath(body, '.spec.missing.deeper')).toBeUndefined();
    expect(readPath(body, '.status.conditions[7].type')).toBeUndefined();
  });

  // Kubernetes validates jsonPath by parsing it with the full jsonpath library, so a filter or a
  // wildcard is a legal CRD. Unsupported is an empty cell, never a throw.
  it('returns undefined for a form it does not support', () => {
    expect(readPath(body, '.status.conditions[?(@.type=="Ready")].status')).toBeUndefined();
    expect(readPath(body, '.status.conditions[*].type')).toBeUndefined();
    expect(readPath(body, 'spec.replicas')).toBeUndefined();
    expect(readPath(body, '')).toBeUndefined();
  });

  it('survives a body that is not an object', () => {
    expect(readPath(undefined, '.spec.replicas')).toBeUndefined();
    expect(readPath('nope', '.spec.replicas')).toBeUndefined();
    expect(readPath(null, '.spec.replicas')).toBeUndefined();
  });
});
