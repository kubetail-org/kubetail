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

import type { ProviderCredentialState, SaveKeyResult } from '@/lib/provider-credentials';
import { expectCodeText, mockTauriCore } from '@/test-utils';

const { factory } = mockTauriCore();
vi.mock('@tauri-apps/api/core', () => factory());

const { SelectSettingsTab } = await import('@/lib/settings-tab');
const { ProviderSetupForm } = await import('./provider-setup-form');

const KEY = 'sk-typed-0123456789abcdef';

function credential(c: Partial<ProviderCredentialState> = {}): ProviderCredentialState {
  return {
    id: 'openai',
    label: 'OpenAI',
    keyVariable: 'OPENAI_API_KEY',
    source: 'None',
    hasEnvironmentKey: false,
    hasSavedKey: false,
    failure: null,
    ...c,
  };
}

describe('ProviderSetupForm', () => {
  it("switches Bedrock's fields with its credential source", async () => {
    const user = userEvent.setup();
    render(<ProviderSetupForm sources={['aws-connector', 'access-key']} onClose={vi.fn()} />);

    expect(screen.getByRole('radiogroup', { name: 'Credentials' })).toBeInTheDocument();
    expect(screen.getByRole('radio', { name: 'AWS connector' })).toBeChecked();
    expect(screen.getByRole('radio', { name: 'AWS connector' })).toHaveAccessibleDescription(
      'Sign in through an AWS connector.',
    );
    expect(screen.getByRole('combobox', { name: 'Connector' })).toBeInTheDocument();
    expect(screen.getByRole('combobox', { name: 'Region' })).toHaveTextContent('us-east-1');

    await user.click(screen.getByRole('radio', { name: 'Access key' }));

    expect(screen.getByRole('radio', { name: 'Access key' })).toBeChecked();
    expect(screen.queryByRole('combobox')).toBeNull();
  });

  it('marks each source that awaits its design as coming soon', () => {
    render(<ProviderSetupForm sources={['environment', 'api-key', 'azure-connector']} onClose={vi.fn()} />);

    expect(screen.getAllByText('Coming soon')).toHaveLength(1);
    expect(screen.getByRole('radio', { name: 'Azure connector' })).toHaveAccessibleDescription(
      'Sign in with your Azure account.',
    );
  });

  it.each([
    ['azure-connector', 'Azure connector', 'Sign in with your Azure account.'],
    ['host', 'Host', 'The address of the server that runs the models.'],
  ] as const)('describes %s, and draws no fields for it', (source, name, description) => {
    render(<ProviderSetupForm sources={[source]} onClose={vi.fn()} />);

    expect(screen.getByRole('radio', { name })).toHaveAccessibleDescription(description);
    expect(screen.getByText('Coming soon')).toBeInTheDocument();
    expect(screen.queryByRole('textbox')).toBeNull();
  });

  it.each([
    ['SavedKey', 'Saved key'],
    ['EnvironmentKey', 'Environment variable'],
    ['Connector', 'OpenRouter connector'],
    ['None', 'Environment variable'],
    ['Restoring', 'Environment variable'],
  ] as const)('opens on the source that signs, %s', (source, name) => {
    render(
      <ProviderSetupForm
        sources={['environment', 'api-key', 'openrouter-connector']}
        credential={credential({ source })}
        onClose={vi.fn()}
      />,
    );
    expect(screen.getByRole('radio', { name })).toBeChecked();
  });

  it('saves a key through the hook, then clears the box and closes', async () => {
    const user = userEvent.setup();
    const onSaveKey = vi.fn((_key: string) => Promise.resolve<SaveKeyResult>('saved'));
    const onClose = vi.fn();
    render(
      <ProviderSetupForm
        sources={['environment', 'api-key']}
        credential={credential()}
        onSaveKey={onSaveKey}
        onClose={onClose}
      />,
    );
    await user.click(screen.getByRole('radio', { name: 'Saved key' }));

    const key = screen.getByLabelText('API key', { selector: 'input' });
    expect(key).toHaveAttribute('type', 'password');
    expect(key).toHaveAttribute('autocomplete', 'off');
    expect(key).toHaveAttribute('spellcheck', 'false');
    expectCodeText(key);
    const save = screen.getByRole('button', { name: 'Save' });
    expect(save).toBeDisabled();
    await user.type(key, '   ');
    expect(save).toBeDisabled();

    await user.clear(key);
    await user.type(key, KEY);
    await user.click(save);

    expect(onSaveKey).toHaveBeenCalledExactlyOnceWith(KEY);
    expect(key).toHaveValue('');
    expect(onClose).toHaveBeenCalledOnce();
    expect(document.body).not.toHaveTextContent(KEY);
  });

  it.each([
    ['bad-key', 'That does not look like an API key.'],
    ['not-saved', "Your system's password store refused the key. Nothing was saved."],
    ['failed', 'Kubetail could not save the key.'],
  ] as const)('keeps the key and says why on %s', async (result, line) => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(
      <ProviderSetupForm
        sources={['api-key']}
        credential={credential()}
        onSaveKey={() => Promise.resolve(result)}
        onClose={onClose}
      />,
    );
    const key = screen.getByLabelText('API key', { selector: 'input' });
    await user.type(key, KEY);
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(line);
    expect(key).toHaveValue(KEY);
    expect(onClose).not.toHaveBeenCalled();
  });

  it("clears a failed save's line once the key is edited", async () => {
    const user = userEvent.setup();
    render(
      <ProviderSetupForm
        sources={['api-key']}
        credential={credential()}
        onSaveKey={() => Promise.resolve('bad-key')}
        onClose={vi.fn()}
      />,
    );
    const key = screen.getByLabelText('API key', { selector: 'input' });
    await user.type(key, KEY);
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toBeInTheDocument();

    await user.type(key, 'x');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  // Placeholder sources: nothing saves them yet, so Save is drawn disabled.
  it('holds Save disabled on a source nothing saves yet', async () => {
    const user = userEvent.setup();
    render(<ProviderSetupForm sources={['aws-connector', 'openrouter-connector']} onClose={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();

    await user.click(screen.getByRole('radio', { name: 'OpenRouter connector' }));
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Open Connectors' })).toBeInTheDocument();
  });

  it('holds Save down while a save is in flight', async () => {
    const user = userEvent.setup();
    let finish: (r: SaveKeyResult) => void = () => {};
    const onSaveKey = vi.fn(
      () =>
        new Promise<SaveKeyResult>((resolve) => {
          finish = resolve;
        }),
    );
    render(
      <ProviderSetupForm sources={['api-key']} credential={credential()} onSaveKey={onSaveKey} onClose={vi.fn()} />,
    );
    await user.type(screen.getByLabelText('API key', { selector: 'input' }), KEY);
    await user.click(screen.getByRole('button', { name: 'Save' }));

    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    finish('failed');
    expect(await screen.findByRole('alert')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled();
  });

  // With no way to save, as on a placeholder row, no secret is typed into the webview.
  it('draws the saved key box disabled without a save', async () => {
    const user = userEvent.setup();
    render(<ProviderSetupForm sources={['azure-connector', 'api-key']} onClose={vi.fn()} />);

    await user.click(screen.getByRole('radio', { name: 'Saved key' }));

    expect(screen.getByLabelText('API key', { selector: 'input' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    expect(screen.getByRole('radio', { name: 'Saved key' })).toHaveAccessibleDescription(
      "A key Kubetail keeps in your system's password store.",
    );
  });

  it('says whether the key variable is set, and what is used over it', () => {
    const { rerender } = render(
      <ProviderSetupForm sources={['environment', 'api-key']} credential={credential()} onClose={vi.fn()} />,
    );
    const env = () => screen.getByRole('radio', { name: 'Environment variable' });
    expect(env()).toHaveAccessibleDescription(
      'OPENAI_API_KEY is not set. Kubetail reads it from your login shell when it starts.',
    );

    rerender(
      <ProviderSetupForm
        sources={['environment', 'api-key']}
        credential={credential({ hasEnvironmentKey: true, source: 'EnvironmentKey' })}
        onClose={vi.fn()}
      />,
    );
    expect(env()).toHaveAccessibleDescription('Kubetail uses OPENAI_API_KEY from your environment.');

    rerender(
      <ProviderSetupForm
        sources={['environment', 'api-key']}
        credential={credential({ hasEnvironmentKey: true, hasSavedKey: true, source: 'SavedKey' })}
        onClose={vi.fn()}
      />,
    );
    expect(env()).toHaveAccessibleDescription(
      'Kubetail uses OPENAI_API_KEY from your environment. A saved key or a connector is used over it.',
    );
    expect(screen.getByRole('radio', { name: 'Saved key' })).toHaveAccessibleDescription(
      'A key is saved. Saving another replaces it.',
    );
  });

  it.each([
    ['openrouter-connector', 'OpenRouter connector', 'Sign in with your OpenRouter account.'],
    ['google-connector', 'Google connector', 'Sign in with your Google account.'],
  ] as const)('sends %s to the Connectors tab, closing the form', async (connector, name, description) => {
    const user = userEvent.setup();
    const selectTab = vi.fn();
    const onClose = vi.fn();
    render(
      <SelectSettingsTab value={selectTab}>
        <ProviderSetupForm sources={['environment', connector]} onClose={onClose} />
      </SelectSettingsTab>,
    );

    const source = screen.getByRole('radio', { name });
    expect(source).toHaveAccessibleDescription(description);
    expect(screen.queryByText('Coming soon')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open Connectors' })).toBeNull();

    await user.click(source);
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
    await user.click(screen.getByRole('button', { name: 'Open Connectors' }));

    expect(onClose).toHaveBeenCalledOnce();
    expect(selectTab).toHaveBeenCalledExactlyOnceWith('Connectors');
  });

  it('collapses on Cancel', async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<ProviderSetupForm sources={['aws-connector']} onClose={onClose} />);

    await user.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(onClose).toHaveBeenCalledOnce();
  });
});
