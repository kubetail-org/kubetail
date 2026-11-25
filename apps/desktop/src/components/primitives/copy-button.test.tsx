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

import { act, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { CopyButton } from './copy-button';

describe('CopyButton', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('copies its text and says so', () => {
    render(<CopyButton text="kubectl get pods" label="Copy message" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('kubectl get pods');
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument();
  });

  it('shows the check for 1.5 seconds after the latest click', () => {
    vi.useFakeTimers();
    const advance = (ms: number) =>
      act(() => {
        vi.advanceTimersByTime(ms);
      });
    render(<CopyButton text="x" label="Copy message" />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
    advance(1000);
    fireEvent.click(screen.getByRole('button', { name: 'Copied' }));
    advance(1000);
    expect(screen.getByRole('button', { name: 'Copied' })).toBeInTheDocument();
    advance(500);
    expect(screen.getByRole('button', { name: 'Copy message' })).toBeInTheDocument();
  });

  it('copies nothing while disabled', () => {
    render(<CopyButton text="x" label="Copy message" disabled />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
    expect(navigator.clipboard.writeText).not.toHaveBeenCalled();
  });
});
