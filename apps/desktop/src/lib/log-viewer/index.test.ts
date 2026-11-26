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

import * as library from '.';

describe('the library’s index', () => {
  it('exports the core, its store and its reader, the window and its first snapshot, the line order and the step back from a moment, how a line’s time reads, and leaves the fake out', () => {
    expect(Object.keys(library).toSorted()).toEqual([
      'CORE_INITIAL_STATE',
      'LogViewerCore',
      'LogWindow',
      'compareTimestamps',
      'containerKey',
      'createCoreStore',
      'lineTime',
      'previousNanosecond',
      'resumeOf',
      'useCoreFields',
    ]);
  });
});
