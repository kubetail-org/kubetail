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

import { isImeKey } from './ime';

describe('isImeKey', () => {
  it('reads a key pressed while an IME composes as its own', () => {
    expect(isImeKey(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true }))).toBe(true);
  });

  it('reads WebKit’s accepting Enter, sent after composing ends with keyCode 229, as its own', () => {
    // oxlint-disable-next-line typescript/no-deprecated -- keyCode is what WebKit's accepting Enter is told apart by
    expect(isImeKey(new KeyboardEvent('keydown', { key: 'Enter', keyCode: 229 }))).toBe(true);
  });

  it('leaves an ordinary key to the page', () => {
    expect(isImeKey(new KeyboardEvent('keydown', { key: 'Escape' }))).toBe(false);
  });
});
