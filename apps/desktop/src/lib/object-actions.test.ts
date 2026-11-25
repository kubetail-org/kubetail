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

import { actionsForKind, logsSourceKind } from './object-actions';

describe('actionsForKind', () => {
  it('gives every kind YAML and events', () => {
    expect(actionsForKind({ apiVersion: 'v1', resource: 'configmaps' })).toEqual(['yaml', 'events']);
    expect(actionsForKind({ apiVersion: 'example.com/v1', resource: 'widgets' })).toEqual(['yaml', 'events']);
  });

  it.each([
    ['v1', 'pods'],
    ['apps/v1', 'deployments'],
    ['apps/v1', 'statefulsets'],
    ['apps/v1', 'daemonsets'],
    ['apps/v1', 'replicasets'],
    ['batch/v1', 'jobs'],
    ['batch/v1', 'cronjobs'],
  ])('adds logs for %s %s', (apiVersion, resource) => {
    expect(actionsForKind({ apiVersion, resource })).toEqual(['yaml', 'events', 'logs']);
  });

  // A CRD reusing a built-in's plural is not that kind.
  it('matches the API group as well as the plural', () => {
    expect(actionsForKind({ apiVersion: 'example.com/v1', resource: 'pods' })).toEqual(['yaml', 'events']);
  });
});

describe('logsSourceKind', () => {
  it('names the log source kind of a kind a log view reads, and nothing for another', () => {
    expect(logsSourceKind({ apiVersion: 'apps/v1', resource: 'statefulsets' })).toBe('StatefulSet');
    expect(logsSourceKind({ apiVersion: 'batch/v1', resource: 'cronjobs' })).toBe('CronJob');
    expect(logsSourceKind({ apiVersion: 'v1', resource: 'services' })).toBeUndefined();
  });
});
