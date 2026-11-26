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
import { describe, expect, it, vi } from 'vitest';

import { EMPTY_LOG_VIEWPORT, createLogViewportHandle } from './log-viewport';

describe('createLogViewportHandle', () => {
  it('starts empty, not at the end, unpinned and playing, with no grep and nothing selected', () => {
    expect(createLogViewportHandle().get()).toEqual({
      range: null,
      atEnd: false,
      pinnedToEnd: false,
      paused: false,
      visible: 0,
      grep: null,
      selection: [],
      selectedCount: 0,
    });
  });

  it('answers the last state written, and tells its listeners once per write', () => {
    const viewport = createLogViewportHandle();
    const listener = vi.fn();
    const unsubscribe = viewport.subscribe(listener);

    const next = {
      range: { from: 'a', to: 'b' },
      atEnd: false,
      pinnedToEnd: false,
      paused: false,
      visible: 3,
      grep: '',
      selection: [],
      selectedCount: 0,
    };
    viewport.set(next);
    expect(viewport.get()).toBe(next);
    expect(listener).toHaveBeenCalledOnce();

    unsubscribe();
    viewport.set(EMPTY_LOG_VIEWPORT);
    expect(listener).toHaveBeenCalledOnce();
  });
});
