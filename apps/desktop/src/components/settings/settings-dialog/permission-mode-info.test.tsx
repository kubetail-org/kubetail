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
import { describe, expect, it } from 'vitest';

import { PermissionModeInfo } from './permission-mode-info';

describe('PermissionModeInfo', () => {
  it('opens on what no mode changes: what always asks, what asks unless a namespace rule allows it, and the fixed limits', async () => {
    const user = userEvent.setup();
    render(<PermissionModeInfo alwaysAsks={['Deleting a namespace']} accessControl={['Any change to roles']} />);
    expect(screen.queryByText('Deleting a namespace')).toBeNull();

    await user.click(screen.getByRole('button', { name: 'What modes and rules cannot change' }));
    const popup = await screen.findByRole('dialog');
    [
      'Your modes and rules decide most changes. These hold whatever you choose.',
      'Always asks, in every mode',
      'A rule can make these refuse, never run unasked. A read-only account refuses them.',
      'Deleting a namespace',
      'Asks unless a rule inside a namespace allows it',
      'Across a whole cluster or a cloud account these always ask.',
      'Any change to roles',
      'No mode or rule changes these',
      "A command running in the background can't change the cluster",
      'A chat outside the sandbox asks before every command',
      "A write Kubetail can't record doesn't run",
    ].forEach((text) => expect(popup).toHaveTextContent(text));
  });
});
