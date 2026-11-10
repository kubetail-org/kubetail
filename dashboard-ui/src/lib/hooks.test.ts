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

import { CombinedGraphQLErrors } from '@apollo/client/errors';

import { isWatchExpiredError } from './hooks';

function gqlError(extensions: Record<string, unknown>) {
  return new CombinedGraphQLErrors({ errors: [{ message: 'watch error', extensions }] });
}

describe('isWatchExpiredError', () => {
  it('returns true for a watch-expired GraphQL error', () => {
    const err = gqlError({ code: 'KUBETAIL_WATCH_ERROR', reason: 'Expired', status: 'Failure' });
    expect(isWatchExpiredError(err)).toBe(true);
  });

  it('returns false for a watch error with another reason', () => {
    const err = gqlError({ code: 'KUBETAIL_WATCH_ERROR', reason: 'Gone', status: 'Failure' });
    expect(isWatchExpiredError(err)).toBe(false);
  });

  it('returns false for a GraphQL error with no errors', () => {
    expect(isWatchExpiredError(new CombinedGraphQLErrors({ errors: [] }))).toBe(false);
  });

  it('returns false for an error that is not a GraphQL error', () => {
    expect(isWatchExpiredError(new Error('network down'))).toBe(false);
  });
});
