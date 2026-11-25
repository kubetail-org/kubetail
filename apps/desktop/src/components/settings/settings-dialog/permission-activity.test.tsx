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
import { describe, expect, it, vi } from 'vitest';

import { ChatGrants, RanWithoutAsking } from './permission-activity';

// Kept out of JSX: the formatter writes an escape inside it as the bare character.
const SPOOFED = 'fix\u202Eit';

const grant = {
  id: 'g1',
  effect: 'Allow' as const,
  line: 'Cluster writes inside kind-dev / default',
  chatTitle: 'Debug the crashloop',
  when: '20 minutes ago',
};

const action = {
  id: 'a1',
  outcome: 'Redacted' as const,
  summary: 'Get secrets/db-credentials in payments on prod-eu-west',
  when: 'Yesterday',
  reason: 'Rule: Deny Secret reads in prod-*',
};

describe('ChatGrants', () => {
  it('says what a chat grant is, and that its list is coming', () => {
    render(<ChatGrants />);
    const section = screen.getByRole('group', { name: 'Chat grants' });
    expect(section).toHaveTextContent(
      'Approvals you gave for the rest of one chat. Revoking one asks again next time.',
    );
    expect(section).toHaveTextContent('Coming soon');
  });

  it('lists each grant with its chat and time, and revokes one', async () => {
    const user = userEvent.setup();
    const onRevoke = vi.fn();
    render(<ChatGrants grants={[grant]} onRevoke={onRevoke} />);
    const row = screen.getByRole('listitem', { name: grant.line });
    expect(row).toHaveTextContent('Allow');
    expect(row).toHaveTextContent('Debug the crashloop · 20 minutes ago');

    await user.click(within(row).getByRole('button', { name: 'Revoke' }));
    expect(onRevoke).toHaveBeenCalledWith('g1');
  });

  it('spells a chat title out', () => {
    render(<ChatGrants grants={[{ ...grant, chatTitle: SPOOFED }]} onRevoke={() => {}} />);
    expect(screen.getByRole('listitem').querySelector('mark')).toHaveTextContent('\\u{202E}');
  });

  it('says when no chat holds a grant', () => {
    render(<ChatGrants grants={[]} onRevoke={() => {}} />);
    expect(screen.getByRole('group', { name: 'Chat grants' })).toHaveTextContent('No chat holds a grant.');
  });
});

describe('RanWithoutAsking', () => {
  it('says what it lists, and that the list is coming', () => {
    render(<RanWithoutAsking />);
    const section = screen.getByRole('group', { name: 'Ran without asking' });
    expect(section).toHaveTextContent('Writes your mode or rules decided with nobody asked. Last 7 days.');
    expect(section).toHaveTextContent('Coming soon');
    expect(within(section).queryByRole('button')).toBeNull();
  });

  it('lists each action with its outcome, time and reason, and opens its chat', async () => {
    const user = userEvent.setup();
    const onOpenChat = vi.fn();
    const onViewAll = vi.fn();
    render(<RanWithoutAsking actions={[action]} total={23} onOpenChat={onOpenChat} onViewAll={onViewAll} />);
    const row = screen.getByRole('listitem', { name: action.summary });
    expect(row).toHaveTextContent('Redacted');
    expect(row).toHaveTextContent('Yesterday · Rule: Deny Secret reads in prod-*');

    await user.click(within(row).getByRole('button', { name: 'Open chat' }));
    expect(onOpenChat).toHaveBeenCalledWith('a1');
    await user.click(screen.getByRole('button', { name: 'View all 23' }));
    expect(onViewAll).toHaveBeenCalled();
  });

  it('spells a reason out', () => {
    render(
      <RanWithoutAsking
        actions={[{ ...action, reason: SPOOFED }]}
        total={1}
        onOpenChat={() => {}}
        onViewAll={() => {}}
      />,
    );
    expect(screen.getByRole('listitem').querySelector('mark')).toHaveTextContent('\\u{202E}');
  });

  it('says when nothing ran without asking', () => {
    render(<RanWithoutAsking actions={[]} total={0} onOpenChat={() => {}} onViewAll={() => {}} />);
    const section = screen.getByRole('group', { name: 'Ran without asking' });
    expect(section).toHaveTextContent('Nothing ran without asking in the last 7 days.');
    expect(within(section).queryByRole('button')).toBeNull();
  });
});
