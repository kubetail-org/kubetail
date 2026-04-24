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

import { formatInTimeZone } from 'date-fns-tz';
import { useCallback } from 'react';

import { usePreferences } from '@/lib/preferences';

export const TimestampFormat = {
  ISO_8601: 'iso8601',
  RFC_3339: 'rfc3339',
  RFC_1123: 'rfc1123',
  UNIX: 'unix',
  UNIX_MS: 'unix_ms',
} as const;

export type TimestampFormatValue = (typeof TimestampFormat)[keyof typeof TimestampFormat];

export const TIMESTAMP_FORMAT_OPTIONS: { value: TimestampFormatValue; label: string; hint: string }[] = [
  { value: TimestampFormat.ISO_8601, label: 'ISO 8601', hint: '2006-01-02T15:04:05.000+00:00' },
  { value: TimestampFormat.RFC_3339, label: 'RFC 3339', hint: '2006-01-02 15:04:05.000+00:00' },
  { value: TimestampFormat.RFC_1123, label: 'RFC 1123', hint: 'Mon, 02 Jan 2006 15:04:05 +0000' },
  { value: TimestampFormat.UNIX, label: 'Unix seconds', hint: '1136214245' },
  { value: TimestampFormat.UNIX_MS, label: 'Unix milliseconds', hint: '1136214245000' },
];

const PATTERNS: Record<TimestampFormatValue, string> = {
  [TimestampFormat.ISO_8601]: "yyyy-MM-dd'T'HH:mm:ss.SSSxxx",
  [TimestampFormat.RFC_3339]: 'yyyy-MM-dd HH:mm:ss.SSSxxx',
  [TimestampFormat.RFC_1123]: 'EEE, dd MMM yyyy HH:mm:ss xx',
  [TimestampFormat.UNIX]: '',
  [TimestampFormat.UNIX_MS]: '',
};

function coerceDate(date: Date | string): Date {
  return date instanceof Date ? date : new Date(date);
}

export function formatTimestamp(date: Date | string, timezone: string, format: string): string {
  const d = coerceDate(date);

  if (format === TimestampFormat.UNIX) {
    return String(Math.floor(d.getTime() / 1000));
  }
  if (format === TimestampFormat.UNIX_MS) {
    return String(d.getTime());
  }

  const pattern = PATTERNS[format as TimestampFormatValue] ?? PATTERNS[TimestampFormat.ISO_8601];
  return formatInTimeZone(d, timezone, pattern);
}

export function useTimestampFormat(): [string, (format: string) => void] {
  const { preferences, updatePreferences } = usePreferences();
  const format = preferences.timestampFormat ?? TimestampFormat.ISO_8601;

  const setFormat = useCallback(
    (next: string) => {
      updatePreferences({ timestampFormat: next });
    },
    [updatePreferences],
  );

  return [format, setFormat];
}
