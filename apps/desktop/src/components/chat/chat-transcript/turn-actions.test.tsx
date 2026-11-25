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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { TurnActions } from './turn-actions';

const HALF_HOUR_AGO = () => new Date(Date.now() - 30 * 60_000).toISOString();

const names = () => screen.getAllByRole('button').map((b) => b.getAttribute('aria-label'));

describe('TurnActions', () => {
  beforeEach(() => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
  });

  it("draws a question's time, then Reload, Edit and Copy, shown on hover", () => {
    const { container } = render(<TurnActions mine text="why?" createdAt={HALF_HOUR_AGO()} />);
    expect(screen.getByText('30 minutes ago')).toBeInTheDocument();
    expect(names()).toEqual(['Reload', 'Edit', 'Copy message']);
    expect(container.querySelector('.opacity-0')).not.toBeNull();
  });

  it('copies the question', () => {
    render(<TurnActions mine text="why?" createdAt={HALF_HOUR_AGO()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy message' }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('why?');
  });

  it('leaves Reload and Edit disabled', () => {
    render(<TurnActions mine text="why?" createdAt={HALF_HOUR_AGO()} />);
    expect(screen.getByRole('button', { name: 'Reload' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
  });

  it("draws an answer's Copy and Reload, then its time, shown on hover", () => {
    const { container } = render(<TurnActions mine={false} text="because" createdAt={HALF_HOUR_AGO()} />);
    expect(names()).toEqual(['Copy answer', 'Reload']);
    expect(screen.getByText('30 minutes ago')).toBeInTheDocument();
    expect(container.querySelector('.opacity-0')).not.toBeNull();
  });

  it("always shows the last answer's Copy and Reload, and its time on hover", () => {
    render(<TurnActions mine={false} text="because" createdAt={HALF_HOUR_AGO()} last />);
    expect(names()).toEqual(['Copy answer', 'Reload']);
    expect(screen.getByRole('button', { name: 'Copy answer' }).closest('.opacity-0')).toBeNull();
    expect(screen.getByText('30 minutes ago').closest('.opacity-0')).not.toBeNull();
  });

  it("draws nothing in a working answer's row", () => {
    const { container } = render(<TurnActions mine={false} text="because" createdAt={HALF_HOUR_AGO()} last working />);
    expect(screen.queryAllByRole('button')).toEqual([]);
    expect(screen.queryByText('30 minutes ago')).toBeNull();
    expect(container.querySelector('.h-6')).not.toBeNull();
  });

  it('disables Copy on a turn with no text', () => {
    render(<TurnActions mine={false} text="" createdAt={HALF_HOUR_AGO()} />);
    expect(screen.getByRole('button', { name: 'Copy answer' })).toBeDisabled();
  });
});
