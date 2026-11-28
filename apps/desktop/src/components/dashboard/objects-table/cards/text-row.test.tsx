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

import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { TextRow } from './text-row';

describe('TextRow', () => {
  it('dashes missing text, and spells what the cluster wrote, in its title too', () => {
    const { rerender } = render(
      <dl>
        <TextRow label="Node" text={undefined} />
      </dl>,
    );
    expect(screen.getByText('Node').nextElementSibling).toHaveTextContent('—');
    expect(screen.getByText('Node').nextElementSibling).not.toHaveAttribute('title');
    rerender(
      <dl>
        <TextRow label="Node" text="a​b" mono />
      </dl>,
    );
    const v = screen.getByText('Node').nextElementSibling;
    expect(v).toHaveTextContent('a\\u{200B}b');
    expect(v).toHaveAttribute('title', 'a\\u{200B}b');
    expect(v).toHaveClass('font-mono');
  });
});
