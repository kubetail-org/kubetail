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

import { expectCodeText } from '@/test-utils';

import { TestRequest } from './permission-test-request';

// Kept out of JSX: the formatter writes an escape inside it as the bare character.
const CONTEXTS = ['prod-eu-west', 'staging\u202E'];

async function open() {
  const user = userEvent.setup();
  render(<TestRequest contexts={CONTEXTS} />);
  await user.click(screen.getByRole('button', { name: 'Test a request' }));
  return { user, dialog: await screen.findByRole('dialog', { name: 'Test a request' }) };
}

describe('TestRequest', () => {
  it('draws the request a test would check, over the known contexts', async () => {
    const { dialog } = await open();
    ['Applies to', 'Context', 'Namespace', 'Verb', 'Resource'].forEach((label) =>
      expect(within(dialog).getByLabelText(label)).toBeInTheDocument(),
    );
    const contexts = within(within(dialog).getByLabelText('Context')).getAllByRole('option');
    expect(contexts[0]).toHaveTextContent('prod-eu-west');
    expect(contexts[1]).toHaveTextContent('staging\\u{202E}');
    expect(within(within(dialog).getByLabelText('Verb')).getByRole('option', { name: 'patch' })).toBeInTheDocument();
    expect(dialog).toHaveTextContent(
      "Checks your modes and saved rules. Chat grants aren't included. Nothing is sent to the cluster.",
    );
  });

  it("turns off the webview's suggestions in its text boxes", async () => {
    const { dialog } = await open();
    within(dialog)
      .getAllByRole('textbox')
      .forEach((box) => expectCodeText(box));
  });

  // Placeholder: no query decides a hypothetical request yet, so nothing is
  // tested and no verdict is drawn.
  it('holds Test, and says results are coming', async () => {
    const { dialog } = await open();
    expect(within(dialog).getByRole('button', { name: 'Test' })).toBeDisabled();
    expect(dialog).toHaveTextContent('Coming soon');
  });

  it('closes', async () => {
    const { user } = await open();
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
