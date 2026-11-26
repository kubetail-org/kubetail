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

import { AppBarButton } from './button';

describe('AppBarButton', () => {
  it('is a 28px ghost square with the bar’s hover wash', () => {
    render(<AppBarButton aria-label="Bell" />);
    const button = screen.getByRole('button', { name: 'Bell' });
    expect(button).toHaveClass('size-7', 'text-muted-foreground', 'hover:bg-app-bar-hover');
    // No outline: only hover paints.
    expect(button).toHaveClass('border-transparent');
  });

  it('takes a caller’s class over its own', () => {
    render(<AppBarButton aria-label="Back" className="size-7.5 rounded-full" />);
    expect(screen.getByRole('button', { name: 'Back' })).toHaveClass('size-7.5', 'rounded-full');
    expect(screen.getByRole('button', { name: 'Back' })).not.toHaveClass('size-7');
  });
});
