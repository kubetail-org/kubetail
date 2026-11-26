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

import { render, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import { HighlightedText } from './highlighted-text';

describe('HighlightedText', () => {
  it('draws the text plain, then highlighted once the grammar arrives', async () => {
    const { container } = render(<HighlightedText lang="yaml" text="kind: Pod" />);
    expect(container).toHaveTextContent('kind: Pod');
    await waitFor(() => expect(container.querySelector('.hljs-attr')).toHaveTextContent('kind'));
    expect(container).toHaveTextContent('kind: Pod');
  });

  it('spells an invisible character, plain and highlighted', async () => {
    const text = 'name: a\u200Bb';
    const { container } = render(<HighlightedText lang="yaml" text={text} />);
    expect(container.querySelector('mark')).toHaveAttribute('title', 'An invisible character');
    await waitFor(() => expect(container.querySelector('.hljs-attr')).not.toBeNull());
    expect(container.querySelector('mark')).toHaveAttribute('title', 'An invisible character');
  });

  it('stays plain in a language with no grammar', async () => {
    const { load } = await import('@/lib/highlight');
    const { container } = render(<HighlightedText lang="nonesuch" text="kind: Pod" />);
    await expect(load('nonesuch')).resolves.toBe(false);
    expect(container).toHaveTextContent('kind: Pod');
    expect(container.querySelector('span')).toBeNull();
  });

  it('stops waiting for the grammar once unmounted', async () => {
    // A fresh module graph, so the grammar is not loaded yet.
    vi.resetModules();
    const { HighlightedText: Fresh } = await import('./highlighted-text');
    const { load } = await import('@/lib/highlight');
    render(<Fresh lang="yaml" text="kind: Pod" />).unmount();
    await expect(load('yaml')).resolves.toBe(true);
  });
});
