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

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import { actionsColumn } from './actions';

const object = { uid: 'u1', namespace: 'shop', name: 'web-1' } as ClusterCachedDataObject;

describe('actionsColumn', () => {
  it('hands the menu’s action on with the row', async () => {
    const user = userEvent.setup();
    const onAction = vi.fn();
    render(<>{actionsColumn({ apiVersion: 'v1', resource: 'pods' }, onAction).cell(object)}</>);
    await user.click(screen.getByRole('button', { name: 'Actions for web-1' }));
    await user.click(await screen.findByRole('menuitem', { name: 'View logs' }));
    expect(onAction).toHaveBeenCalledWith('logs', object);
  });

  it('is the menu and the chevron in one pinned column, 74px wide', () => {
    const column = actionsColumn({ apiVersion: 'v1', resource: 'pods' }, vi.fn());
    expect(column).toMatchObject({ pinned: true, width: 74 });
  });

  it('opens the object on its YAML from the chevron', async () => {
    const user = userEvent.setup();
    const onAction = vi.fn();
    render(<>{actionsColumn({ apiVersion: 'v1', resource: 'pods' }, onAction).cell(object)}</>);
    await user.click(screen.getByRole('button', { name: 'Open web-1' }));
    expect(onAction).toHaveBeenCalledWith('yaml', object);
  });

  // The button spells a name as its Name cell does, so two rows told apart there sound apart.
  it('names the chevron by the name as the row spells it', () => {
    const spelled = { ...object, name: 'web\u200b1' };
    render(<>{actionsColumn({ apiVersion: 'v1', resource: 'pods' }, vi.fn()).cell(spelled)}</>);
    expect(screen.getByRole('button', { name: 'Open web\\u{200B}1' })).toBeInTheDocument();
  });
});
