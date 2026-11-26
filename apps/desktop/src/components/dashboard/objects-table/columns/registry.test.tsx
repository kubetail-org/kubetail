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

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import type { ObjectColumn } from './common';
import { columnsForKind, toneForKind } from './registry';
import { cellText, objectWith } from './testutil';

const widgetGVR = { apiVersion: 'example.com/v1', resource: 'widgets' };

describe('columnsForKind', () => {
  it('gives an unregistered kind with no descriptors nothing', () => {
    expect(columnsForKind(widgetGVR, [])).toEqual([]);
  });

  // A hand-written accessor exists because it says what a jsonPath cannot — podStatus reads
  // container state to correct a crash-looping pod that reports phase Running.
  it('prefers the hand-written registry over descriptors', () => {
    const columns = columnsForKind({ apiVersion: 'v1', resource: 'pods' }, [
      { name: 'Phase', type: 'string', jsonPath: '.status.phase', priority: 0 },
    ]);

    expect(columns.filter((c) => !c.hiddenByDefault).map((c) => c.header)).toEqual([
      'Status',
      'Ready',
      'Restarts',
      'Node',
    ]);
  });

  it('builds columns from descriptors in declared order', () => {
    const columns = columnsForKind(widgetGVR, [
      { name: 'Replicas', type: 'integer', jsonPath: '.spec.replicas', priority: 0 },
      { name: 'Phase', type: 'string', jsonPath: '.status.phase', priority: 0 },
    ]);

    expect(columns.map((c) => c.header)).toEqual(['Replicas', 'Phase']);
    // Under the table's fixed layout an unsized column would split the slack with Name.
    columns.forEach((c) => expect(c.width).toBeGreaterThan(0));
    const o = objectWith({ spec: { replicas: 3 }, status: { phase: 'Running' } });
    expect(cellText(columns[0], o)).toBe('3');
    expect(cellText(columns[1], o)).toBe('Running');
  });

  // The layout is fixed and a cell does not wrap, so a long value would run over the next column.
  it('cuts a value to its column, with the whole of it on hover', () => {
    const [column] = columnsForKind(widgetGVR, [
      { name: 'Image', type: 'string', jsonPath: '.spec.image', priority: 0 },
    ]);
    const image = 'registry.example.com/team/widget-controller:v1.2.3';
    const { container } = render(<>{column.cell(objectWith({ spec: { image } }))}</>);
    expect(column.className).toContain('truncate');
    expect(container.querySelector('span')).toHaveAttribute('title', image);
  });

  // A user's choices are kept against the key.
  it('keys a column by its name, wherever the CRD lists it, and a repeated name by its count', () => {
    const phase = { name: 'Phase', type: 'string', jsonPath: '.status.phase', priority: 0 };
    const replicas = { name: 'Replicas', type: 'integer', jsonPath: '.spec.replicas', priority: 0 };
    const keys = (...ds: (typeof phase)[]) => columnsForKind(widgetGVR, ds).map((c) => c.key);
    expect(keys(phase)).toEqual(['printer:Phase']);
    expect(keys(replicas, phase)).toEqual(['printer:Replicas', 'printer:Phase']);
    expect(keys(phase, phase)).toEqual(['printer:Phase', 'printer:Phase#2']);
  });

  // kubectl prints these under -o wide alone.
  it('hides columns above priority 0 until picked', () => {
    const columns = columnsForKind(widgetGVR, [
      { name: 'Phase', type: 'string', jsonPath: '.status.phase', priority: 0 },
      { name: 'Node', type: 'string', jsonPath: '.spec.nodeName', priority: 1 },
    ]);

    expect(columns.map((c) => [c.header, Boolean(c.hiddenByDefault)])).toEqual([
      ['Phase', false],
      ['Node', true],
    ]);
  });

  it('renders a boolean and a missing value', () => {
    const columns = columnsForKind(widgetGVR, [
      { name: 'Paused', type: 'boolean', jsonPath: '.spec.paused', priority: 0 },
      { name: 'Missing', type: 'string', jsonPath: '.spec.nope', priority: 0 },
    ]);

    const o = objectWith({ spec: { paused: false } });
    expect(cellText(columns[0], o)).toBe('false');
    expect(cellText(columns[1], o)).toBe('—');
  });

  // A path resolving to a container renders nothing rather than "[object Object]".
  it('renders a non-scalar as absent', () => {
    const columns = columnsForKind(widgetGVR, [{ name: 'Spec', type: 'string', jsonPath: '.spec', priority: 0 }]);

    expect(cellText(columns[0], objectWith({ spec: { replicas: 3 } }))).toBe('—');
  });

  it('renders a date as its age, and a date that does not parse as absent', () => {
    const columns = columnsForKind(widgetGVR, [
      { name: 'Started', type: 'date', jsonPath: '.status.startedAt', priority: 0 },
    ]);

    const started = new Date(Date.now() - 5 * 60_000).toISOString();
    expect(cellText(columns[0], objectWith({ status: { startedAt: started } }))).toMatch(/5 minutes ago/);
    expect(cellText(columns[0], objectWith({ status: { startedAt: 'soon' } }))).toBe('—');
  });

  it('survives a partial body, which a Deleted row carries', () => {
    const columns = columnsForKind(widgetGVR, [
      { name: 'Replicas', type: 'integer', jsonPath: '.spec.replicas', priority: 0 },
    ]);

    expect(cellText(columns[0], objectWith(undefined))).toBe('—');
  });
});

describe('sort values', () => {
  const sortOf = (c: ObjectColumn, o: ClusterCachedDataObject) => c.sortValue?.(o);

  it('sorts a declared column by its type, a missing value as none', () => {
    const [count, when, phase] = columnsForKind(widgetGVR, [
      { name: 'Count', type: 'integer', jsonPath: '.spec.count', priority: 0 },
      { name: 'When', type: 'date', jsonPath: '.status.when', priority: 0 },
      { name: 'Phase', type: 'string', jsonPath: '.status.ok', priority: 0 },
    ]);
    const o = objectWith({ spec: { count: '7' }, status: { when: '1970-01-01T00:00:01Z', ok: true } });
    expect([count, when, phase].map((c) => sortOf(c, o))).toEqual([7, -1000, 'true']);
    expect([count.descFirst, when.descFirst]).toEqual([true, false]);
    expect([count, when, phase].map((c) => sortOf(c, objectWith({})))).toEqual([undefined, undefined, undefined]);
    expect(sortOf(when, objectWith({ status: { when: 'soon' } }))).toBeUndefined();
  });
});

describe('toneForKind', () => {
  it('gives a kind without a status no tone', () => {
    expect(toneForKind(widgetGVR)).toBeUndefined();
  });
});
