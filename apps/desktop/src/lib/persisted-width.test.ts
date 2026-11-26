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

import { usePersistedWidth } from './persisted-width';

// Helpers -------------------------------------------------------------

const BOUNDS = { min: 100, max: 400, initial: 200 };

const renderWidth = (key = 'pane_width') => renderHook(() => usePersistedWidth(key, BOUNDS));

const renderSwitchable = (key: string) =>
  renderHook(({ name }) => usePersistedWidth(name, BOUNDS), { initialProps: { name: key } });

beforeEach(() => {
  localStorage.clear();
});

// Tests ---------------------------------------------------------------

describe('usePersistedWidth', () => {
  it('opens at the initial width when nothing is stored', () => {
    const { result } = renderWidth();
    expect(result.current[0]).toBe(200);
  });

  it('prefixes the name it is given, so a call site cannot forget to', () => {
    const { result } = renderWidth();
    act(() => result.current[1](300));
    expect(localStorage.getItem('kubetail:pane_width')).toBe('300');
  });

  it('reads the stored width back', () => {
    localStorage.setItem(storageKey('pane_width'), '250');
    const { result } = renderWidth();
    expect(result.current[0]).toBe(250);
  });

  it('clamps what it reads, so a stored width outlives a change of bounds', () => {
    localStorage.setItem(storageKey('pane_width'), '9999');
    const { result } = renderWidth();
    expect(result.current[0]).toBe(400);
  });

  it('bounds only the minimum when the maximum is Infinity', () => {
    localStorage.setItem(storageKey('pane_width'), '9999');
    const { result } = renderHook(() => usePersistedWidth('pane_width', { ...BOUNDS, max: Infinity }));
    expect(result.current[0]).toBe(9999);

    act(() => result.current[1](12000));
    expect(result.current[0]).toBe(12000);
    act(() => result.current[1](10));
    expect(result.current[0]).toBe(100);
  });

  it('clamps and stores what it is given', () => {
    const { result } = renderWidth();
    act(() => result.current[1](300));
    expect(result.current[0]).toBe(300);
    expect(localStorage.getItem(storageKey('pane_width'))).toBe('300');

    act(() => result.current[1](10));
    expect(result.current[0]).toBe(100);
    expect(localStorage.getItem(storageKey('pane_width'))).toBe('100');
  });

  it('reads the new key’s width when the caller switches keys', () => {
    localStorage.setItem(storageKey('pane_a'), '150');
    localStorage.setItem(storageKey('pane_b'), '350');
    const { result, rerender } = renderSwitchable('pane_a');
    expect(result.current[0]).toBe(150);

    // A pane whose width is kept per mode: switching must not carry the old
    // mode's width across.
    rerender({ name: 'pane_b' });
    expect(result.current[0]).toBe(350);
  });

  it('falls back to the initial width for a key nothing is stored under', () => {
    localStorage.setItem(storageKey('pane_a'), '150');
    const { result, rerender } = renderSwitchable('pane_a');

    rerender({ name: 'pane_b' });
    expect(result.current[0]).toBe(200);
  });

  it('keeps each pane under its own key', () => {
    localStorage.setItem(storageKey('other_width'), '150');
    const { result } = renderWidth('other_width');
    expect(result.current[0]).toBe(150);

    act(() => result.current[1](260));
    expect(localStorage.getItem(storageKey('other_width'))).toBe('260');
    expect(localStorage.getItem(storageKey('pane_width'))).toBeNull();
  });
});
