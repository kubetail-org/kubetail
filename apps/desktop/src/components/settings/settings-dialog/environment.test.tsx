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

import type { EnvVars } from '@/lib/env-vars';
import { expectCodeText } from '@/test-utils';

// The seams: the variables hook, which records the scope it is asked for, and
// the scope picker, a button that picks the cluster prod.
const { env } = vi.hoisted(() => ({ env: { current: {} as EnvVars, scopes: [] as (string | null)[] } }));
vi.mock('@/lib/env-vars', () => ({
  useEnvVars: (clusterID: string | null) => {
    env.scopes.push(clusterID);
    return env.current;
  },
}));
vi.mock('@/lib/cluster-scope', () => ({
  EVERY: '',
  useScope: (picked: string) => ({ present: [], archived: [], cluster: picked ? { id: picked } : undefined }),
}));
vi.mock('./scope-picker', () => ({
  ScopePicker: ({ onPick }: { onPick: (picked: string) => void }) => (
    <button type="button" onClick={() => onPick('prod')}>
      pick prod
    </button>
  ),
}));

const { EnvironmentSettings } = await import('./environment');

function withEnv(over: Partial<EnvVars>) {
  env.current = {
    vars: [],
    loadError: null,
    changing: false,
    removeError: null,
    set: vi.fn(async () => null),
    remove: vi.fn(async () => {}),
    ...over,
  };
}

const helm = { name: 'HELM_NAMESPACE', value: 'apps', target: 'Both', refusal: null } as const;

beforeEach(() => {
  withEnv({});
  env.scopes = [];
});

describe('EnvironmentSettings', () => {
  it("reads every cluster's variables, then the picked cluster's, closing any open form", async () => {
    render(<EnvironmentSettings />);
    expect(env.scopes.at(-1)).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));

    await userEvent.click(screen.getByRole('button', { name: 'pick prod' }));
    expect(env.scopes.at(-1)).toBe('prod');
    expect(screen.queryByRole('textbox', { name: 'Variable name' })).not.toBeInTheDocument();
  });

  it('says when the scope has no variable, and nothing before the list answers', () => {
    const { rerender } = render(<EnvironmentSettings />);
    expect(screen.getByText('No variables.')).toBeInTheDocument();

    withEnv({ vars: undefined });
    rerender(<EnvironmentSettings />);
    expect(screen.queryByText('No variables.')).not.toBeInTheDocument();
  });

  it('lists each variable, its name and value spelled out, with the commands it reaches', () => {
    withEnv({
      vars: [
        { ...helm, value: 'apps\u200b' },
        { name: 'AWS_PROFILE', value: 'dev', target: 'Outside', refusal: null },
        { name: 'PAGER', value: 'less', target: 'Sandbox', refusal: null },
      ],
    });
    render(<EnvironmentSettings />);
    const rows = within(screen.getByRole('list', { name: 'Variables' })).getAllByRole('listitem');
    expect(rows[0]).toHaveTextContent('HELM_NAMESPACE=apps\\u{200B}');
    expect(rows[0]).toHaveTextContent('All commands');
    expect(rows[1]).toHaveTextContent('Commands outside the sandbox');
    expect(rows[2]).toHaveTextContent('Sandboxed commands');
  });

  it('adds a variable for the commands picked, saying a value is no secret, and closes once it is taken', async () => {
    const user = userEvent.setup();
    render(<EnvironmentSettings />);
    await user.click(screen.getByRole('button', { name: 'Add variable' }));
    expect(screen.getByRole('button', { name: 'Add variable' })).toBeDisabled();
    expect(screen.getByText(/Not a secret/)).toHaveTextContent('any command can print it');
    const name = screen.getByRole('textbox', { name: 'Variable name' });
    const value = screen.getByRole('textbox', { name: 'Variable value' });
    expectCodeText(name);
    expectCodeText(value);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();

    await user.type(name, 'AWS_PROFILE');
    await user.type(value, 'dev');
    expect(screen.getByRole('combobox', { name: 'Given to' })).toHaveTextContent('All commands');
    await user.click(screen.getByRole('combobox', { name: 'Given to' }));
    await user.click(await screen.findByRole('option', { name: 'Commands outside the sandbox' }));
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(env.current.set).toHaveBeenCalledWith('AWS_PROFILE', 'dev', 'Outside');
    expect(screen.queryByRole('textbox', { name: 'Variable name' })).not.toBeInTheDocument();
  });

  it("keeps the form open with the refusal's reason, which closes with it", async () => {
    withEnv({ set: vi.fn(async () => 'Kubetail sets PATH itself.') });
    render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));
    await userEvent.type(screen.getByRole('textbox', { name: 'Variable name' }), 'PATH');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.getByRole('textbox', { name: 'Variable name' })).toBeInTheDocument();
    expect(screen.getByText('Kubetail sets PATH itself.')).toBeInTheDocument();

    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('textbox', { name: 'Variable name' })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));
    expect(screen.queryByText('Kubetail sets PATH itself.')).not.toBeInTheDocument();
  });

  it('edits a variable in place, its name fixed and its commands kept', async () => {
    withEnv({ vars: [{ ...helm, target: 'Sandbox' }] });
    render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));

    expect(screen.getByRole('textbox', { name: 'Variable name' })).toBeDisabled();
    const value = screen.getByRole('textbox', { name: 'Variable value' });
    expect(value).toHaveValue('apps');
    await userEvent.clear(value);
    await userEvent.type(value, 'web');
    await userEvent.click(screen.getByRole('button', { name: 'Save' }));

    expect(env.current.set).toHaveBeenCalledWith('HELM_NAMESPACE', 'web', 'Sandbox');
  });

  it('closes the form of a variable removed elsewhere', async () => {
    withEnv({ vars: [helm] });
    const { rerender } = render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(screen.getByRole('button', { name: 'Add variable' })).toBeDisabled();

    withEnv({ vars: [] });
    rerender(<EnvironmentSettings />);

    expect(screen.queryByRole('textbox', { name: 'Variable value' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add variable' })).toBeEnabled();
  });

  it('reopens an edit with the value another window set', async () => {
    withEnv({ vars: [helm] });
    const { rerender } = render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }));
    withEnv({ vars: [{ ...helm, value: 'web' }] });
    rerender(<EnvironmentSettings />);
    expect(screen.getByRole('textbox', { name: 'Variable value' })).toHaveValue('web');
  });

  it('removes a variable, and shows a refused remove', async () => {
    withEnv({ vars: [helm], removeError: 'That variable is not set.' });
    render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }));

    expect(env.current.remove).toHaveBeenCalledWith('HELM_NAMESPACE');
    expect(screen.getByText('That variable is not set.')).toBeInTheDocument();
  });

  it('keeps a value across lines', async () => {
    const user = userEvent.setup();
    render(<EnvironmentSettings />);
    await user.click(screen.getByRole('button', { name: 'Add variable' }));
    await user.type(screen.getByRole('textbox', { name: 'Variable name' }), 'CERT');
    await user.type(screen.getByRole('textbox', { name: 'Variable value' }), 'a{Enter}b');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(env.current.set).toHaveBeenCalledWith('CERT', 'a\nb', 'Both');
  });

  it('holds Edit while a form is open, so nothing typed is dropped', async () => {
    withEnv({ vars: [helm] });
    render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
  });

  it('says why the list could not be read', () => {
    withEnv({ vars: undefined, loadError: 'no app.db\u{202E}' });
    render(<EnvironmentSettings />);
    expect(screen.getByText('no app.db', { exact: false })).toHaveTextContent('no app.db\\u{202E}');
  });

  it('holds the buttons while a change is in flight', () => {
    withEnv({ vars: [helm], changing: true });
    render(<EnvironmentSettings />);
    expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled();
  });

  it('holds Cancel while a save is in flight, since its answer closes the form', async () => {
    const { rerender } = render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));
    withEnv({ changing: true });
    rerender(<EnvironmentSettings />);
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled();
  });

  it('refuses to add a name already listed', async () => {
    withEnv({ vars: [{ ...helm, name: 'EDITOR' }] });
    render(<EnvironmentSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Add variable' }));
    await userEvent.type(screen.getByRole('textbox', { name: 'Variable name' }), 'EDITOR');

    expect(screen.getByText(/is already listed\. Edit it instead\./)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
  });

  it('draws a refused variable on its own row with its reason, and removes it', async () => {
    withEnv({
      vars: [
        { name: 'LD_PRELOAD', value: '/x', target: 'Both', refusal: 'LD_PRELOAD changes how\u200b a program starts.' },
      ],
    });
    render(<EnvironmentSettings />);
    const row = within(screen.getByRole('list', { name: 'Variables' })).getByRole('listitem');
    expect(row).toHaveTextContent('LD_PRELOAD=/x');
    expect(row).toHaveTextContent('Not given to any command: LD_PRELOAD changes how\\u{200B} a program starts.');
    expect(row).not.toHaveTextContent('All commands');

    await userEvent.click(within(row).getByRole('button', { name: 'Remove' }));
    expect(env.current.remove).toHaveBeenCalledWith('LD_PRELOAD');
  });
});
