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
import { describe, expect, it, vi } from 'vitest';

import { DiffBlock } from './diff-block';

const diff = '@@ -1,3 +1,3 @@\n data:\n-  k: old\n+  k: new​\n kind: ConfigMap\n';

describe('DiffBlock', () => {
  it('draws a line per span, an added and a removed line by class, a hunk muted', () => {
    const { container } = render(<DiffBlock head={diff} rest="" shown={false} onShow={vi.fn()} />);
    const lines = [...container.querySelectorAll('pre > span')];
    expect(lines.map((l) => l.textContent)).toEqual([
      '@@ -1,3 +1,3 @@\n',
      ' data:\n',
      '-  k: old\n',
      expect.stringContaining('+  k: new'),
      ' kind: ConfigMap\n',
    ]);
    expect(lines[0]).toHaveClass('text-muted-foreground');
    expect(lines[2]).toHaveClass('diff-del');
    expect(lines[3]).toHaveClass('diff-add');
    expect(lines[1].className).toBe('');
    expect(lines[3].querySelector('mark')).not.toBeNull();
  });

  it('folds the rest behind Show the rest', () => {
    const onShow = vi.fn();
    const { rerender } = render(<DiffBlock head={'+a\n'} rest={'+b\n-c\n'} shown={false} onShow={onShow} />);
    expect(screen.queryByText('+b')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /^Show the rest/ }));
    expect(onShow).toHaveBeenCalled();

    rerender(<DiffBlock head={'+a\n'} rest={'+b\n-c\n'} shown onShow={onShow} />);
    expect(screen.queryByRole('button', { name: /^Show the rest/ })).toBeNull();
    expect(screen.getByText('-c')).toHaveClass('diff-del');
  });
});
