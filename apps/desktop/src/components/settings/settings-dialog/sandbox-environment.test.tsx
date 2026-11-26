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

import type { EnvVar } from '@/lib/env-vars';
import { SelectSettingsTab } from '@/lib/settings-tab';

const { env } = vi.hoisted(() => ({
  env: { vars: [] as EnvVar[] | undefined, loadError: null as string | null, scopes: [] as (string | null)[] },
}));
vi.mock('@/lib/env-vars', () => ({
  useEnvVars: (clusterID: string | null) => {
    env.scopes.push(clusterID);
    return { vars: env.vars, loadError: env.loadError };
  },
}));

const { SandboxEnvironment } = await import('./sandbox-environment');

beforeEach(() => {
  env.vars = [];
  env.loadError = null;
  env.scopes = [];
});

describe('SandboxEnvironment', () => {
  // Placeholder: these rows are static until the sidecar serves the run's environment.
  it("lists the variables from the shell and Kubetail's own, marked as coming soon", () => {
    render(<SandboxEnvironment />);
    const group = screen.getByRole('group', { name: 'Environment' });
    expect(screen.getByText('From your shell').closest('li')).toHaveTextContent('PATHLANGTZ');
    expect(screen.getByText('Set by Kubetail').closest('li')).toHaveTextContent('TERM=dumb');
    expect(within(group).getByText('Coming soon')).toBeInTheDocument();
    expect(within(group).getByRole('button', { name: 'Import from shell' })).toBeDisabled();
  });

  it('lists the variables never passed', async () => {
    render(<SandboxEnvironment />);
    await userEvent.click(screen.getByRole('button', { name: 'Never passed' }));
    const list = await screen.findByRole('list', { name: 'Never passed' });
    expect(within(list).getByText('SSH_AUTH_SOCK')).toBeInTheDocument();
  });

  it("lists every cluster's variables a sandboxed command gets, with nothing to edit", () => {
    env.vars = [
      { name: 'HELM_NAMESPACE', value: 'apps\u200b', target: 'Both', refusal: null },
      { name: 'PAGER', value: 'less', target: 'Sandbox', refusal: null },
      { name: 'AWS_PROFILE', value: 'dev', target: 'Outside', refusal: null },
      { name: 'LD_PRELOAD', value: '/x', target: 'Both', refusal: 'LD_PRELOAD changes how a program starts.' },
    ];
    render(<SandboxEnvironment />);
    expect(env.scopes).toEqual([null]);
    const rows = within(screen.getByRole('list', { name: 'Custom' })).getAllByRole('listitem');
    expect(rows.map((r) => r.textContent)).toEqual(['HELM_NAMESPACE=apps\\u{200B}', 'PAGER=less']);
    expect(screen.getByText(/^For every cluster\./)).toHaveTextContent("A chat's cluster may add its own.");
    expect(screen.queryByRole('button', { name: 'Remove' })).not.toBeInTheDocument();
  });

  it('says when there is none, and neither before the list answers', () => {
    const { rerender } = render(<SandboxEnvironment />);
    expect(screen.getByText(/^None for every cluster\./)).toBeInTheDocument();

    env.vars = undefined;
    rerender(<SandboxEnvironment />);
    expect(screen.queryByText(/^None for every cluster\./)).not.toBeInTheDocument();
  });

  it('says why the variables could not be read', () => {
    env.vars = undefined;
    env.loadError = 'no app.db';
    render(<SandboxEnvironment />);
    expect(screen.getByText('no app.db')).toBeInTheDocument();
  });

  it('sends the user to the Environment tab to edit them', async () => {
    const selectTab = vi.fn();
    render(
      <SelectSettingsTab value={selectTab}>
        <SandboxEnvironment />
      </SelectSettingsTab>,
    );
    await userEvent.click(screen.getByRole('button', { name: 'Edit variables' }));
    expect(selectTab).toHaveBeenCalledWith('Environment');
  });
});
