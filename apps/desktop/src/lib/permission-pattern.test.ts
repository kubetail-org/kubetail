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

import { describe, expect, it } from 'vitest';

import { literal, parseScope, scopeFor } from './permission-pattern';

describe('literal', () => {
  it('escapes each character a pattern reads, and nothing else', () => {
    expect(literal('dev')).toBe('dev');
    expect(literal('dev*')).toBe('dev\\*');
    expect(literal('a?b')).toBe('a\\?b');
    expect(literal('back\\slash')).toBe('back\\\\slash');
    expect(literal('a/b')).toBe('a/b');
    expect(literal('')).toBe('');
  });
});

describe('scopeFor', () => {
  it.each([
    ['an account and no place is it and everything under it', { account: 'c1', place: '' }, 'c1/**'],
    ['an account and its own objects alone is the account', { account: 'c1', place: '', accountOnly: true }, 'c1'],
    ['an account and a namespace pattern', { account: 'c1', place: 'team-*' }, 'c1/team-*'],
    ['no account and no place is every account', { account: null, place: '' }, '*'],
    ['no account and a pattern', { account: null, place: 'kube-system' }, '*/kube-system'],
    ['no account, its own objects alone, is every account', { account: null, place: '', accountOnly: true }, '*'],
    ['no account keeps its pattern', { account: null, place: 'kube-system', accountOnly: true }, '*/kube-system'],
    ['a cloud account and a region', { account: '123456789012', place: 'eu-west-1' }, '123456789012/eu-west-1'],
    ['an account with pattern characters is a literal', { account: 'dev*', place: '' }, 'dev\\*/**'],
  ])('%s', (_, choice, want) => {
    expect(scopeFor(choice)).toBe(want);
  });
});

describe('parseScope', () => {
  it.each([
    ['c1/**', { account: 'c1', place: '', accountOnly: false }],
    ['c1', { account: 'c1', place: '', accountOnly: true }],
    ['c1/team-*', { account: 'c1', place: 'team-*', accountOnly: false }],
    ['*', { account: null, place: '', accountOnly: false }],
    ['*/kube-system', { account: null, place: 'kube-system', accountOnly: false }],
    ['dev\\*/**', { account: 'dev*', place: '', accountOnly: false }],
  ])('reads %s back as the choice that writes it', (scope, want) => {
    expect(parseScope(scope)).toEqual(want);
    expect(scopeFor(want)).toBe(scope);
  });

  it.each(['**', 'c1/a/b', 'de?/x', '*/**', 'c1/**/x', 'c1\\', '\\a', 'c1/'])(
    'reads %s as no choice the form can draw',
    (scope) => {
      expect(parseScope(scope)).toBeNull();
    },
  );
});
