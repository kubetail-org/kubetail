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
import { Globe } from 'lucide-react';
import { describe, expect, it } from 'vitest';

import { SectionHeading } from './section-heading';

describe('SectionHeading', () => {
  it('is a heading named by its label, and nothing to press', () => {
    render(<SectionHeading label="Namespace" />);
    expect(screen.getByRole('heading', { name: 'Namespace' })).toBeInTheDocument();
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('starts 8px below what is above it, unless the caller says otherwise', () => {
    const { rerender } = render(<SectionHeading label="Namespace" />);
    expect(screen.getByRole('heading')).toHaveClass('mt-2');
    rerender(<SectionHeading label="Namespace" className="mt-0" />);
    expect(screen.getByRole('heading')).toHaveClass('mt-0');
    expect(screen.getByRole('heading')).not.toHaveClass('mt-2');
  });

  it('draws an icon before its label when given one, hidden from the reader', () => {
    const { container, rerender } = render(<SectionHeading label="Cluster" icon={Globe} />);
    const icon = container.querySelector('svg')!;
    expect(icon).toHaveClass('lucide-globe');
    expect(icon).toHaveAttribute('aria-hidden', 'true');
    expect(screen.getByRole('heading', { name: 'Cluster' })).toBeInTheDocument();

    rerender(<SectionHeading label="Cluster" />);
    expect(container.querySelector('svg')).toBeNull();
  });
});
