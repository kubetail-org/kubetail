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

import { CombinedError } from 'urql';
import { describe, expect, it } from 'vitest';

import { refusalOf, savedUnread } from './refusal';

describe('refusalOf', () => {
  it("reads the sidecar's words, else the transport's, else nothing", () => {
    expect(refusalOf(undefined)).toBeNull();
    expect(refusalOf(new CombinedError({ graphQLErrors: [{ message: 'That executable is already listed.' }] }))).toBe(
      'That executable is already listed.',
    );
    expect(refusalOf(new CombinedError({ networkError: new Error('socket closed') }))).toBe('[Network] socket closed');
  });
});

describe('savedUnread', () => {
  it('is a write that took though its answer could not be read, and nothing else', () => {
    const saved = new CombinedError({
      graphQLErrors: [{ message: 'The change was saved, but…', extensions: { code: 'KUBETAIL_SAVED_UNREAD' } }],
    });
    expect(savedUnread(saved)).toBe(true);
    expect(savedUnread(undefined)).toBe(false);
    expect(
      savedUnread(
        new CombinedError({ graphQLErrors: [{ message: 'no', extensions: { code: 'KUBETAIL_VALIDATION_ERROR' } }] }),
      ),
    ).toBe(false);
  });
});
