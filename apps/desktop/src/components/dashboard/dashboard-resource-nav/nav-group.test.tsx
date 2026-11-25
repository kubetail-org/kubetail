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
import { describe, expect, it, vi } from 'vitest';

import { NavGroup } from './nav-group';

describe('NavGroup', () => {
  it('is a toggle that says whether it is open', () => {
    const onToggle = vi.fn();
    const { rerender } = render(<NavGroup label="Workloads" open={false} onToggle={onToggle} />);
    const button = screen.getByRole('button', { name: 'Workloads' });
    expect(button).toHaveAttribute('aria-expanded', 'false');

    button.click();
    expect(onToggle).toHaveBeenCalledOnce();

    rerender(<NavGroup label="Workloads" open onToggle={onToggle} />);
    expect(button).toHaveAttribute('aria-expanded', 'true');
  });

  it('draws a known count, and none while it is unknown', () => {
    const { rerender } = render(<NavGroup label="Workloads" open={false} count={268} onToggle={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveTextContent('Workloads268');

    rerender(<NavGroup label="Workloads" open={false} onToggle={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveTextContent(/^Workloads$/);
  });

  it('draws as selected while it stands in for the current kind', () => {
    const { rerender } = render(<NavGroup label="Workloads" open={false} onToggle={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveAttribute('data-active', 'false');

    rerender(<NavGroup label="Workloads" open={false} active onToggle={vi.fn()} />);
    expect(screen.getByRole('button')).toHaveAttribute('data-active', 'true');
  });
});
