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

import { expectProseText } from '@/test-utils';

import { MemoryInstructions, SAVED_SHOWN_MS } from './memory-instructions';

const box = () => screen.getByRole('textbox', { name: 'Instructions' });
// Hidden by opacity, so it is in the tree throughout.
const saved = () => screen.getByText('Saved');
const type = (text: string) => fireEvent.change(box(), { target: { value: text } });
const leave = () => fireEvent.blur(box());
const advance = (ms: number) => {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
};

describe('MemoryInstructions', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('keeps spellcheck but no other suggestions', () => {
    render(<MemoryInstructions />);
    expectProseText(box());
  });

  it('describes itself for one cluster unless told otherwise', () => {
    const { rerender } = render(<MemoryInstructions />);
    expect(screen.getByText('How Kubetail should behave on this cluster.')).toBeInTheDocument();
    rerender(<MemoryInstructions description="Given to every chat." />);
    expect(screen.getByText('Given to every chat.')).toBeInTheDocument();
  });

  it('opens empty, with its placeholder and "Saved" hidden', () => {
    render(<MemoryInstructions />);
    expect(box()).toHaveValue('');
    expect(box()).toHaveAttribute('placeholder', 'e.g. keep explanations brief and to the point');
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
    expect(saved()).toHaveClass('opacity-0');
  });

  it('never saves while typing, however long the pause', () => {
    render(<MemoryInstructions />);
    type('Always use traefik.');
    expect(box()).toHaveValue('Always use traefik.');
    advance(60_000);
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
  });

  it('shows "Saved" when the box loses focus, then hides it', () => {
    render(<MemoryInstructions />);
    type('Always use traefik.');
    leave();
    expect(saved()).toHaveAttribute('aria-hidden', 'false');
    expect(saved()).not.toHaveClass('opacity-0');
    expect(screen.getByRole('status')).toBe(saved());

    advance(SAVED_SHOWN_MS);
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
  });

  it('saves on Cmd+Enter or Ctrl+Enter, keeping the text', () => {
    render(<MemoryInstructions />);
    type('Always use traefik.');
    fireEvent.keyDown(box(), { key: 'Enter', metaKey: true });
    expect(saved()).toHaveAttribute('aria-hidden', 'false');
    expect(box()).toHaveValue('Always use traefik.');

    type('Always use nginx.');
    fireEvent.keyDown(box(), { key: 'Enter', ctrlKey: true });
    expect(saved()).toHaveAttribute('aria-hidden', 'false');
  });

  it('does not save on a bare Enter or while an IME composes', () => {
    render(<MemoryInstructions />);
    type('Always use traefik.');
    fireEvent.keyDown(box(), { key: 'Enter' });
    fireEvent.keyDown(box(), { key: 'Enter', metaKey: true, isComposing: true });
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
  });

  it('does not save when the text is unchanged since the last save', () => {
    render(<MemoryInstructions />);
    leave();
    expect(saved()).toHaveAttribute('aria-hidden', 'true');

    type('Always use traefik.');
    leave();
    advance(SAVED_SHOWN_MS);
    leave();
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
  });

  it('hides "Saved" when typing resumes', () => {
    render(<MemoryInstructions />);
    type('Always');
    leave();
    type('Always use traefik.');
    expect(saved()).toHaveAttribute('aria-hidden', 'true');
    // The hide timer from the last save is gone, not left to fire later.
    expect(vi.getTimerCount()).toBe(0);
  });

  it('drops its timer when it unmounts', () => {
    const { unmount } = render(<MemoryInstructions />);
    type('Always use traefik.');
    leave();
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
