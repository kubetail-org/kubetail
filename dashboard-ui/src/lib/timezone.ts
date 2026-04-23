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

import { format, toZonedTime } from 'date-fns-tz';
import { useCallback } from 'react';

import { usePreferences } from '@/lib/preferences';

export function useTimezone(): [string, (tz: string) => void] {
  const { preferences, updatePreferences } = usePreferences();
  const timezone = preferences.timezone ?? 'UTC';

  const setTimezone = useCallback(
    (tz: string) => {
      updatePreferences({ timezone: tz });
    },
    [updatePreferences],
  );

  return [timezone, setTimezone];
}

export function formatTimestamp(date: Date | string, timezone: string): string {
  const zoned = toZonedTime(date, timezone);
  return format(zoned, 'LLL dd, y HH:mm:ss.SSS', { timeZone: timezone });
}

export function formatTimezoneOffset(tz: string): string {
  const now = new Date();
  return format(toZonedTime(now, tz), 'xxx', { timeZone: tz });
}

export const TIMEZONES: string[] = ['UTC', ...Intl.supportedValuesOf('timeZone').filter((tz) => tz !== 'UTC')];
