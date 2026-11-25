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

import { errorMessage, toError } from './error-bus';

describe('toError', () => {
  it('passes an Error through unchanged (preserves the stack)', () => {
    const e = new Error('boom');
    expect(toError(e)).toBe(e);
  });

  it('preserves Error subclasses', () => {
    const e = new TypeError('nope');
    expect(toError(e)).toBe(e);
  });

  it('wraps a non-Error value via String()', () => {
    const e = toError('plain string');
    expect(e).toBeInstanceOf(Error);
    expect(e.message).toBe('plain string');
  });

  it('wraps null/undefined without throwing', () => {
    expect(toError(undefined).message).toBe('undefined');
    expect(toError(null).message).toBe('null');
  });
});

describe('errorMessage', () => {
  it("returns an Error's message (not its 'Error: ' toString)", () => {
    expect(errorMessage(new Error('the message'))).toBe('the message');
  });

  it('stringifies a non-Error value', () => {
    expect(errorMessage('raw')).toBe('raw');
    expect(errorMessage(42)).toBe('42');
  });
});
