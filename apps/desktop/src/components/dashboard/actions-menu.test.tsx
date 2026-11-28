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
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';

import { ActionsMenu } from './actions-menu';

const PODS = { apiVersion: 'v1', resource: 'pods' };

describe('ActionsMenu', () => {
  it('opens a menu of the kind’s actions, each handing its action on', async () => {
    const user = userEvent.setup();
    const onAction = vi.fn();
    render(<ActionsMenu gvr={PODS} name="web-1" onAction={onAction} />);

    const open = () => user.click(screen.getByRole('button', { name: 'Actions for web-1' }));
    await open();
    expect((await screen.findAllByRole('menuitem')).map((i) => i.textContent)).toEqual([
      'View YAML',
      'View events',
      'View logs',
    ]);
    await user.click(screen.getByRole('menuitem', { name: 'View YAML' }));
    await open();
    await user.click(await screen.findByRole('menuitem', { name: 'View events' }));
    await open();
    await user.click(await screen.findByRole('menuitem', { name: 'View logs' }));
    expect(onAction.mock.calls).toEqual([['yaml'], ['events'], ['logs']]);
  });

  it('leaves logs out for a kind no log view follows', async () => {
    const user = userEvent.setup();
    render(<ActionsMenu gvr={{ apiVersion: 'v1', resource: 'configmaps' }} name="web-1" onAction={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: 'Actions for web-1' }));
    expect((await screen.findAllByRole('menuitem')).map((i) => i.textContent)).toEqual(['View YAML', 'View events']);
  });

  // The trigger spells a name as its Name cell does, so two rows told apart there sound apart.
  it('names its trigger by the name as the row spells it', () => {
    render(<ActionsMenu gvr={PODS} name={'web\u200b1'} onAction={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Actions for web\\u{200B}1' })).toBeInTheDocument();
  });
});
