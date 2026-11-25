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

import { act, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import type { ObjectColumn } from './common';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith, pillTone } from './testutil';

const sortOf = (c: ObjectColumn, o: ClusterCachedDataObject) => c.sortValue?.(o);

function kind(apiVersion: string, resource: string) {
  const columns = columnsForKind({ apiVersion, resource });
  const col = (header: string) => columns.find((c) => c.header === header)!;
  return {
    columns,
    col,
    visible: columns.filter((c) => !c.hiddenByDefault).map((c) => c.header),
    hidden: columns.filter((c) => c.hiddenByDefault).map((c) => c.header),
    cell: (header: string, raw: unknown) => cellText(col(header), objectWith(raw)),
    tone: (raw: unknown) => toneForKind({ apiVersion, resource })?.(objectWith(raw)),
  };
}

const template = {
  metadata: { labels: { app: 'web', tier: 'front' } },
  spec: {
    containers: [
      { name: 'web', image: 'web:1' },
      { name: 'proxy', image: 'proxy:2' },
    ],
  },
};

describe('deployment columns', () => {
  const d = kind('apps/v1', 'deployments');
  const [, ready, updated, available] = d.columns;

  it('counts ready of desired, up-to-date and available replicas', () => {
    const o = objectWith({
      spec: { replicas: 3 },
      status: { readyReplicas: 2, updatedReplicas: 3, availableReplicas: 1 },
    });
    expect(d.visible).toEqual(['Status', 'Ready', 'Up-to-date', 'Available']);
    expect([ready, updated, available].map((c) => cellText(c, o))).toEqual(['2/3', '3', '1']);
  });

  it('sorts by its share of ready replicas, one scaled to zero as whole', () => {
    const o = objectWith({
      spec: { replicas: 4 },
      status: { readyReplicas: 1, updatedReplicas: 3, availableReplicas: 2 },
    });
    expect([ready, updated, available].map((c) => sortOf(c, o))).toEqual([0.25, 3, 2]);
    expect(sortOf(ready, objectWith({}))).toBe(1);
    expect(sortOf(ready, objectWith({ spec: { replicas: 2 } }))).toBe(0);
    expect([updated, available].map((c) => sortOf(c, objectWith({})))).toEqual([0, 0]);
  });

  it('reads a body with no status as none ready', () => {
    expect([ready, updated, available].map((c) => cellText(c, objectWith({})))).toEqual(['0/0', '0', '0']);
  });

  it('offers the template’s containers, images and selector', () => {
    expect(d.hidden).toEqual(['Containers', 'Images', 'Selector']);
    const raw = {
      spec: {
        template,
        selector: {
          matchLabels: { app: 'web' },
          matchExpressions: [
            { key: 'tier', operator: 'In', values: ['a', 'b'] },
            { key: 'env', operator: 'NotIn', values: ['dev'] },
            { key: 'canary', operator: 'Exists' },
            { key: 'legacy', operator: 'DoesNotExist' },
          ],
        },
      },
    };
    expect(['Containers', 'Images', 'Selector'].map((h) => d.cell(h, raw))).toEqual([
      'web, proxy',
      'web:1, proxy:2',
      'app=web,tier in (a,b),env notin (dev),canary,!legacy',
    ]);
    expect(d.cell('Selector', {})).toBe('—');
  });

  it('reads a rollout as failed, paused, scaled to zero, available or progressing', () => {
    const at = (spec: object, status: object = {}, metadata: object = {}) => ({ metadata, spec, status });
    const rolled = { replicas: 2, updatedReplicas: 2, availableReplicas: 2 };
    const cases: [unknown, string, string][] = [
      [at({ replicas: 2 }, { conditions: [{ type: 'Progressing', status: 'False' }] }), 'Failed', 'failing'],
      [at({ replicas: 2 }, { conditions: [{ type: 'ReplicaFailure', status: 'True' }] }), 'Failed', 'failing'],
      [at({ replicas: 2, paused: true }), 'Paused', 'pending'],
      [at({ replicas: 0 }), 'Scaled to zero', 'ok'],
      [at({ replicas: 2 }, rolled), 'Available', 'ok'],
      [at({ replicas: 2 }, { ...rolled, observedGeneration: 3 }, { generation: 3 }), 'Available', 'ok'],
      [at({ replicas: 2 }, { ...rolled, availableReplicas: 1 }), 'Progressing', 'pending'],
      // The controller has yet to see the spec these counts would satisfy.
      [at({ replicas: 2 }, { ...rolled, observedGeneration: 3 }, { generation: 4 }), 'Progressing', 'pending'],
      // Nor the spec that replaced a failed rollout, whose conditions the status still holds.
      [
        at(
          { replicas: 2 },
          { observedGeneration: 3, conditions: [{ type: 'Progressing', status: 'False' }] },
          { generation: 4 },
        ),
        'Progressing',
        'pending',
      ],
      // A new pod not yet ready beside the old one it replaces, which is still available.
      [at({ replicas: 1 }, { replicas: 2, updatedReplicas: 1, availableReplicas: 1 }), 'Progressing', 'pending'],
      // Every new pod is up, and an old one has yet to go.
      [at({ replicas: 2 }, { ...rolled, replicas: 3, availableReplicas: 3 }), 'Progressing', 'pending'],
      [at({ replicas: 2 }), 'Progressing', 'pending'],
      [at({}), 'Scaled to zero', 'ok'],
      [at({ replicas: 2 }, { updatedReplicas: 2, availableReplicas: 2 }), 'Available', 'ok'],
      [at({ replicas: 2 }, { updatedReplicas: 2 }), 'Progressing', 'pending'],
      [{ spec: { replicas: 2 } }, 'Progressing', 'pending'],
    ];
    for (const [raw, label, tone] of cases) {
      expect(d.cell('Status', raw)).toBe(label);
      expect(pillTone(d.col('Status'), objectWith(raw))).toBe(tone);
      expect(d.tone(raw)).toBe(tone);
    }
  });
});

describe('statefulset columns', () => {
  const s = kind('apps/v1', 'statefulsets');

  it('reads ready of desired, and its status by it', () => {
    expect(s.visible).toEqual(['Status', 'Ready']);
    expect(s.hidden).toEqual(['Containers', 'Images', 'Service', 'Up-to-date']);
    expect(s.cell('Ready', { spec: { replicas: 3 }, status: { readyReplicas: 2 } })).toBe('2/3');
    expect(s.cell('Status', { spec: { replicas: 3 }, status: { readyReplicas: 3 } })).toBe('Ready');
    expect(s.tone({ spec: { replicas: 3 }, status: { readyReplicas: 2 } })).toBe('pending');
    expect(s.cell('Status', { spec: { replicas: 0 } })).toBe('Scaled to zero');
    expect(s.cell('Service', { spec: { serviceName: 'db' } })).toBe('db');
    expect(s.cell('Up-to-date', {})).toBe('0');
    expect(s.cell('Ready', {})).toBe('0/0');
    expect(s.cell('Status', {})).toBe('Scaled to zero');
  });
});

describe('daemonset columns', () => {
  const ds = kind('apps/v1', 'daemonsets');

  it('counts its nodes as kubectl does, with no spec.replicas', () => {
    expect(ds.visible).toEqual(['Status', 'Desired', 'Current', 'Ready', 'Up-to-date', 'Available', 'Node selector']);
    const raw = {
      spec: { template: { spec: { nodeSelector: { 'kubernetes.io/os': 'linux' } } } },
      status: {
        desiredNumberScheduled: 3,
        currentNumberScheduled: 3,
        numberReady: 2,
        updatedNumberScheduled: 1,
        numberAvailable: 2,
      },
    };
    expect(ds.visible.map((h) => ds.cell(h, raw))).toEqual([
      'Not ready',
      '3',
      '3',
      '2',
      '1',
      '2',
      'kubernetes.io/os=linux',
    ]);
    expect(ds.visible.slice(1, 6).map((h) => ds.cell(h, {}))).toEqual(['0', '0', '0', '0', '0']);
    expect(ds.cell('Status', {})).toBe('No nodes');
    expect(ds.tone({})).toBe('ok');
    expect(ds.cell('Node selector', {})).toBe('—');
  });
});

describe('replicaset and replicationcontroller columns', () => {
  const rs = kind('apps/v1', 'replicasets');
  const rc = kind('v1', 'replicationcontrollers');

  it('counts desired, current and ready', () => {
    expect(rs.visible).toEqual(['Status', 'Desired', 'Current', 'Ready']);
    const raw = { spec: { replicas: 2 }, status: { replicas: 2, readyReplicas: 2 } };
    expect(rs.visible.map((h) => rs.cell(h, raw))).toEqual(['Ready', '2', '2', '2']);
    expect(rs.visible.slice(1).map((h) => rs.cell(h, {}))).toEqual(['0', '0', '0']);
  });

  it('reads a replica failure as failed', () => {
    const raw = { spec: { replicas: 2 }, status: { conditions: [{ type: 'ReplicaFailure', status: 'True' }] } };
    expect(rs.cell('Status', raw)).toBe('Failed');
    expect(rc.tone(raw)).toBe('failing');
  });

  it('reads one with no desired count as scaled to zero', () => {
    expect(rs.tone({})).toBe('ok');
  });

  it('reads a replicaset’s selector, and a controller’s plain map', () => {
    expect(rs.cell('Selector', { spec: { selector: { matchLabels: { app: 'web' } } } })).toBe('app=web');
    expect(rc.cell('Selector', { spec: { selector: { app: 'web', tier: 'front' } } })).toBe('app=web,tier=front');
    expect(rc.cell('Selector', {})).toBe('—');
  });
});

describe('job columns', () => {
  const j = kind('batch/v1', 'jobs');
  afterEach(() => vi.useRealTimers());

  it('shows status, completions and duration', () => {
    expect(j.visible).toEqual(['Status', 'Completions', 'Duration']);
    expect(j.hidden).toEqual(['Active', 'Failed', 'Containers', 'Images', 'Selector']);
  });

  it('reads its status in kubectl’s order', () => {
    const cond = (...types: string[]) => ({ status: { conditions: types.map((type) => ({ type, status: 'True' })) } });
    const cases: [unknown, string, string][] = [
      [cond('Complete', 'Failed'), 'Complete', 'ok'],
      [cond('Failed'), 'Failed', 'failing'],
      [{ ...cond('FailureTarget'), metadata: { deletionTimestamp: '2026-01-01T00:00:00Z' } }, 'Terminating', 'pending'],
      [cond('FailureTarget'), 'FailureTarget', 'failing'],
      [cond('SuccessCriteriaMet'), 'SuccessCriteriaMet', 'ok'],
      [cond('Suspended'), 'Suspended', 'pending'],
      [{}, 'Running', 'pending'],
    ];
    for (const [raw, label, tone] of cases) {
      expect(j.cell('Status', raw)).toBe(label);
      expect(j.tone(raw)).toBe(tone);
    }
  });

  it('counts completions of a fixed count, a work queue and a single pod', () => {
    expect(j.cell('Completions', { spec: { completions: 3 }, status: { succeeded: 1 } })).toBe('1/3');
    expect(j.cell('Completions', { spec: { parallelism: 4 }, status: { succeeded: 2 } })).toBe('2/1 of 4');
    expect(j.cell('Completions', {})).toBe('0/1');
    expect(sortOf(j.col('Completions'), objectWith({ spec: { completions: 4 }, status: { succeeded: 1 } }))).toBe(0.25);
    expect(sortOf(j.col('Completions'), objectWith({}))).toBe(0);
  });

  it('times a finished job to its completion and a running one to now', () => {
    const finished = { status: { startTime: '2026-01-01T00:00:00Z', completionTime: '2026-01-01T00:05:30Z' } };
    expect(j.cell('Duration', finished)).toBe('5m30s');
    expect(sortOf(j.col('Duration'), objectWith(finished))).toBe(330);
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-01-01T00:00:42Z'));
    expect(j.cell('Duration', { status: { startTime: '2026-01-01T00:00:00Z' } })).toBe('42s');
  });

  it('grows a running job’s duration with the clock, with no new frame', () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse('2026-01-01T00:00:42Z'));
    const running = objectWith({ status: { startTime: '2026-01-01T00:00:00Z' } });
    const { container } = render(<>{j.col('Duration').cell(running)}</>);
    expect(container).toHaveTextContent('42s');
    act(() => {
      vi.advanceTimersByTime(10 * 60_000);
    });
    expect(container).toHaveTextContent('10m');
  });

  it('runs no clock for a finished job', () => {
    vi.useFakeTimers();
    const finished = { status: { startTime: '2026-01-01T00:00:00Z', completionTime: '2026-01-01T00:05:30Z' } };
    render(<>{j.col('Duration').cell(objectWith(finished))}</>);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('dashes a duration it cannot read', () => {
    expect(j.cell('Duration', {})).toBe('—');
    expect(j.cell('Duration', { status: { startTime: '2026-01-01T00:00:00Z', completionTime: 'soon' } })).toBe('—');
  });

  it('counts active and failed pods', () => {
    expect(j.cell('Active', { status: { active: 2 } })).toBe('2');
    expect(j.cell('Failed', {})).toBe('0');
    expect(j.cell('Active', {})).toBe('0');
  });
});

describe('pod template columns', () => {
  const withTemplate = {
    spec: { template, selector: { matchLabels: { app: 'web' } }, jobTemplate: { spec: { template } } },
  };

  it('reads every workload’s containers and images off its template', () => {
    for (const [apiVersion, resource] of [
      ['apps/v1', 'statefulsets'],
      ['apps/v1', 'daemonsets'],
      ['apps/v1', 'replicasets'],
      ['v1', 'replicationcontrollers'],
      ['batch/v1', 'jobs'],
      ['batch/v1', 'cronjobs'],
    ]) {
      const k = kind(apiVersion, resource);
      expect(['Containers', 'Images'].map((h) => k.cell(h, withTemplate))).toEqual(['web, proxy', 'web:1, proxy:2']);
    }
  });

  it('reads a daemonset’s and a job’s selector', () => {
    expect(kind('apps/v1', 'daemonsets').cell('Selector', withTemplate)).toBe('app=web');
    expect(kind('batch/v1', 'jobs').cell('Selector', withTemplate)).toBe('app=web');
  });

  it('reads an expression with no values as an empty set', () => {
    const raw = { spec: { selector: { matchExpressions: [{ key: 'tier', operator: 'In' }] } } };
    expect(kind('apps/v1', 'deployments').cell('Selector', raw)).toBe('tier in ()');
  });
});

describe('cronjob columns', () => {
  const c = kind('batch/v1', 'cronjobs');

  it('reads its schedule, suspension, active jobs and last schedule', () => {
    expect(c.visible).toEqual(['Schedule', 'Suspend', 'Active', 'Last schedule']);
    expect(c.hidden).toEqual(['Time zone', 'Last success', 'Containers', 'Images', 'Selector']);
    const raw = {
      spec: {
        schedule: '*/5 * * * *',
        timeZone: 'Etc/UTC',
        suspend: true,
        jobTemplate: { spec: { template, selector: { matchLabels: { job: 'x' } } } },
      },
      status: { active: [{}, {}] },
    };
    expect(['Schedule', 'Suspend', 'Active', 'Time zone', 'Images', 'Selector'].map((h) => c.cell(h, raw))).toEqual([
      '*/5 * * * *',
      'True',
      '2',
      'Etc/UTC',
      'web:1, proxy:2',
      'job=x',
    ]);
    expect(['Suspend', 'Active', 'Last schedule'].map((h) => c.cell(h, {}))).toEqual(['False', '0', '—']);
    const success = new Date(Date.now() - 5 * 60_000).toISOString();
    expect(c.cell('Last success', { status: { lastSuccessfulTime: success } })).toBe('5m');
    expect(toneForKind({ apiVersion: 'batch/v1', resource: 'cronjobs' })).toBeUndefined();
  });
});

describe('horizontalpodautoscaler columns', () => {
  const hpa = kind('autoscaling/v2', 'horizontalpodautoscalers');

  it('reads its reference, bounds and replicas', () => {
    expect(hpa.visible).toEqual(['Reference', 'Targets', 'Min pods', 'Max pods', 'Replicas']);
    expect(hpa.hidden).toEqual(['Desired']);
    const raw = {
      spec: { scaleTargetRef: { kind: 'Deployment', name: 'web' }, minReplicas: 2, maxReplicas: 10 },
      status: { currentReplicas: 3, desiredReplicas: 4 },
    };
    expect(hpa.columns.map((col) => cellText(col, objectWith(raw)))).toEqual([
      'Deployment/web',
      '—',
      '2',
      '10',
      '3',
      '4',
    ]);
    expect(['Reference', 'Min pods', 'Max pods'].map((h) => hpa.cell(h, {}))).toEqual(['—', '1', '—']);
  });

  it('reads each metric as current over target, unknown until read', () => {
    const raw = {
      spec: {
        metrics: [
          { type: 'Resource', resource: { name: 'cpu', target: { type: 'Utilization', averageUtilization: 80 } } },
          { type: 'Pods', pods: { metric: { name: 'rps' }, target: { type: 'AverageValue', averageValue: '100' } } },
          { type: 'External', external: { metric: { name: 'queue' }, target: { type: 'Value', value: '30' } } },
          { type: 'Object', object: { target: {} } },
        ],
      },
      status: {
        currentMetrics: [
          { type: 'Resource', resource: { name: 'cpu', current: { averageUtilization: 45 } } },
          { type: 'Pods', pods: { metric: { name: 'rps' }, current: { averageValue: '20' } } },
        ],
      },
    };
    expect(hpa.cell('Targets', raw)).toBe('cpu: 45%/80%, rps: 20/100, queue: <unknown>/30, <unknown>/<unknown>');
    expect(
      hpa.cell('Targets', {
        spec: { metrics: [{ containerResource: { name: 'memory', target: { type: 'Utilization' } } }] },
      }),
    ).toBe('memory: <unknown>/<unknown>');
  });
});

describe('poddisruptionbudget columns', () => {
  const pdb = kind('policy/v1', 'poddisruptionbudgets');

  it('reads its bounds as numbers or percentages, and its allowed disruptions', () => {
    expect(pdb.visible).toEqual(['Min available', 'Max unavailable', 'Allowed disruptions']);
    expect(pdb.hidden).toEqual(['Healthy', 'Selector']);
    const raw = {
      spec: { minAvailable: 2, maxUnavailable: '25%', selector: { matchLabels: { app: 'web' } } },
      status: { disruptionsAllowed: 1, currentHealthy: 3, desiredHealthy: 2 },
    };
    expect(pdb.columns.map((col) => cellText(col, objectWith(raw)))).toEqual(['2', '25%', '1', '3/2', 'app=web']);
    expect(pdb.columns.map((col) => cellText(col, objectWith({})))).toEqual(['—', '—', '0', '0/0', '—']);
  });
});

describe('controllerrevision columns', () => {
  const cr = kind('apps/v1', 'controllerrevisions');

  it('reads its controller and revision', () => {
    const raw = {
      metadata: { ownerReferences: [{ kind: 'StatefulSet', name: 'db', controller: true }] },
      revision: 3,
    };
    expect(cr.visible).toEqual(['Controller', 'Revision']);
    expect(cr.columns.map((col) => cellText(col, objectWith(raw)))).toEqual(['statefulset/db', '3']);
    expect(sortOf(cr.col('Controller'), objectWith(raw))).toBe('statefulset/db');
    expect(cr.columns.map((col) => cellText(col, objectWith({})))).toEqual(['—', '—']);
  });
});

describe('podtemplate columns', () => {
  const pt = kind('v1', 'podtemplates');

  it('reads its containers, images and pod labels', () => {
    expect(pt.visible).toEqual(['Containers', 'Images', 'Pod labels']);
    expect(pt.columns.map((col) => cellText(col, objectWith({ template })))).toEqual([
      'web, proxy',
      'web:1, proxy:2',
      'app=web,tier=front',
    ]);
    expect(pt.columns.map((col) => cellText(col, objectWith({})))).toEqual(['—', '—', '—']);
  });
});
