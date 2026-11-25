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

import {
  cacheFailureSummary,
  connectionFailureSummary,
  contextHealth,
  overallTone,
  syncReason,
} from '@/lib/cluster-health';
import { clusterRecord } from '@/test-utils';

describe('overallTone', () => {
  it('rolls up to the most severe of the two sub-system tones', () => {
    expect(overallTone('ok', 'ok')).toBe('ok');
    expect(overallTone('ok', 'muted')).toBe('ok');
    expect(overallTone('muted', 'muted')).toBe('muted');
    expect(overallTone('attention', 'muted')).toBe('attention');
    expect(overallTone('ok', 'attention')).toBe('attention');
    expect(overallTone('error', 'attention')).toBe('error');
    expect(overallTone('ok', 'error')).toBe('error');
  });
});

describe('connectionFailureSummary', () => {
  it('names the probe’s class of failure', () => {
    expect(connectionFailureSummary('Unreachable')).toBe('Can’t reach the server');
    expect(connectionFailureSummary('TLSInvalid')).toBe('Server certificate rejected');
    expect(connectionFailureSummary('Unauthorized')).toBe('Credentials rejected');
  });

  it('reads an unclassified, unknown or missing reason as a bare failure', () => {
    expect(connectionFailureSummary('ProbeFailed')).toBe('Connection failed');
    expect(connectionFailureSummary('constructor')).toBe('Connection failed');
    expect(connectionFailureSummary(undefined)).toBe('Connection failed');
  });
});

describe('cacheFailureSummary', () => {
  it('names a failed sync by its class of failure', () => {
    expect(cacheFailureSummary('SyncFailed', 'Forbidden')).toBe('Permission denied');
    expect(cacheFailureSummary('SyncFailed', 'NotFound')).toBe('Resource no longer served');
    expect(cacheFailureSummary('SyncFailed', 'Timeout')).toBe('Server didn’t answer in time');
  });

  it('names a failed sync with no class, or an unknown one, as a failed sync', () => {
    expect(cacheFailureSummary('SyncFailed')).toBe('Some kinds aren’t syncing');
    expect(cacheFailureSummary('SyncFailed', '')).toBe('Some kinds aren’t syncing');
    expect(cacheFailureSummary('SyncFailed', 'Bogus')).toBe('Some kinds aren’t syncing');
  });

  it('names a failed store, and anything else as a cache error', () => {
    expect(cacheFailureSummary('StoreFailed')).toBe('Cache storage failed');
    expect(cacheFailureSummary(undefined)).toBe('Cache error');
  });
});

describe('contextHealth', () => {
  it('reads a context with no record as muted and not reached', () => {
    expect(contextHealth([clusterRecord({ context: 'staging' })], 'prod')).toEqual({
      tone: 'muted',
      summary: 'Not reached',
    });
  });

  it('reads every context so before the clusters snapshot lands', () => {
    expect(contextHealth(null, 'prod')).toEqual({ tone: 'muted', summary: 'Not reached' });
  });

  it('reads a connected, watching record as ok', () => {
    expect(contextHealth([clusterRecord({ context: 'prod' })], 'prod')).toEqual({
      tone: 'ok',
      summary: 'Active · Current',
    });
  });

  it('reads a disconnected record as an error, its sync stalled', () => {
    expect(contextHealth([clusterRecord({ context: 'prod', connected: 'False' })], 'prod')).toEqual({
      tone: 'error',
      summary: 'Disconnected · Stalled',
    });
  });
});

describe('syncReason', () => {
  const health = (over: Record<string, unknown>) => ({
    cacheID: 'cache-u',
    status: 'False' as const,
    reason: 'Watching',
    unhealthyKindRefs: [] as { apiVersion: string; resource: string; reason: string }[],
    failure: '',
    totalKinds: 10,
    unhealthyKinds: 0,
    pausedKinds: 0,
    lastUpdateAt: null,
    lastLiveAt: null,
    ...over,
  });
  const pods = (reason: string) => ({ apiVersion: 'v1', resource: 'pods', reason });
  const deployments = (reason: string) => ({ apiVersion: 'apps/v1', resource: 'deployments', reason });

  it('names the kinds a stale cache is not hearing from', () => {
    const h = health({ reason: 'Stale', unhealthyKindRefs: [pods('Stale'), deployments('Stale')], unhealthyKinds: 2 });
    expect(syncReason({ label: 'Stale', tone: 'attention' }, h)).toBe('pods, deployments not receiving updates.');
  });

  it('calls no kind stale that is only syncing', () => {
    const h = health({
      reason: 'Stale',
      unhealthyKindRefs: [deployments('Stale'), pods('Syncing')],
      unhealthyKinds: 2,
    });
    expect(syncReason({ label: 'Stale', tone: 'attention' }, h)).toBe('deployments not receiving updates.');
  });

  it('blames the watch when no kind behind a stale verdict is named', () => {
    const h = health({ reason: 'Stale', unhealthyKindRefs: [pods('Syncing')], unhealthyKinds: 1 });
    expect(syncReason({ label: 'Stale', tone: 'attention' }, h)).toBe('The watch may have stopped delivering updates.');
  });

  it('blames the watch when a stale cache names no kind', () => {
    const h = health({ reason: 'Stale' });
    expect(syncReason({ label: 'Stale', tone: 'attention' }, h)).toBe('The watch may have stopped delivering updates.');
  });

  it('says a cache whose file will not open', () => {
    const h = health({ reason: 'StoreFailed' });
    expect(syncReason({ label: 'Storage error', tone: 'error' }, h)).toBe('The cache file will not open.');
  });

  it('summarises the failing kinds', () => {
    const h = health({ reason: 'SyncFailed', unhealthyKindRefs: [pods('SyncFailed')], unhealthyKinds: 1 });
    expect(syncReason({ label: 'Error', tone: 'error' }, h)).toBe('9 of 10 kinds — pods not syncing');
  });

  it('counts the paused kinds beside the failing ones', () => {
    const h = health({
      reason: 'SyncFailed',
      unhealthyKindRefs: [pods('SyncFailed')],
      unhealthyKinds: 1,
      pausedKinds: 2,
    });
    expect(syncReason({ label: 'Error', tone: 'error' }, h)).toBe('7 of 10 kinds — pods not syncing, 2 paused');
  });

  it('calls no kind failing that is only syncing', () => {
    const h = health({
      reason: 'SyncFailed',
      unhealthyKindRefs: [deployments('SyncFailed'), pods('Syncing')],
      unhealthyKinds: 2,
    });
    expect(syncReason({ label: 'Error', tone: 'error' }, h)).toBe('8 of 10 kinds — deployments not syncing');
  });

  it('gives no reason for a healthy or muted verdict, or without a rollup', () => {
    const failing = health({ reason: 'SyncFailed', unhealthyKindRefs: [pods('SyncFailed')], unhealthyKinds: 1 });
    expect(syncReason({ label: 'Current', tone: 'ok' }, health({}))).toBeNull();
    expect(syncReason({ label: 'Stalled', tone: 'muted' }, failing)).toBeNull();
    expect(syncReason({ label: 'Degraded', tone: 'attention' }, health({ reason: 'QuotaExceeded' }))).toBeNull();
    expect(syncReason({ label: 'Syncing', tone: 'ok' }, null)).toBeNull();
  });
});
