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

import { FakeClient } from './fake-client';
import type { LogRecord } from './types';

const INITIAL_LINE_COUNT = 100;

function getMessages(records: LogRecord[]) {
  return records.map((r) => r.message);
}

describe('FakeClient', () => {
  let client: FakeClient;

  beforeEach(() => {
    client = new FakeClient({ initialLines: INITIAL_LINE_COUNT, rate: 0, fetchDelayMs: 10 });
  });

  describe('fetchSince', () => {
    it('should fetch N entries starting with the first record when cursor is empty (inclusive)', async () => {
      const result = await client.fetchSince({ limit: 3 });
      expect(getMessages(result.records)).toEqual(['line 0', 'line 1', 'line 2']);
      expect(result.nextCursor).toEqual(client.getAtPos(3).cursor);
    });

    it('should fetch N entries starting at the given cursor (inclusive)', async () => {
      const { cursor } = client.getAtPos(3);
      const result = await client.fetchSince({ cursor, limit: 3 });
      expect(getMessages(result.records)).toEqual(['line 3', 'line 4', 'line 5']);
      expect(result.nextCursor).toEqual(client.getAtPos(6).cursor);
    });

    it('should handle fetching at the end of available lines', async () => {
      const { cursor } = client.getAtPos(-2);
      const result = await client.fetchSince({ cursor, limit: 5 });
      // Should only get the last 2 lines
      expect(getMessages(result.records)).toEqual([`line ${INITIAL_LINE_COUNT - 2}`, `line ${INITIAL_LINE_COUNT - 1}`]);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when cursor is beyond the last line', async () => {
      const lastTS = Date.parse(client.getAtPos(-1).timestamp);
      const cursor = new Date(lastTS + 1).toISOString();
      const result = await client.fetchSince({ cursor, limit: 5 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when limit is 0', async () => {
      const result = await client.fetchSince({ limit: 0 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(client.getAtPos(0).cursor);
    });

    it('should limit results to available lines when limit exceeds available', async () => {
      const lineCount = INITIAL_LINE_COUNT;
      const result = await client.fetchSince({ limit: lineCount + 100 });
      expect(result.records.length).toBe(lineCount);
      expect(result.records[0].message).toBe('line 0');
      expect(result.records[result.records.length - 1].message).toBe(`line ${lineCount - 1}`);
      expect(result.nextCursor).toEqual(null);
    });
  });

  describe('fetchUntil', () => {
    it('should fetch N entries ending at the last record when cursor is empty (inclusive)', async () => {
      const result = await client.fetchUntil({ limit: 3 });
      expect(getMessages(result.records)).toEqual([
        `line ${INITIAL_LINE_COUNT - 3}`,
        `line ${INITIAL_LINE_COUNT - 2}`,
        `line ${INITIAL_LINE_COUNT - 1}`,
      ]);
      expect(result.nextCursor).toEqual(client.getAtPos(-4).cursor);
    });

    it('should fetch N entries ending at the given cursor (inclusive)', async () => {
      const { cursor } = client.getAtPos(9);
      const result = await client.fetchUntil({ cursor, limit: 3 });
      expect(getMessages(result.records)).toEqual(['line 7', 'line 8', 'line 9']);
      expect(result.nextCursor).toEqual(client.getAtPos(6).cursor);
    });

    it('should handle fetching at the beginning of available lines', async () => {
      const { cursor } = client.getAtPos(2);
      const result = await client.fetchUntil({ cursor, limit: 5 });
      // Should only get lines 0, 1, 2 (idx is inclusive)
      expect(getMessages(result.records)).toEqual(['line 0', 'line 1', 'line 2']);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when cursor < firstTS', async () => {
      const firstTS = Date.parse(client.getAtPos(0).timestamp);
      const cursor = new Date(firstTS - 1).toISOString();
      const result = await client.fetchUntil({ cursor, limit: 5 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when limit is 0', async () => {
      const result = await client.fetchUntil({ limit: 0 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(client.getAtPos(-1).cursor);
    });

    it('should limit results to available lines when count exceeds available', async () => {
      const lineCount = INITIAL_LINE_COUNT;
      const result = await client.fetchUntil({ limit: 1000 });
      // Should get lines 0 through 49
      expect(result.records.length).toBe(INITIAL_LINE_COUNT);
      expect(result.records[0].message).toBe('line 0');
      expect(result.records[result.records.length - 1].message).toBe(`line ${lineCount - 1}`);
      expect(result.nextCursor).toEqual(null);
    });
  });

  describe('fetchSince and fetchUntil symmetry', () => {
    it('should allow navigation through the log using both methods', async () => {
      const r1 = await client.fetchSince({ limit: Infinity });
      const r2 = await client.fetchUntil({ limit: Infinity });
      expect(r1.records).toEqual(r2.records);
    });
  });

  describe('fetchAfter', () => {
    it('should fetch entries starting with the first entry when cursor is empty', async () => {
      const result = await client.fetchAfter({ limit: 5 });
      expect(getMessages(result.records)).toEqual(['line 0', 'line 1', 'line 2', 'line 3', 'line 4']);
      expect(result.nextCursor).toEqual(client.getAtPos(5).cursor);
    });

    it('should fetch N entries after the given cursor (exclusive)', async () => {
      const { cursor } = client.getAtPos(0);
      const result = await client.fetchAfter({ cursor, limit: 3 });
      expect(getMessages(result.records)).toEqual(['line 1', 'line 2', 'line 3']);
      expect(result.nextCursor).toEqual(client.getAtPos(4).cursor);
    });

    it('should handle fetching at the end of available records', async () => {
      const lastTS = Date.parse(client.getAtPos(-1).timestamp);
      const cursor = new Date(lastTS - 1).toISOString();
      const result = await client.fetchAfter({ cursor, limit: 5 });
      // Should only get the last line
      expect(getMessages(result.records)).toEqual([`line ${INITIAL_LINE_COUNT - 1}`]);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when cursor is at or beyond the last record', async () => {
      const { cursor } = client.getAtPos(-1);
      const result = await client.fetchAfter({ cursor, limit: 5 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(null);
    });
  });

  describe('fetchBefore', () => {
    it('should fetch entries ending at the last line when cursor is empty', async () => {
      const lineCount = INITIAL_LINE_COUNT;
      const result = await client.fetchBefore({ limit: 5 });
      expect(getMessages(result.records)).toEqual([
        `line ${lineCount - 5}`,
        `line ${lineCount - 4}`,
        `line ${lineCount - 3}`,
        `line ${lineCount - 2}`,
        `line ${lineCount - 1}`,
      ]);
      expect(result.nextCursor).toEqual(client.getAtPos(-6).cursor);
    });

    it('should fetch N entries before the given cursor (exclusive)', async () => {
      const { cursor } = client.getAtPos(10);
      const result = await client.fetchBefore({ cursor, limit: 3 });
      expect(getMessages(result.records)).toEqual(['line 7', 'line 8', 'line 9']);
      expect(result.nextCursor).toEqual(client.getAtPos(6).cursor);
    });

    it('should handle fetching at the beginning of available records', async () => {
      const { cursor } = client.getAtPos(3);
      const result = await client.fetchBefore({ cursor, limit: 5 });
      // Should only get lines 0, 1, 2 (3 is exclusive)
      expect(getMessages(result.records)).toEqual(['line 0', 'line 1', 'line 2']);
      expect(result.nextCursor).toEqual(null);
    });

    it('should return empty array when cursor is at or before the first cursor', async () => {
      const { cursor } = client.getAtPos(0);
      const result = await client.fetchBefore({ cursor, limit: 5 });
      expect(result.records).toEqual([]);
      expect(result.nextCursor).toEqual(null);
    });
  });

  describe('subscribe', () => {
    it('should forward records immediately when options is undefined', () => {
      const received: string[] = [];
      client.subscribe((record) => received.push(record.message));
      (client as any).notify({ timestamp: 1, message: 'line 1' });
      expect(received).toEqual(['line 1']);
    });

    it('should forward records immediately when options.after is undefined', () => {
      const received: string[] = [];
      client.subscribe((record) => received.push(record.message), { after: undefined });
      (client as any).notify({ timestamp: 1, message: 'line 1' });
      expect(received).toEqual(['line 1']);
    });

    it('should forward records immediately when options.after is null', () => {
      const received: string[] = [];
      client.subscribe((record) => received.push(record.message), { after: null });
      (client as any).notify({ timestamp: 1, message: 'line 1' });
      expect(received).toEqual(['line 1']);
    });

    it('should replay all records when options.after is set to BEGINNING', async () => {
      vi.useFakeTimers();
      const now = new Date('2020-01-01T00:00:01.000Z').getTime();
      vi.setSystemTime(now);
      client = new FakeClient({ initialLines: 5, rate: 0, fetchDelayMs: 0 });

      const received: string[] = [];
      client.subscribe((record) => received.push(record.message), { after: 'BEGINNING' });

      await vi.runAllTimersAsync();

      expect(received).toEqual(['line 0', 'line 1', 'line 2', 'line 3', 'line 4']);
      vi.useRealTimers();
    });

    it('should forward records immediately when options.after is set to BEGINNING', async () => {
      vi.useFakeTimers();
      const now = new Date('2020-01-01T00:00:01.000Z').getTime();
      vi.setSystemTime(now);
      client = new FakeClient({ initialLines: 0, rate: 0, fetchDelayMs: 0 });

      const received: string[] = [];
      client.subscribe((record) => received.push(record.message), { after: 'BEGINNING' });

      await vi.runAllTimersAsync();

      const timestamp1 = new Date(now + 1).toISOString();
      (client as any).notify({
        timestamp: timestamp1,
        message: 'live 0',
        cursor: timestamp1,
      });

      expect(received).toEqual(['live 0']);
      vi.useRealTimers();
    });

    it('should forward records immediately when options.after is set to BEGINNING and setAppendRate() is modified', async () => {
      vi.useFakeTimers();
      const now = new Date('2020-01-01T00:00:01.000Z').getTime();
      vi.setSystemTime(now);
      client = new FakeClient({ initialLines: 0, rate: 0, fetchDelayMs: 0 });

      const received: string[] = [];
      client.subscribe((record) => received.push(record.message), { after: 'BEGINNING' });

      await vi.runAllTimersAsync();

      client.setAppendRate(1);

      await vi.advanceTimersByTimeAsync(1000);

      expect(received).toEqual(['line 0']);
      vi.useRealTimers();
    });

    it('should replay fetchAfter results then drain buffered records when options.after is set', async () => {
      vi.useFakeTimers();
      const now = new Date('2020-01-01T00:00:01.000Z').getTime();
      vi.setSystemTime(now);
      client = new FakeClient({ initialLines: 5, rate: 0, fetchDelayMs: 0 });

      const received: string[] = [];
      const after = client.getAtPos(-5).cursor;
      client.subscribe((record) => received.push(record.message), { after });

      const lastTS = Date.parse(client.getAtPos(-1).timestamp);

      const timestamp1 = new Date(lastTS + 1).toISOString();
      (client as any).notify({
        timestamp: timestamp1,
        message: 'buffered',
        cursor: timestamp1,
      });

      await vi.runAllTimersAsync();

      expect(received).toEqual(['line 1', 'line 2', 'line 3', 'line 4', 'buffered']);

      const timestamp2 = new Date(lastTS + 2).toISOString();
      (client as any).notify({
        timestamp: timestamp2,
        message: 'live',
        cursor: timestamp2,
      });

      expect(received).toEqual(['line 1', 'line 2', 'line 3', 'line 4', 'buffered', 'live']);
      vi.useRealTimers();
    });
  });
});
