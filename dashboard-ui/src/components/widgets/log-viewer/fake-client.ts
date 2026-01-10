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

import type {
  Client,
  Cursor,
  FetchOptions,
  FetchResult,
  LogRecord,
  SubscriptionCallback,
  SubscriptionCancelFunction,
  SubscriptionOptions,
} from './types';

const DEFAULT_INITIAL_LINES = 1000;
const DEFAULT_APPEND_RATE = 0;

const DEFAULT_LIMIT = 10;
const DEFAULT_FETCH_DELAY_MS = 1000;

const DEFAULT_LOREM_MIN_WORDS = 5;
const DEFAULT_LOREM_MAX_WORDS = 20;

interface FakeFetchOptions extends FetchOptions {
  fetchDelayMs?: number;
}

/**
 * Deterministic lorem ipsum generator
 */

/* eslint-disable no-bitwise */
function mulberry32(seed: number): () => number {
  let t = seed >>> 0;
  return () => {
    t += 0x6d2b79f5;
    let r = Math.imul(t ^ (t >>> 15), t | 1);
    r ^= r + Math.imul(r ^ (r >>> 7), r | 61);
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };
}
/* eslint-enable no-bitwise */

const LOREM_WORDS = [
  'lorem',
  'ipsum',
  'dolor',
  'sit',
  'amet',
  'consectetur',
  'adipiscing',
  'elit',
  'sed',
  'do',
  'eiusmod',
  'tempor',
  'incididunt',
  'ut',
  'labore',
  'et',
  'dolore',
  'magna',
  'aliqua',
  'enim',
  'ad',
  'minim',
  'veniam',
  'quis',
  'nostrud',
  'exercitation',
  'ullamco',
  'laboris',
  'nisi',
  'aliquip',
  'ex',
  'ea',
  'commodo',
  'consequat',
];

function generateLorem(seed: number, minWords: number, maxWords: number): string {
  if (minWords > maxWords) {
    throw new Error('minWords must be <= maxWords');
  }

  // Collapse arbitrarily large integers deterministically
  // eslint-disable-next-line no-bitwise
  const normalizedSeed = seed | 0;
  const rng = mulberry32(normalizedSeed);

  const wordCount = minWords + Math.floor(rng() * (maxWords - minWords + 1));

  const words: string[] = [];
  for (let i = 0; i < wordCount; i += 1) {
    const idx = Math.floor(rng() * LOREM_WORDS.length);
    words.push(LOREM_WORDS[idx]);
  }

  // Capitalize first word, add period
  words[0] = words[0][0].toUpperCase() + words[0].slice(1);
  return `${words.join(' ')}.`;
}

/**
 * FakeClient - Represents a fake client
 */

export type FakeClientOptions = {
  initialLines?: number;
  rate?: number;
  fetchDelayMs?: number;
  lorem?: boolean;
};

export class FakeClient implements Client {
  protected firstTS: number | undefined = undefined;

  protected lastTS: number | undefined = undefined;

  private numLines: number;

  private timer: ReturnType<typeof setInterval> | null = null;

  private subscribers: Set<(record: LogRecord) => void> = new Set();

  private fetchDelayMs: number;

  private lorem: boolean;

  /**
   * Constructor
   * @param initialLines - Initial number of lines (default: 1000)
   * @param rate - Lines per second append rate (default: 0)
   * @param fetchDelayMs - Delay in milliseconds for fetch operations (default: 1000ms)
   */
  constructor({
    initialLines = DEFAULT_INITIAL_LINES,
    rate = DEFAULT_APPEND_RATE,
    fetchDelayMs = DEFAULT_FETCH_DELAY_MS,
    lorem = false,
  }: FakeClientOptions) {
    if (initialLines > 0) {
      const now = new Date().getTime();
      this.firstTS = now - initialLines;
      this.lastTS = now - 1;
    }
    this.numLines = initialLines;
    this.fetchDelayMs = fetchDelayMs;
    this.setAppendRate(rate);
    this.lorem = lorem;
  }

  /**
   * fetchSince - Get the first `limit` log entries starting with `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchSince({ cursor, limit = DEFAULT_LIMIT, fetchDelayMs = 0 }: FakeFetchOptions): Promise<FetchResult> {
    let ts = 0;
    if (cursor === 'BEGINNING') {
      ts = 0;
    } else if (cursor) {
      ts = Date.parse(cursor);
    }

    let records: LogRecord[] = [];
    let nextCursor: Cursor | null = null;

    if (this.firstTS && this.lastTS) {
      const startTS = Math.max(ts, this.firstTS);
      const stopTS = Math.min(this.lastTS + 1, startTS + limit);
      records = this.createRecords(startTS, stopTS);
      if (stopTS < this.lastTS) nextCursor = new Date(stopTS).toISOString();
    }

    return new Promise<FetchResult>((resolve) => {
      setTimeout(resolve, fetchDelayMs || this.fetchDelayMs, { records, nextCursor });
    });
  }

  /**
   * fetchUntil - Get the last `limit` log entries ending with the `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchUntil({ cursor, limit = DEFAULT_LIMIT, fetchDelayMs = 0 }: FakeFetchOptions): Promise<FetchResult> {
    const ts = cursor ? Date.parse(cursor) : Infinity;

    let records: LogRecord[] = [];
    let nextCursor: Cursor | null = null;

    if (this.firstTS && this.lastTS) {
      const stopTS = Math.min(ts + 1, this.lastTS + 1);
      const startTS = Math.max(this.firstTS, stopTS - limit);
      records = this.createRecords(startTS, stopTS);
      if (startTS > this.firstTS) nextCursor = new Date(startTS - 1).toISOString();
    }

    return new Promise<FetchResult>((resolve) => {
      setTimeout(resolve, fetchDelayMs || this.fetchDelayMs, { records, nextCursor });
    });
  }

  /**
   * fetchAfter - Get the first `limit` log entries after `curosor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchAfter({ cursor, ...other }: FakeFetchOptions): Promise<FetchResult> {
    let newCursor: Cursor | undefined;
    if (cursor === 'BEGINNING') {
      newCursor = 'BEGINNING';
    } else if (cursor) {
      newCursor = new Date(Date.parse(cursor) + 1).toISOString();
    }
    return this.fetchSince({ cursor: newCursor, ...other });
  }

  /**
   * fetchBefore - Get the last `limit` log entries before `cursor`
   * @param options - Fetch options
   * @returns A promise that resolves to the fetch result
   */
  async fetchBefore({ cursor, ...other }: FakeFetchOptions): Promise<FetchResult> {
    const newCursor = cursor ? new Date(Date.parse(cursor) - 1).toISOString() : undefined;
    return this.fetchUntil({ cursor: newCursor, ...other });
  }

  /**
   * subscribe - Subscribe to new lines
   * @param callback - The function to call with every record
   * @param options - Subscription options
   * @returns cancel - The cancellation function
   */
  subscribe(callback: SubscriptionCallback, options?: SubscriptionOptions): SubscriptionCancelFunction {
    let sendToBuffer = options?.after !== undefined && options.after !== null;
    const buffer: LogRecord[] = [];

    const cb = (record: LogRecord) => {
      if (sendToBuffer) buffer.push(record);
      else callback(record);
    };

    this.subscribers.add(cb);

    if (sendToBuffer) {
      (async () => {
        const result = await this.fetchAfter({ cursor: options?.after, limit: Infinity, fetchDelayMs: 0 });
        // Write results to callback
        result.records.forEach(callback);

        // Get last timestamp
        const lastTS = result.records.length ? Date.parse(result.records[result.records.length - 1].timestamp) : 0;

        // Empty buffer
        while (buffer.length > 0) {
          const record = buffer.shift();
          if (record && Date.parse(record.timestamp) > lastTS) callback(record);
        }

        // Update flag
        sendToBuffer = false;
      })();
    }

    return () => {
      this.subscribers.delete(cb);
    };
  }

  /**
   * setAppendRate - Set append rate
   */
  setAppendRate(rate: number): void {
    if (this.timer !== null) {
      clearInterval(this.timer);
      this.timer = null;
    }

    if (rate > 0) {
      // Use a reasonable interval (100ms min) and batch lines accordingly
      // Larger intervals reduce timer overhead at high rates
      const intervalMs = Math.max(100, 1000 / rate);
      const linesPerTick = Math.max(1, Math.round((rate * intervalMs) / 1000));

      this.timer = setInterval(() => {
        if (!this.firstTS || !this.lastTS) {
          const now = new Date().getTime();
          this.firstTS = now;
          this.lastTS = now - 1;
        }

        // Optimize: skip expensive operations if no subscribers
        if (this.subscribers.size > 0) {
          for (let i = 0; i < linesPerTick; i += 1) {
            this.numLines += 1;
            this.lastTS += 1;
            this.notify(this.createRecord(this.lastTS));
          }
        } else {
          // Just increment the limit without creating strings or notifying
          this.numLines += linesPerTick;
          this.lastTS += linesPerTick;
        }
      }, intervalMs);
    }
  }

  /**
   * getAtPos - Return the record at a given index
   */
  getAtPos(idx: number): LogRecord {
    if (this.firstTS === undefined || this.lastTS === undefined) {
      throw new RangeError('Index out of range');
    }
    let pos = idx;
    if (pos < 0) pos = this.numLines + pos;
    if (pos < 0 || pos >= this.numLines) {
      throw new RangeError('Index out of range');
    }
    return this.createRecord(this.firstTS + pos);
  }

  /**
   * getLineCount - Return total number of lines
   */
  getLineCount(): number {
    return this.numLines;
  }

  /**
   * notify - Send message to subscribers
   */
  protected notify(record: LogRecord): void {
    this.subscribers.forEach((callback) => callback(record));
  }

  /**
   * createRecord - Helper method to generate a record from a timestamp
   */
  protected createRecord(ts: number): LogRecord {
    const firstTS = this.firstTS || 0;
    const timestamp = new Date(ts).toISOString();

    let message = `line ${ts - firstTS}`;
    if (this.lorem) message += ` ${generateLorem(ts, DEFAULT_LOREM_MIN_WORDS, DEFAULT_LOREM_MAX_WORDS)}`;

    return {
      timestamp,
      message,
      cursor: timestamp,
      source: {
        metadata: {
          region: 'region-1',
          zone: 'az-1',
          os: 'linux',
          arch: 'amd64',
          node: 'node-1',
        },
        namespace: 'ns',
        podName: 'pod-1',
        containerName: 'container-1',
      },
    };
  }

  /**
   * createRecords - Helper method to generate a list of LogRecords
   *                 from start/stop timestamps.
   */
  protected createRecords(startTS: number, stopTS: number): LogRecord[] {
    return Array.from({ length: stopTS - startTS }, (_, i) => this.createRecord(startTS + i));
  }
}
