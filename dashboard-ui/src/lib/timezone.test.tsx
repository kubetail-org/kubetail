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

import { act, renderHook } from '@testing-library/react';

import { PreferencesProvider } from './preferences';
import { formatTimezoneOffset, TIMEZONES, useTimezone } from './timezone';

function wrapper({ children }: React.PropsWithChildren) {
  return <PreferencesProvider>{children}</PreferencesProvider>;
}

describe('useTimezone', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to UTC', () => {
    const { result } = renderHook(() => useTimezone(), { wrapper });
    const [timezone] = result.current;
    expect(timezone).toBe('UTC');
  });

  it('updates the timezone when setter is called', () => {
    const { result } = renderHook(() => useTimezone(), { wrapper });

    act(() => {
      const [, setTimezone] = result.current;
      setTimezone('America/New_York');
    });

    const [timezone] = result.current;
    expect(timezone).toBe('America/New_York');
  });
});

describe('TIMEZONES', () => {
  it('is a non-empty array of strings', () => {
    expect(Array.isArray(TIMEZONES)).toBe(true);
    expect(TIMEZONES.length).toBeGreaterThan(0);
    expect(typeof TIMEZONES[0]).toBe('string');
  });

  it('includes UTC', () => {
    expect(TIMEZONES).toContain('UTC');
  });

  it('includes common IANA timezones', () => {
    expect(TIMEZONES).toContain('America/New_York');
    expect(TIMEZONES).toContain('Europe/London');
    expect(TIMEZONES).toContain('Asia/Tokyo');
  });
});

describe('formatTimezoneOffset', () => {
  it('returns "+00:00" for UTC', () => {
    expect(formatTimezoneOffset('UTC')).toBe('+00:00');
  });

  it('returns a signed offset string for non-UTC timezones', () => {
    const offset = formatTimezoneOffset('America/New_York');
    expect(offset).toMatch(/^[+-]\d{2}:\d{2}$/);
  });
});
