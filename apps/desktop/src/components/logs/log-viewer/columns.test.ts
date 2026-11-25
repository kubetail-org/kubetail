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
import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  COLUMNS,
  DEFAULT_COLUMNS,
  DOT_PALETTE,
  dotColor,
  readColumns,
  useLogViewerColumns,
  writeColumns,
} from './columns';

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('the columns', () => {
  it('are in kubetail’s order, the message last', () => {
    expect(COLUMNS).toEqual([
      'Timestamp',
      'Dot',
      'Pod',
      'Container',
      'Node',
      'Region',
      'Zone',
      'OS',
      'Arch',
      'Message',
    ]);
  });

  it('default to the time, the dot and the message', () => {
    expect(DEFAULT_COLUMNS).toEqual(['Timestamp', 'Dot', 'Message']);
  });
});

describe('readColumns', () => {
  it('reads nothing stored as the default', () => {
    expect(readColumns(null)).toEqual(DEFAULT_COLUMNS);
  });

  it('reads a value that does not parse, or is not a list, as the default', () => {
    expect(readColumns('{oops')).toEqual(DEFAULT_COLUMNS);
    expect(readColumns('"Pod"')).toEqual(DEFAULT_COLUMNS);
  });

  it('drops a name it does not know', () => {
    expect(readColumns('["Pod","Namespace",3]')).toEqual(['Pod', 'Message']);
  });

  it('reads a set out of order in column order, the message always last', () => {
    expect(readColumns('["Node","Message","Timestamp"]')).toEqual(['Timestamp', 'Node', 'Message']);
  });

  it('round-trips what writeColumns stores, which leaves the message out', () => {
    const stored = writeColumns(['Timestamp', 'Pod', 'Message']);
    expect(JSON.parse(stored)).toEqual(['Timestamp', 'Pod']);
    expect(readColumns(stored)).toEqual(['Timestamp', 'Pod', 'Message']);
  });
});

describe('dotColor', () => {
  it('is stable for one container and always a palette colour', () => {
    const a = dotColor('prod', 'webapp-7f9c', 'app');
    expect(dotColor('prod', 'webapp-7f9c', 'app')).toBe(a);
    expect(DOT_PALETTE).toContain(a);
  });

  it('spreads containers over the palette', () => {
    const colors = new Set(Array.from({ length: 200 }, (_, i) => dotColor('prod', `pod-${i}`, 'app')));
    expect(colors.size).toBeGreaterThan(DOT_PALETTE.length / 2);
  });
});

describe('useLogViewerColumns', () => {
  it('opens with what the last window left, and keeps what the user picks', () => {
    localStorage.setItem('kubetail:log-viewer-columns', '["Pod"]');
    const { result } = renderHook(() => useLogViewerColumns());
    expect(result.current[0]).toEqual(['Pod', 'Message']);

    act(() => result.current[1](['Timestamp', 'Node', 'Message']));
    expect(result.current[0]).toEqual(['Timestamp', 'Node', 'Message']);
    expect(localStorage.getItem('kubetail:log-viewer-columns')).toBe('["Timestamp","Node"]');
  });

  it('opens with the default, and still changes, when storage refuses', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const { result } = renderHook(() => useLogViewerColumns());
    expect(result.current[0]).toEqual(DEFAULT_COLUMNS);
    act(() => result.current[1](['Pod', 'Message']));
    expect(result.current[0]).toEqual(['Pod', 'Message']);
  });
});
