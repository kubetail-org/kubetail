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

import { StatusPill } from './status-pill';

const ZWSP = '\u200b';

describe('StatusPill', () => {
  it('draws its label through VisibleText, marked with its tone', () => {
    const { container } = render(<StatusPill label={`Run${ZWSP}ning`} tone="ok" />);
    expect(container.firstChild).toHaveAttribute('data-tone', 'ok');
    expect(screen.getByText(/^Run/)).toHaveTextContent('Run\\u{200B}ning');
  });

  // The pill cuts a long reason to its column.
  it('gives the whole label on hover', () => {
    const { container } = render(<StatusPill label="CreateContainerConfigError" tone="failing" />);
    expect(container.firstChild).toHaveAttribute('title', 'CreateContainerConfigError');
  });

  it('draws a failing label in the destructive colour', () => {
    render(<StatusPill label="CrashLoopBackOff" tone="failing" />);
    expect(screen.getByText('CrashLoopBackOff').closest('.text-destructive')).not.toBeNull();
  });

  it('draws its dot green, amber or red', () => {
    const dot = (tone: 'ok' | 'pending' | 'failing') =>
      render(<StatusPill label="x" tone={tone} />).container.querySelector('[aria-hidden]');
    expect(dot('ok')).toHaveClass('bg-emerald-500');
    expect(dot('pending')).toHaveClass('bg-amber-500');
    expect(dot('failing')).toHaveClass('bg-red-500');
  });
});
