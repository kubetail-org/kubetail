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

import { readFileSync } from 'node:fs';

import { describe, expect, it } from 'vitest';

const html = readFileSync('index.html', 'utf8');

describe('index.html boot mark', () => {
  it('drags the window from a strip as tall as the 56px app bar', () => {
    expect(html).toMatch(/\.boot-drag \{[^}]*height: 56px;/);
  });

  // The <img> attributes size the mark on frames the <style> has not reached.
  it('places the 48px static mark and drag strip inside #root, where the mount replaces them', () => {
    expect(html).toContain(
      '<div id="root"><div class="boot-drag" data-tauri-drag-region="deep"></div><div class="boot-mark" aria-hidden="true">' +
        '<img class="light-only" src="/src/assets/icons/app-light.svg" width="48" height="48" alt="" />' +
        '<img class="dark-only" src="/src/assets/icons/app-dark.svg" width="48" height="48" alt="" /></div></div>',
    );
  });
});

// src/assets/icons/ is make generate-icons' output; an edit there is lost on the next run.
describe('src/assets/icons', () => {
  it.each(['app-light.svg', 'app-dark.svg'])('holds src-icons/%s as it is', (name) => {
    expect(readFileSync(`src/assets/icons/${name}`, 'utf8')).toBe(readFileSync(`src-icons/${name}`, 'utf8'));
  });
});
