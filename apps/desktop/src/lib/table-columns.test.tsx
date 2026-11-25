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

import { arrange, readChoices, useTableColumns } from './table-columns';

const PODS = { apiVersion: 'v1', resource: 'pods' };
const KEY = 'kubetail:table-columns:v1/pods';
const NONE = { shown: {}, order: [] };

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('readChoices', () => {
  it('reads what is shown and the order, dropping what is not the right shape', () => {
    expect(readChoices('{"shown":{"node":false,"ip":true,"age":7},"order":["ip",3,"node"]}')).toEqual({
      shown: { node: false, ip: true },
      order: ['ip', 'node'],
    });
    expect(readChoices('{"shown":["node"],"order":"ip"}')).toEqual(NONE);
    expect(readChoices('{"shown":null}')).toEqual(NONE);
  });

  it('reads anything but an object as no choices', () => {
    expect(readChoices(null)).toEqual(NONE);
    expect(readChoices('{')).toEqual(NONE);
    expect(readChoices('null')).toEqual(NONE);
    expect(readChoices('7')).toEqual(NONE);
  });
});

describe('arrange', () => {
  const cols = (...keys: string[]) => keys.map((key) => ({ key }));
  const keys = (cs: { key: string }[]) => cs.map((c) => c.key);

  it('puts the named columns in the order given', () => {
    expect(keys(arrange(cols('a', 'b', 'c'), ['c', 'a', 'b']))).toEqual(['c', 'a', 'b']);
  });

  it('keeps a column the order lacks in its default place, and drops a name no column has', () => {
    expect(keys(arrange(cols('a', 'new', 'b', 'c'), ['c', 'gone', 'b', 'a']))).toEqual(['c', 'new', 'b', 'a']);
  });

  it('leaves the default order with no order', () => {
    expect(keys(arrange(cols('a', 'b'), []))).toEqual(['a', 'b']);
  });
});

describe('useTableColumns', () => {
  it('keeps what is shown and the order, per kind', () => {
    const { result } = renderHook(() => useTableColumns(PODS));
    act(() => result.current.setShown('node', false));
    act(() => result.current.setShown('ip', true));
    act(() => result.current.setOrder(['ip', 'node']));
    expect(result.current.choices).toEqual({ shown: { node: false, ip: true }, order: ['ip', 'node'] });
    expect(JSON.parse(localStorage.getItem(KEY)!)).toEqual(result.current.choices);
    expect(
      renderHook(() => useTableColumns({ apiVersion: 'apps/v1', resource: 'deployments' })).result.current.choices,
    ).toEqual(NONE);
  });

  it('reads what an earlier mount kept', () => {
    localStorage.setItem(KEY, '{"shown":{"age":false},"order":["age"]}');
    expect(renderHook(() => useTableColumns(PODS)).result.current.choices).toEqual({
      shown: { age: false },
      order: ['age'],
    });
  });

  it('resets to the defaults, and forgets the stored choices', () => {
    const { result } = renderHook(() => useTableColumns(PODS));
    act(() => result.current.setShown('node', false));
    act(() => result.current.reset());
    expect(result.current.choices).toEqual(NONE);
    expect(localStorage.getItem(KEY)).toBeNull();
  });

  it('keeps the choices in state when storage refuses', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied');
    });
    const { result } = renderHook(() => useTableColumns(PODS));
    expect(result.current.choices).toEqual(NONE);
    act(() => result.current.setShown('node', false));
    expect(result.current.choices.shown).toEqual({ node: false });
  });
});
