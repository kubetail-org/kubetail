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

import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { VisibleText } from './visible-text';

describe('VisibleText', () => {
  it('marks a character the eye cannot see', () => {
    const { container } = render(<VisibleText text={'/opt/\u202ebin'} />);
    expect(container.querySelectorAll('mark')).toHaveLength(1);
    expect(container.textContent).toContain('/opt/');
  });
});
