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
import { afterEach, describe, expect, it, vi } from 'vitest';

import { expectCodeText, expectProseText } from '@/test-utils';

import { Input, SearchBox, Textarea } from './text-entry';

describe('text entry', () => {
  it('turns off suggestions, correction and spellcheck by default', () => {
    render(
      <>
        <Input aria-label="input" />
        <Textarea aria-label="textarea" />
      </>,
    );
    expectCodeText(screen.getByLabelText('input'));
    expectCodeText(screen.getByLabelText('textarea'));
  });

  it('keeps spellcheck for prose', () => {
    render(
      <>
        <Input prose aria-label="input" />
        <Textarea prose aria-label="textarea" />
      </>,
    );
    expectProseText(screen.getByLabelText('input'));
    expectProseText(screen.getByLabelText('textarea'));
  });

  it('turns them off in a search box too', () => {
    render(<SearchBox aria-label="search" />);
    expectCodeText(screen.getByRole('searchbox', { name: 'search' }));
  });

  // jsdom applies no CSS, so this checks the class that turns the library's
  // ring off, and that a caller's classes still join it.
  it('marks focus by the border alone, never a ring', () => {
    render(
      <>
        <Input aria-label="input" className="h-8" />
        <Textarea aria-label="textarea" />
        <SearchBox aria-label="search" />
      </>,
    );
    for (const box of [
      screen.getByLabelText('input'),
      screen.getByLabelText('textarea'),
      screen.getByLabelText('search'),
    ]) {
      expect(box).toHaveClass('focus-visible:border-ring', 'focus-visible:ring-0');
      expect(box).not.toHaveClass('focus-visible:ring-3');
    }
    expect(screen.getByLabelText('input')).toHaveClass('h-8');
  });

  it('lets a caller override a setting', () => {
    render(<Input autoComplete="on" aria-label="input" />);
    expect(screen.getByLabelText('input')).toHaveAttribute('autocomplete', 'on');
  });
});

describe('SearchBox onClear', () => {
  const reached = vi.fn();
  const onClear = vi.fn();
  const onKeyDown = vi.fn();
  const box = () => screen.getByRole('searchbox', { name: 'search' });
  const renderBox = (value: string) => {
    window.addEventListener('keydown', reached);
    render(<SearchBox aria-label="search" value={value} onChange={() => {}} onClear={onClear} onKeyDown={onKeyDown} />);
  };

  afterEach(() => {
    window.removeEventListener('keydown', reached);
    vi.clearAllMocks();
  });

  it('clears on Escape, keeping that Escape from whatever is around the box', () => {
    renderBox('st');
    fireEvent.keyDown(box(), { key: 'Escape' });
    expect(onClear).toHaveBeenCalledOnce();
    expect(reached).not.toHaveBeenCalled();
    expect(onKeyDown).toHaveBeenCalledOnce();
  });

  it('lets the Escape of an empty box through', () => {
    renderBox('');
    fireEvent.keyDown(box(), { key: 'Escape' });
    expect(onClear).not.toHaveBeenCalled();
    expect(reached).toHaveBeenCalledOnce();
  });

  it('leaves an IME’s Escape and every other key alone', () => {
    renderBox('st');
    fireEvent.keyDown(box(), { key: 'Escape', isComposing: true });
    fireEvent.keyDown(box(), { key: 'Enter' });
    expect(onClear).not.toHaveBeenCalled();
    expect(reached).toHaveBeenCalledTimes(2);
  });

  it('takes no Escape without the prop', () => {
    window.addEventListener('keydown', reached);
    render(<SearchBox aria-label="search" defaultValue="st" />);
    fireEvent.keyDown(box(), { key: 'Escape' });
    expect(reached).toHaveBeenCalledOnce();
  });
});
