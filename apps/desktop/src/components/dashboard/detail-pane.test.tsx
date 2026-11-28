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

import { DetailPane } from './detail-pane';

const pane = () => screen.getByRole('region', { name: 'Pod' });

describe('DetailPane', () => {
  it('draws the title and the actions over a body that scrolls', () => {
    render(
      <DetailPane
        label="Pod"
        title={<h2>worker</h2>}
        actions={<button type="button">Logs</button>}
        closeLabel="Close"
        onClose={vi.fn()}
      >
        body
      </DetailPane>,
    );
    const header = pane().querySelector('header')!;
    expect(header).toHaveClass('items-start', 'gap-2', 'pr-2.5');
    expect(header).toHaveTextContent('workerLogs');
    expect(screen.getByText('body')).toHaveClass('overflow-auto', 'gap-3', 'pr-2.5');
  });

  it('draws a note under the header, and no body without one', () => {
    render(<DetailPane label="Pod" closeLabel="Close" note="This object no longer exists." onClose={vi.fn()} />);
    expect(screen.getByText('This object no longer exists.')).toHaveClass('text-xs', 'text-muted-foreground');
    expect(pane().children).toHaveLength(2);
  });

  it('takes focus as it opens', () => {
    render(<DetailPane label="Pod" closeLabel="Close" onClose={vi.fn()} />);
    expect(pane()).toHaveFocus();
  });

  it('closes on Escape from inside it, and on its X', () => {
    const onClose = vi.fn();
    render(<DetailPane label="Pod" closeLabel="Close worker" onClose={onClose} />);
    fireEvent.keyDown(pane(), { key: 'Enter' });
    expect(onClose).not.toHaveBeenCalled();
    fireEvent.keyDown(pane(), { key: 'Escape' });
    fireEvent.click(screen.getByRole('button', { name: 'Close worker' }));
    expect(onClose).toHaveBeenCalledTimes(2);
  });
});
