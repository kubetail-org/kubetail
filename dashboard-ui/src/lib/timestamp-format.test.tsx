// Copyright 2024-2026 Andres Morey
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

import { act, renderHook } from '@testing-library/react';

import { PreferencesProvider } from './preferences';
import { TimestampFormat, formatTimestamp, useTimestampFormat } from './timestamp-format';

function wrapper({ children }: React.PropsWithChildren) {
  return <PreferencesProvider>{children}</PreferencesProvider>;
}

const FIXED_DATE = new Date('2026-04-23T14:35:22.123Z');
const FIXED_EPOCH_MS = FIXED_DATE.getTime();
const FIXED_EPOCH_S = Math.floor(FIXED_EPOCH_MS / 1000);

describe('TimestampFormat', () => {
  it('exposes the supported format constants', () => {
    expect(TimestampFormat.ISO_8601).toBe('iso8601');
    expect(TimestampFormat.RFC_3339).toBe('rfc3339');
    expect(TimestampFormat.RFC_1123).toBe('rfc1123');
    expect(TimestampFormat.UNIX).toBe('unix');
    expect(TimestampFormat.UNIX_MS).toBe('unix_ms');
  });
});

describe('formatTimestamp', () => {
  it('formats ISO 8601 with the T separator and colon offset in UTC', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', TimestampFormat.ISO_8601)).toBe('2026-04-23T14:35:22.123+00:00');
  });

  it('formats RFC 3339 with a space separator and colon offset in UTC', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', TimestampFormat.RFC_3339)).toBe('2026-04-23 14:35:22.123+00:00');
  });

  it('formats RFC 1123 with weekday, month abbreviation, and numeric offset in UTC', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', TimestampFormat.RFC_1123)).toBe('Thu, 23 Apr 2026 14:35:22 +0000');
  });

  it('formats Unix seconds as an integer string', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', TimestampFormat.UNIX)).toBe(String(FIXED_EPOCH_S));
  });

  it('formats Unix milliseconds as an integer string', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', TimestampFormat.UNIX_MS)).toBe(String(FIXED_EPOCH_MS));
  });

  it('honours timezone for ISO 8601 (non-UTC offset)', () => {
    const formatted = formatTimestamp(FIXED_DATE, 'America/New_York', TimestampFormat.ISO_8601);
    expect(formatted).toMatch(/^2026-04-23T\d{2}:35:22\.123-04:00$/);
  });

  it('honours timezone for RFC 1123 (non-UTC offset)', () => {
    const formatted = formatTimestamp(FIXED_DATE, 'America/New_York', TimestampFormat.RFC_1123);
    expect(formatted).toMatch(/^Thu, 23 Apr 2026 \d{2}:35:22 -0400$/);
  });

  it('accepts an ISO date string', () => {
    expect(formatTimestamp('2026-04-23T14:35:22.123Z', 'UTC', TimestampFormat.ISO_8601)).toBe(
      '2026-04-23T14:35:22.123+00:00',
    );
  });

  it('falls back to ISO 8601 for unknown format strings', () => {
    expect(formatTimestamp(FIXED_DATE, 'UTC', 'not-a-format')).toBe('2026-04-23T14:35:22.123+00:00');
  });
});

describe('useTimestampFormat', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to ISO 8601', () => {
    const { result } = renderHook(() => useTimestampFormat(), { wrapper });
    const [format] = result.current;
    expect(format).toBe(TimestampFormat.ISO_8601);
  });

  it('updates the format when setter is called', () => {
    const { result } = renderHook(() => useTimestampFormat(), { wrapper });

    act(() => {
      const [, setFormat] = result.current;
      setFormat(TimestampFormat.RFC_1123);
    });

    const [format] = result.current;
    expect(format).toBe(TimestampFormat.RFC_1123);
  });
});
