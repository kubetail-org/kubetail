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

import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';

import { Group, InfoPopover, Intro, NoteRow } from './group';

describe('Group', () => {
  it('names a group by its heading, with its lede and children', () => {
    render(
      <Group title="Folders" lede="What commands reach.">
        <p>a row</p>
      </Group>,
    );
    const group = screen.getByRole('group', { name: 'Folders' });
    expect(within(group).getByRole('heading', { name: 'Folders' })).toBeInTheDocument();
    expect(within(group).getByText('What commands reach.')).toBeInTheDocument();
    expect(within(group).getByText('a row')).toBeInTheDocument();
  });

  it("draws the heading's action and an info control beside it", () => {
    render(
      <Group title="PATH" info={<button type="button">About</button>} action={<button type="button">Import</button>}>
        <p>a row</p>
      </Group>,
    );
    const group = screen.getByRole('group', { name: 'PATH' });
    expect(within(group).getByRole('button', { name: 'Import' })).toBeInTheDocument();
    expect(within(group).getByRole('button', { name: 'About' })).toBeInTheDocument();
  });

  it('draws a note row as its label and note', () => {
    render(
      <ul>
        <NoteRow label="Your clusters">Through Kubetail.</NoteRow>
      </ul>,
    );
    expect(screen.getByRole('listitem')).toHaveTextContent('Your clustersThrough Kubetail.');
  });

  it('draws a title alone', () => {
    render(<Group title="Appearance">body</Group>);
    expect(screen.getByRole('group', { name: 'Appearance' }).querySelector('p')).toBeNull();
  });

  it('draws an intro as its lede over its body, with no heading or group', () => {
    render(
      <Intro lede="Services Kubetail signs in to.">
        <ul>
          <li>a row</li>
        </ul>
      </Intro>,
    );
    expect(screen.getByText('Services Kubetail signs in to.')).toBeInTheDocument();
    expect(screen.getByRole('listitem')).toHaveTextContent('a row');
    expect(screen.queryByRole('heading')).toBeNull();
    expect(screen.queryByRole('group')).toBeNull();
  });

  it('opens its note from the info icon', async () => {
    render(<InfoPopover label="About folders">Some stay closed.</InfoPopover>);
    expect(screen.queryByText('Some stay closed.')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'About folders' }));
    expect(await screen.findByText('Some stay closed.')).toBeInTheDocument();
  });
});
