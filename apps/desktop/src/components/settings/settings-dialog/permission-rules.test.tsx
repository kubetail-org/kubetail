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
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { EffectBadge, PermissionRow, PermissionRules } from './permission-rules';

const allow = {
  id: 'r1',
  line: 'Allow writes in staging / payments-*',
  effect: 'Allow' as const,
  accessLevel: 'Write' as const,
  principal: null,
  target: 'Kubernetes' as const,
  scope: 'c2/payments-*',
  service: '*',
  operation: '*',
  resource: '*',
};
const deny = {
  ...allow,
  id: 'r2',
  line: 'Deny data reads everywhere',
  effect: 'Deny' as const,
  accessLevel: 'DataRead' as const,
  target: null,
  scope: '*',
};

// Kept out of JSX: the formatter writes an escape inside it as the bare character.
const RLO = 'prod\u202Eue';

const onAdd = vi.fn<(input: unknown) => Promise<boolean>>();
const onUpdate = vi.fn<(id: string, input: unknown) => Promise<boolean>>();
const onRemove = vi.fn();

function renderRules(over: Partial<Parameters<typeof PermissionRules>[0]> = {}) {
  render(
    <PermissionRules
      rules={[allow, deny]}
      targets={['Kubernetes']}
      accounts={{
        Kubernetes: [
          { value: 'c1', label: 'dev-eks' },
          { value: 'c2', label: 'staging' },
        ],
      }}
      contexts={['staging']}
      onAdd={onAdd}
      onUpdate={onUpdate}
      onRemove={onRemove}
      {...over}
    />,
  );
  return screen.getByRole('group', { name: 'Rules' });
}

const rowOf = (line: string) => screen.getByRole('listitem', { name: line });

async function openMenu(user: ReturnType<typeof userEvent.setup>, line: string) {
  await user.click(within(rowOf(line)).getByRole('button', { name: `Actions for ${line}` }));
}

beforeEach(() => {
  vi.clearAllMocks();
  onAdd.mockResolvedValue(true);
  onUpdate.mockResolvedValue(true);
});

describe('EffectBadge', () => {
  it.each([
    ['Deny', true],
    ['Redacted', true],
    ['Refused', true],
    ['Allow', false],
    ['Ask', false],
    ['Allowed', false],
  ])('draws %s outlined: %s', (effect, outlined) => {
    render(<EffectBadge effect={effect} />);
    expect(screen.getByText(effect).className.includes('border')).toBe(outlined);
  });
});

describe('PermissionRow', () => {
  it('spells its line and detail out', () => {
    render(
      <ul>
        <PermissionRow badge="Allow" line={RLO} detail={RLO} />
      </ul>,
    );
    const marks = screen.getByRole('listitem').querySelectorAll('mark');
    expect(marks).toHaveLength(2);
    marks.forEach((mark) => expect(mark).toHaveTextContent('\\u{202E}'));
  });
});

describe('PermissionRules', () => {
  it('lists each rule with its effect, and says what rules do', () => {
    const card = renderRules();
    expect(card).toHaveTextContent(
      'Checked before the mode. Each field matches anything when empty; a deny wins over an ask, and an ask over an allow.',
    );
    expect(rowOf(allow.line)).toHaveTextContent('Allow');
    expect(rowOf(deny.line)).toHaveTextContent('Deny');
    expect(card).toHaveTextContent('See what a write would do under your modes and rules.');
    expect(within(card).getByRole('button', { name: 'Test a request' })).toBeInTheDocument();
  });

  it('says when there are no rules', () => {
    const card = renderRules({ rules: [] });
    expect(card).toHaveTextContent('No rules yet.');
  });

  it('removes a rule from its menu, unconfirmed', async () => {
    const user = userEvent.setup();
    renderRules();
    await openMenu(user, allow.line);
    await user.click(await screen.findByRole('menuitem', { name: 'Remove' }));
    expect(onRemove).toHaveBeenCalledWith('r1');
  });

  it('opens the add form at the top of the card, and closes it on Cancel', async () => {
    const user = userEvent.setup();
    renderRules();
    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    expect(screen.getByRole('form', { name: 'Add a rule' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('form')).toBeNull();
  });

  it('adds a rule, and closes the form once it is taken', async () => {
    const user = userEvent.setup();
    renderRules();
    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    await user.selectOptions(screen.getByLabelText('Cluster'), 'staging');
    await user.click(screen.getByRole('button', { name: 'Add' }));
    expect(onAdd).toHaveBeenCalledWith(expect.objectContaining({ scope: 'c2/**' }));
    expect(screen.queryByRole('form')).toBeNull();
  });

  it('keeps the add form open when the add is refused', async () => {
    const user = userEvent.setup();
    onAdd.mockResolvedValue(false);
    renderRules();
    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    await user.click(screen.getByRole('button', { name: 'Add' }));
    expect(screen.getByRole('form', { name: 'Add a rule' })).toBeInTheDocument();
  });

  it('edits a rule in place of its row, by its id', async () => {
    const user = userEvent.setup();
    renderRules();

    await openMenu(user, allow.line);
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    const form = screen.getByRole('form', { name: 'Edit rule' });
    expect(screen.queryByRole('listitem', { name: allow.line })).toBeNull();
    expect(within(form).getByLabelText('Cluster')).toHaveValue('c2');
    expect(within(form).getByRole('button', { name: 'Save' })).toBeDisabled();

    await user.type(within(form).getByLabelText('Verb'), 'patch');
    await user.click(within(form).getByRole('button', { name: 'Save' }));
    expect(onUpdate).toHaveBeenCalledWith('r1', {
      effect: 'Allow',
      accessLevel: 'Write',
      principal: null,
      target: 'Kubernetes',
      scope: 'c2/payments-*',
      service: '*',
      operation: 'patch',
      resource: '*',
    });
    expect(onAdd).not.toHaveBeenCalled();
    expect(onRemove).not.toHaveBeenCalled();
    expect(screen.queryByRole('form')).toBeNull();
  });

  it('opens a rule for every target on Every target', async () => {
    const user = userEvent.setup();
    renderRules();
    await openMenu(user, deny.line);
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    const form = screen.getByRole('form', { name: 'Edit rule' });
    expect(within(form).getByLabelText('Reaches')).toHaveValue('');
    expect(within(form).getByLabelText('Account')).toHaveValue('');
  });

  it('keeps the edit form open when the edit is refused', async () => {
    const user = userEvent.setup();
    onUpdate.mockResolvedValue(false);
    renderRules();

    await openMenu(user, allow.line);
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    await user.type(screen.getByLabelText('Verb'), 'patch');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(onUpdate).toHaveBeenCalled();
    expect(screen.getByRole('form', { name: 'Edit rule' })).toBeInTheDocument();
  });
});
