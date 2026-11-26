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

import { objectYaml } from './object-yaml';

describe('objectYaml', () => {
  it('leads with kubectl’s keys, the rest in the body’s order', () => {
    const body = { status: { phase: 'Running' }, extra: 1, metadata: { name: 'web' }, kind: 'Pod', apiVersion: 'v1' };
    expect(objectYaml(body)).toBe(
      'apiVersion: v1\nkind: Pod\nmetadata:\n  name: web\nstatus:\n  phase: Running\nextra: 1\n',
    );
  });

  it('keeps a long value on its line', () => {
    const long = 'x '.repeat(100).trim();
    expect(objectYaml({ data: { note: long } })).toBe(`data:\n  note: ${long}\n`);
  });

  it('prints a body that is not an object as it is', () => {
    expect(objectYaml(null)).toBe('null\n');
    expect(objectYaml(undefined)).toBe('null\n');
    expect(objectYaml(['a'])).toBe('- a\n');
  });
});
