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

import type { ToolGroup } from '@/gql/graphql';
import type { ToolSettings } from '@/lib/tool-settings';

const { hook } = vi.hoisted(() => ({ hook: { current: {} as ToolSettings } }));
vi.mock('@/lib/tool-settings', () => ({ useToolSettings: () => hook.current }));

const { ToolsSettings } = await import('./tools');

const tool = (name: string, group: ToolGroup, enabled = true, alwaysOn = false) => ({
  name,
  label: name,
  group,
  description: `What ${name} does.`,
  enabled,
  alwaysOn,
});

beforeEach(() => {
  hook.current = {
    tools: [tool('Read', 'Files'), tool('Bash', 'Commands'), tool('WebFetch', 'Web', false), tool('Write', 'Files')],
    changing: new Set(),
    error: null,
    set: vi.fn(async () => {}),
  };
});

describe('ToolsSettings', () => {
  it('draws nothing but the lede before the sidecar answers', () => {
    hook.current = { ...hook.current, tools: undefined };
    render(<ToolsSettings />);
    expect(screen.queryByRole('group')).toBeNull();
    expect(screen.queryByRole('switch')).toBeNull();
  });

  it('groups the tools by what they do, in a fixed order, leaving out an empty group', () => {
    render(<ToolsSettings />);
    const groups = screen.getAllByRole('group');
    expect(groups.map((g) => within(g).getByRole('heading').textContent)).toEqual(['Commands', 'Files', 'Web']);
    const files = within(groups[1]);
    expect(files.getAllByRole('switch').map((s) => s.getAttribute('aria-label'))).toEqual(['Read', 'Write']);
    expect(files.getByText('What Read does.')).toBeInTheDocument();
  });

  it("draws each switch as the tool's state, and sends a flip", async () => {
    const user = userEvent.setup();
    render(<ToolsSettings />);
    expect(screen.getByRole('switch', { name: 'Bash' })).toBeChecked();
    const webFetch = screen.getByRole('switch', { name: 'WebFetch' });
    expect(webFetch).not.toBeChecked();

    await user.click(webFetch);
    expect(hook.current.set).toHaveBeenCalledWith('WebFetch', true);
    await user.click(screen.getByRole('switch', { name: 'Bash' }));
    expect(hook.current.set).toHaveBeenCalledWith('Bash', false);
  });

  it("holds an always-on tool's switch on", () => {
    hook.current = { ...hook.current, tools: [tool('TaskStop', 'Commands', true, true)] };
    render(<ToolsSettings />);
    const taskStop = screen.getByRole('switch', { name: 'TaskStop' });
    expect(taskStop).toBeChecked();
    expect(taskStop).toHaveAttribute('aria-disabled', 'true');
  });

  it('holds a switch while its change is in flight, and shows a refusal', () => {
    hook.current = { ...hook.current, changing: new Set(['Bash']), error: 'No listed tool has that name.' };
    render(<ToolsSettings />);
    expect(screen.getByRole('switch', { name: 'Bash' })).toHaveAttribute('aria-disabled', 'true');
    expect(screen.getByRole('switch', { name: 'Read' })).not.toHaveAttribute('aria-disabled');
    expect(screen.getByRole('alert')).toHaveTextContent('No listed tool has that name.');
  });
});
