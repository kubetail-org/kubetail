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

import { render } from '@testing-library/react';
import { stripAnsi } from 'fancy-ansi';
import { describe, expect, it } from 'vitest';

import { AnsiText, ansiRuns } from './ansi-text';

describe('ansiRuns', () => {
  it('reads a plain message as one run', () => {
    expect(ansiRuns('ok')).toEqual([{ text: 'ok', style: undefined }]);
  });

  it('styles the text its sequences colour', () => {
    expect(ansiRuns('\x1b[31merror\x1b[0m: disk')).toEqual([
      { text: 'error', style: { color: 'var(--ansi-red, #cc0000)' } },
      { text: ': disk', style: undefined },
    ]);
  });

  it('keeps leading whitespace', () => {
    expect(ansiRuns('\n\t  prefix\x1b[31mred').map((r) => r.text)).toEqual(['\n\t  prefix', 'red']);
  });

  it('keeps an extended colour set beside another attribute', () => {
    const bold = 'var(--ansi-bold-font-weight, 600)';
    expect(ansiRuns('\x1b[1;38;2;255;0;0mred')).toEqual([
      { text: 'red', style: { fontWeight: bold, color: 'rgb(255, 0, 0)' } },
    ]);
    expect(ansiRuns('\x1b[38;5;196;1mred')).toEqual([
      { text: 'red', style: { fontWeight: bold, color: 'rgb(255, 0, 0)' } },
    ]);
  });

  it('clears bold and dim at 22, keeping the colour', () => {
    expect(ansiRuns('\x1b[2;32mdim\x1b[1mbold\x1b[22mnormal')).toEqual([
      { text: 'dim', style: { opacity: 'var(--ansi-dim-opacity, 0.7)', color: 'var(--ansi-green, #4e9a06)' } },
      {
        text: 'bold',
        style: {
          opacity: 'var(--ansi-dim-opacity, 0.7)',
          fontWeight: 'var(--ansi-bold-font-weight, 600)',
          color: 'var(--ansi-green, #4e9a06)',
        },
      },
      { text: 'normal', style: { color: 'var(--ansi-green, #4e9a06)' } },
    ]);
  });

  it('reads markup in the text as text', () => {
    const runs = ansiRuns('\x1b[1m<img src=x onerror="alert(1)">&amp;');
    expect(runs.map((r) => r.text).join('')).toBe('<img src=x onerror="alert(1)">&amp;');
  });

  it('never hides text', () => {
    expect(ansiRuns('\x1b[8msecret')).toEqual([{ text: 'secret', style: undefined }]);
  });

  // Copy and the wrap estimate read a message through stripAnsi, the runs draw it: the forms the
  // sidecar keeps (`isSGR`) must mean the same text to both.
  it('reads the same text stripAnsi leaves', () => {
    ['\x1b[1;38;2;255;0;0mred\x1b[0m', '\x1b[mbare', '\x1b[;1;mempty', '\x1b[0001;0000mfour'].forEach((message) => {
      expect(
        ansiRuns(message)
          .map((r) => r.text)
          .join(''),
      ).toBe(stripAnsi(message));
    });
  });
});

describe('AnsiText', () => {
  it('draws coloured text in a styled span', () => {
    // oxlint-disable-next-line react/jsx-curly-brace-presence -- a JSX attribute string reads no escapes
    const { container } = render(<AnsiText text={'a\x1b[32mb'} />);
    expect(container).toHaveTextContent('ab');
    expect(container.querySelector<HTMLElement>('span[style]')?.style.color).toBe('var(--ansi-green, #4e9a06)');
  });

  it('spells out what cluster text could hide inside a colour', () => {
    // oxlint-disable-next-line react/jsx-curly-brace-presence -- a JSX attribute string reads no escapes
    const { container } = render(<AnsiText text={'\x1b[31mok‮evil'} />);
    expect(container.querySelector('span mark')).not.toBeNull();
  });

  it('draws no element from markup in the text', () => {
    // oxlint-disable-next-line react/jsx-curly-brace-presence -- a JSX attribute string reads no escapes
    const { container } = render(<AnsiText text={'\x1b[31m<b>bold</b><img src=x>'} />);
    expect(container.querySelector('b, img')).toBeNull();
    expect(container).toHaveTextContent('<b>bold</b><img src=x>');
  });
});
