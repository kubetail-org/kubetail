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
import { beforeEach, describe, expect, it } from 'vitest';

import { storageKey } from '@/lib/storage-key';

import { usePersistedFlag } from './persisted-flag';

// Helpers -------------------------------------------------------------

const renderFlag = (key = 'pane_open', initial = false) => renderHook(() => usePersistedFlag(key, initial));

const renderSwitchable = (key: string) =>
  renderHook(({ name }) => usePersistedFlag(name, false), { initialProps: { name: key } });

beforeEach(() => {
  localStorage.clear();
});

// Tests ---------------------------------------------------------------

describe('usePersistedFlag', () => {
  it('opens at the initial value when nothing is stored', () => {
    expect(renderFlag('pane_open', true).result.current[0]).toBe(true);
    expect(renderFlag('other_open', false).result.current[0]).toBe(false);
  });

  it('prefixes the name it is given, so a call site cannot forget to', () => {
    const { result } = renderFlag();
    act(() => result.current[1](true));
    expect(localStorage.getItem('kubetail:pane_open')).toBe('true');
  });

  it('reads a stored value back, either way', () => {
    localStorage.setItem(storageKey('pane_open'), 'true');
    expect(renderFlag().result.current[0]).toBe(true);

    // A stored `false` is a choice, not an absence: it must beat an initial `true`.
    localStorage.setItem(storageKey('pane_open'), 'false');
    expect(renderFlag('pane_open', true).result.current[0]).toBe(false);
  });

  it('stores what it is given', () => {
    const { result } = renderFlag();
    act(() => result.current[1](true));
    expect(result.current[0]).toBe(true);
    expect(localStorage.getItem(storageKey('pane_open'))).toBe('true');

    act(() => result.current[1](false));
    expect(result.current[0]).toBe(false);
    expect(localStorage.getItem(storageKey('pane_open'))).toBe('false');
  });

  it('reads the new key’s value when the caller switches keys', () => {
    localStorage.setItem(storageKey('pane_a'), 'true');
    const { result, rerender } = renderSwitchable('pane_a');
    expect(result.current[0]).toBe(true);

    // A pane whose state is kept per mode: switching must not carry the old
    // mode's state across.
    rerender({ name: 'pane_b' });
    expect(result.current[0]).toBe(false);
  });

  it('keeps each pane under its own key', () => {
    const { result } = renderFlag('other_open');
    act(() => result.current[1](true));
    expect(localStorage.getItem(storageKey('other_open'))).toBe('true');
    expect(localStorage.getItem(storageKey('pane_open'))).toBeNull();
  });
});
