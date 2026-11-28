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

import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { DetailRow } from './detail-row';

describe('DetailRow', () => {
  it('draws a label and its value, in the foreground unless told otherwise', () => {
    const { rerender } = render(
      <dl>
        <DetailRow label="Node">pool-a</DetailRow>
      </dl>,
    );
    expect(screen.getByText('Node').tagName).toBe('DT');
    expect(screen.getByText('pool-a')).toHaveClass('truncate', 'text-foreground');
    rerender(
      <dl>
        <DetailRow label="Node" className="font-mono">
          pool-a
        </DetailRow>
      </dl>,
    );
    expect(screen.getByText('pool-a')).toHaveClass('font-mono');
    expect(screen.getByText('pool-a')).not.toHaveClass('text-foreground');
  });

  it('titles its value and hears the pointer arrive', () => {
    const onPointerEnter = vi.fn();
    render(
      <dl>
        <DetailRow label="Node" title="pool-a" onPointerEnter={onPointerEnter}>
          pool-a
        </DetailRow>
      </dl>,
    );
    const value = screen.getByText('pool-a');
    expect(value).toHaveAttribute('title', 'pool-a');
    fireEvent.pointerEnter(value);
    expect(onPointerEnter).toHaveBeenCalled();
  });
});
