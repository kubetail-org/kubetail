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

import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { Catalog, Model } from '@/lib/models';
import type { ProviderCredentialState, ProviderCredentials } from '@/lib/provider-credentials';

const { catalog, keys } = vi.hoisted(() => ({
  catalog: { current: undefined as unknown as Catalog },
  keys: { current: undefined as unknown as ProviderCredentials },
}));
vi.mock('@/lib/models', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/models')>()),
  useModels: () => catalog.current,
}));
vi.mock('@/lib/provider-credentials', () => ({ useProviderCredentials: () => keys.current }));

const { SelectSettingsTab } = await import('@/lib/settings-tab');
const { keyFailureText, ProvidersSettings } = await import('./providers');

function model(providerID: string, label: string, id: string): Model {
  return { provider: { id: providerID, label }, id, label: id, efforts: [], defaultEffort: '' };
}

function serve(models: Model[], state: Partial<Catalog> = {}) {
  catalog.current = { models, snapshotComplete: true, ...state };
}

// Every provider, as the sidecar lists them, each with no credential. The last
// two take no key: only their connectors sign them.
const CREDENTIALS: [string, string, string | null][] = [
  ['anthropic', 'Anthropic', 'ANTHROPIC_API_KEY'],
  ['openai', 'OpenAI', 'OPENAI_API_KEY'],
  ['gemini', 'Gemini', 'GEMINI_API_KEY'],
  ['groq', 'Groq', 'GROQ_API_KEY'],
  ['mistral', 'Mistral', 'MISTRAL_API_KEY'],
  ['deepseek', 'DeepSeek', 'DEEPSEEK_API_KEY'],
  ['xai', 'xAI', 'XAI_API_KEY'],
  ['openrouter', 'OpenRouter', 'OPENROUTER_API_KEY'],
  ['together', 'Together', 'TOGETHER_API_KEY'],
  ['fireworks', 'Fireworks', 'FIREWORKS_API_KEY'],
  ['opencode-go', 'OpenCode Go', 'OPENCODE_GO_API_KEY'],
  ['chatgpt', 'ChatGPT plan', null],
  ['google', 'Vertex AI', null],
];

// Serves the state watch: every provider with no credential, but for changes, by id,
// and less the ids in omit.
function credentials(
  changes: Record<string, Partial<ProviderCredentialState>> = {},
  snapshotComplete = true,
  omit: string[] = [],
) {
  keys.current = {
    snapshotComplete,
    credentials: CREDENTIALS.filter(([id]) => !omit.includes(id)).map(([id, label, keyVariable]) => ({
      id,
      label,
      keyVariable,
      source: 'None',
      hasEnvironmentKey: false,
      hasSavedKey: false,
      failure: null,
      ...changes[id],
    })),
    saveKey: vi.fn(() => Promise.resolve('saved' as const)),
    deleteKey: vi.fn(() => Promise.resolve(true)),
  };
}

const list = () => screen.getByRole('list');
const rows = () => within(list()).getAllByRole('listitem');
const names = () => rows().map((r) => r.querySelector('[data-slot="name"]')?.textContent);
const row = (name: string) => within(list()).getByText(name).closest('li')!;

// Every provider, by name.
const ALL = [
  'Amazon Bedrock',
  'Anthropic',
  'ChatGPT plan',
  'DeepSeek',
  'Fireworks',
  'Gemini',
  'Groq',
  'Microsoft Foundry',
  'Mistral',
  'Ollama',
  'OpenAI',
  'OpenCode Go',
  'OpenRouter',
  'Together',
  'Vertex AI',
  'xAI',
];

// What each provider with no key variable lacks.
const MISSING: Record<string, string> = {
  'Amazon Bedrock': 'Needs credentials',
  'ChatGPT plan': 'Not connected',
  'Vertex AI': 'Not connected',
  'Microsoft Foundry': 'Needs credentials',
  Ollama: 'No host set',
};

// The providers only a connector sets up.
const CONNECTOR_ONLY = ['ChatGPT plan', 'Vertex AI'];

async function openMenu(name: string) {
  await userEvent.setup().click(screen.getByRole('button', { name: `${name} actions` }));
}

beforeEach(() => {
  serve([]);
  credentials();
});

describe('ProvidersSettings', () => {
  it('lists every keyed provider and the other providers, by name', () => {
    render(<ProvidersSettings />);

    expect(
      screen.getByText("Configure your chats' models. Enable model providers with connectors or API keys."),
    ).toBeInTheDocument();
    expect(names()).toEqual(ALL);
    for (const name of ALL) {
      expect(row(name)).toHaveTextContent(MISSING[name] ?? 'No API key found');
      const action = CONNECTOR_ONLY.includes(name) ? 'Connect' : 'Set up';
      expect(within(row(name)).getByRole('button', { name: action })).toBeInTheDocument();
    }
  });

  it('draws a label from the state watch through VisibleText', () => {
    credentials({ groq: { label: 'Gro​q' } });
    render(<ProvidersSettings />);
    expect(names()).toContain('Gro\\u{200B}q');
  });

  it.each([
    [
      { failure: 'NotDeleted', source: 'EnvironmentKey' },
      'Kubetail could not remove the saved key. It returns after a restart.',
    ],
    [{ failure: 'KeyRejected', source: 'SavedKey' }, 'The saved key was rejected. Replace or remove it.'],
    [{ failure: 'KeyRejected', source: 'EnvironmentKey' }, 'OPENAI_API_KEY was rejected.'],
    [{ source: 'Connector' }, 'Connected on the Connectors tab'],
    [{ source: 'SavedKey' }, 'Using a saved key'],
    [{ source: 'EnvironmentKey' }, 'Found OPENAI_API_KEY'],
    [{ source: 'None' }, 'No API key found'],
    [{ source: 'Restoring' }, 'Checking your keychain…'],
  ] as const)('draws the detail for %o', (state, line) => {
    credentials({ openai: state });
    render(<ProvidersSettings />);
    expect(row('OpenAI')).toHaveTextContent(line);
  });

  it("draws a restoring provider's dot as a dashed ring, neither set up nor not", () => {
    credentials({ anthropic: { source: 'Restoring', hasEnvironmentKey: true } });
    render(<ProvidersSettings />);

    const status = row('Anthropic').querySelector('[data-slot="status"]');
    expect(status).not.toHaveAttribute('data-set-up');
    expect(status).toHaveClass('border-dashed');
  });

  it('marks a keyed provider set up while it has a credential, with a menu', async () => {
    credentials({ anthropic: { source: 'EnvironmentKey', hasEnvironmentKey: true } });
    render(<ProvidersSettings />);

    const status = (name: string) => row(name).querySelector('[data-slot="status"]');
    expect(status('Anthropic')).toHaveAttribute('data-set-up', 'true');
    expect(status('Groq')).toHaveAttribute('data-set-up', 'false');
    expect(status('Anthropic')).toHaveAttribute('aria-hidden', 'true');
    expect(within(row('Anthropic')).queryByRole('button', { name: 'Set up' })).toBeNull();

    await openMenu('Anthropic');
    expect(await screen.findByRole('menuitem', { name: 'Configure…' })).not.toHaveAttribute('aria-disabled');
    expect(screen.getByRole('menuitem', { name: 'Disable' })).toHaveAttribute('aria-disabled', 'true');
    expect(screen.queryByRole('menuitem', { name: 'Remove key…' })).toBeNull();
  });

  it("opens a keyed provider's form on the source that signs, and saves a key through the hook", async () => {
    const user = userEvent.setup();
    credentials({ openai: { source: 'SavedKey', hasSavedKey: true } });
    render(<ProvidersSettings />);

    await openMenu('OpenAI');
    await user.click(await screen.findByRole('menuitem', { name: 'Configure…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Configure OpenAI' });
    expect(within(dialog).getByRole('radio', { name: 'Saved key' })).toBeChecked();
    // Connecting ChatGPT never sets up the openai provider.
    expect(within(dialog).queryByRole('radio', { name: 'ChatGPT connector' })).toBeNull();

    await user.type(within(dialog).getByLabelText('API key', { selector: 'input' }), 'sk-new-0123456789abcdef');
    await user.click(within(dialog).getByRole('button', { name: 'Save' }));

    expect(keys.current.saveKey).toHaveBeenCalledExactlyOnceWith('openai', 'sk-new-0123456789abcdef');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it.each([
    [{ source: 'Connector', hasSavedKey: true }, 'OpenAI stays connected on the Connectors tab.'],
    [
      { source: 'SavedKey', hasSavedKey: true, hasEnvironmentKey: true },
      'Kubetail will use OPENAI_API_KEY from your environment.',
    ],
    [{ source: 'SavedKey', hasSavedKey: true }, "OpenAI's models leave the picker."],
    [{ source: 'None', failure: 'NotDeleted' }, "OpenAI's models leave the picker."],
  ] as const)('removes a saved key behind a confirm naming what follows, for %o', async (state, follows) => {
    const user = userEvent.setup();
    credentials({ openai: state });
    render(<ProvidersSettings />);

    await openMenu('OpenAI');
    await user.click(await screen.findByRole('menuitem', { name: 'Remove key…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Remove the saved key for OpenAI?' });
    expect(dialog).toHaveTextContent(follows);
    expect(keys.current.deleteKey).not.toHaveBeenCalled();

    await user.click(within(dialog).getByRole('button', { name: 'Remove' }));
    expect(keys.current.deleteKey).toHaveBeenCalledExactlyOnceWith('openai');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('follows the state watch while a dialog is open', async () => {
    const user = userEvent.setup();
    credentials({ openai: { source: 'EnvironmentKey', hasEnvironmentKey: true } });
    const { rerender } = render(<ProvidersSettings />);
    await openMenu('OpenAI');
    await user.click(await screen.findByRole('menuitem', { name: 'Configure…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Configure OpenAI' });
    expect(within(dialog).getByRole('radio', { name: 'Saved key' })).toHaveAccessibleDescription(
      "A key Kubetail keeps in your system's password store.",
    );

    // Another window saves a key.
    credentials({ openai: { source: 'SavedKey', hasSavedKey: true, hasEnvironmentKey: true } });
    rerender(<ProvidersSettings />);
    expect(within(dialog).getByRole('radio', { name: 'Saved key' })).toHaveAccessibleDescription(
      'A key is saved. Saving another replaces it.',
    );
  });

  it('keeps the key when the confirm is cancelled', async () => {
    const user = userEvent.setup();
    credentials({ openai: { source: 'SavedKey', hasSavedKey: true } });
    render(<ProvidersSettings />);

    await openMenu('OpenAI');
    await user.click(await screen.findByRole('menuitem', { name: 'Remove key…' }));
    const dialog = await screen.findByRole('dialog', { name: 'Remove the saved key for OpenAI?' });
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(keys.current.deleteKey).not.toHaveBeenCalled();
  });

  it('lists a provider the catalog serves beyond the known ones, with nothing to configure', async () => {
    serve([model('fake', 'Fake', 'echo')]);
    render(<ProvidersSettings />);

    expect(row('Fake')).toHaveTextContent('Configured');
    await openMenu('Fake');
    expect(await screen.findByRole('menuitem', { name: 'Configure…' })).toHaveAttribute('aria-disabled', 'true');
  });

  it('marks a placeholder set up while the catalog serves it', () => {
    serve([model('ollama', 'Ollama', 'llama')]);
    render(<ProvidersSettings />);

    expect(row('Ollama')).toHaveTextContent('Configured');
    expect(row('Ollama').querySelector('[data-slot="status"]')).toHaveAttribute('data-set-up', 'true');
  });

  it('draws a keyed provider the catalog serves once', () => {
    serve([model('gemini', 'Gemini', 'flash')]);
    render(<ProvidersSettings />);
    expect(names()).toEqual(ALL);
  });

  it.each(CONNECTOR_ONLY)('sends %s, which only a connector sets up, to the Connectors tab', async (name) => {
    const user = userEvent.setup();
    const selectTab = vi.fn();
    render(
      <SelectSettingsTab value={selectTab}>
        <ProvidersSettings />
      </SelectSettingsTab>,
    );

    const r = row(name);
    expect(r).toHaveTextContent('Not connected');
    expect(within(r).queryByRole('button', { name: 'Set up' })).toBeNull();
    await user.click(within(r).getByRole('button', { name: 'Connect' }));

    expect(selectTab).toHaveBeenCalledExactlyOnceWith('Connectors');
  });

  it.each([
    ['google', 'Vertex AI', 'Google connector'],
    ['chatgpt', 'ChatGPT plan', 'ChatGPT connector'],
  ])("draws %s's connected row from the state watch, configured on the Connectors tab", async (id, name, connector) => {
    const user = userEvent.setup();
    const selectTab = vi.fn();
    credentials({ [id]: { source: 'Connector' } });
    render(
      <SelectSettingsTab value={selectTab}>
        <ProvidersSettings />
      </SelectSettingsTab>,
    );

    expect(names()).toEqual(ALL);
    expect(row(name)).toHaveTextContent('Connected on the Connectors tab');
    await openMenu(name);
    expect(screen.queryByRole('menuitem', { name: 'Remove key…' })).toBeNull();
    await user.click(await screen.findByRole('menuitem', { name: 'Configure…' }));

    const dialog = await screen.findByRole('dialog', { name: `Configure ${name}` });
    expect(within(dialog).getAllByRole('radio')).toHaveLength(1);
    expect(within(dialog).getByRole('radio', { name: connector })).toBeChecked();
    expect(within(dialog).queryByLabelText('API key', { selector: 'input' })).toBeNull();
    expect(dialog).not.toHaveTextContent('Coming soon');
    await user.click(within(dialog).getByRole('button', { name: 'Open Connectors' }));
    expect(selectTab).toHaveBeenCalledExactlyOnceWith('Connectors');
  });

  it.each([
    ['chatgpt', 'ChatGPT plan'],
    ['google', 'Vertex AI'],
  ])('checks the keychain for %s, which takes no key, while it restores', (id, name) => {
    credentials({ [id]: { source: 'Restoring' } });
    render(<ProvidersSettings />);

    expect(row(name)).toHaveTextContent('Checking your keychain…');
    expect(row(name)).not.toHaveTextContent('Not connected');
  });

  it('offers no action on a restoring provider, with a key or without one', () => {
    credentials({ anthropic: { source: 'Restoring' }, chatgpt: { source: 'Restoring' } });
    render(<ProvidersSettings />);

    expect(within(row('Anthropic')).queryByRole('button')).toBeNull();
    expect(within(row('ChatGPT plan')).queryByRole('button')).toBeNull();
  });

  it.each([
    ['SavedKey', 'anthropic', 'Anthropic', 'Using a saved key', 'true', 'Anthropic actions'],
    ['None', 'anthropic', 'Anthropic', 'No API key found', 'false', 'Set up'],
    ['Connector', 'chatgpt', 'ChatGPT plan', 'Connected on the Connectors tab', 'true', 'ChatGPT plan actions'],
    ['None', 'chatgpt', 'ChatGPT plan', 'Not connected', 'false', 'Connect'],
  ] as const)('draws the usual row once restored to %s (%s)', (source, id, name, detail, setUp, action) => {
    credentials({ [id]: { source: 'Restoring' } });
    const { rerender } = render(<ProvidersSettings />);

    credentials({ [id]: { source, hasSavedKey: source === 'SavedKey' } });
    rerender(<ProvidersSettings />);
    expect(row(name)).toHaveTextContent(detail);
    expect(row(name).querySelector('[data-slot="status"]')).toHaveAttribute('data-set-up', setUp);
    expect(within(row(name)).getByRole('button', { name: action })).toBeInTheDocument();
  });

  it('reads no key failure on a provider that takes no key', () => {
    const plan: ProviderCredentialState = {
      id: 'chatgpt',
      label: 'ChatGPT plan',
      keyVariable: null,
      source: 'None',
      hasEnvironmentKey: false,
      hasSavedKey: false,
      failure: 'KeyRejected',
    };
    expect(keyFailureText(plan)).toBeUndefined();
  });

  it('draws no Vertex AI row when the state watch has none', () => {
    credentials({}, true, ['google']);
    render(<ProvidersSettings />);
    expect(names()).toEqual(ALL.filter((n) => n !== 'Vertex AI'));
  });

  it("offers the OpenRouter connector in OpenRouter's form, and no Google sign-in in Gemini's", async () => {
    const user = userEvent.setup();
    render(<ProvidersSettings />);

    await user.click(within(row('OpenRouter')).getByRole('button', { name: 'Set up' }));
    let dialog = await screen.findByRole('dialog', { name: 'Set up OpenRouter' });
    expect(within(dialog).getByRole('radio', { name: 'OpenRouter connector' })).toBeInTheDocument();
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());

    await user.click(within(row('Gemini')).getByRole('button', { name: 'Set up' }));
    dialog = await screen.findByRole('dialog', { name: 'Set up Gemini' });
    expect(within(dialog).getByRole('radio', { name: 'Environment variable' })).toBeChecked();
    expect(within(dialog).queryByRole('radio', { name: 'Google connector' })).toBeNull();
    expect(dialog).toHaveTextContent('GEMINI_API_KEY is not set.');
  });

  it('draws nothing before the state watch answers', () => {
    credentials({}, false);
    render(<ProvidersSettings />);
    expect(screen.queryByRole('list')).toBeNull();
  });

  it('draws nothing before the catalog answers', () => {
    serve([], { snapshotComplete: false });
    render(<ProvidersSettings />);
    expect(screen.queryByRole('list')).toBeNull();
  });

  it("opens a placeholder's form in a dialog from Set up, which Cancel closes", async () => {
    const user = userEvent.setup();
    render(<ProvidersSettings />);

    await user.click(within(row('Amazon Bedrock')).getByRole('button', { name: 'Set up' }));
    const dialog = await screen.findByRole('dialog', { name: 'Set up Amazon Bedrock' });
    expect(within(dialog).getByRole('radio', { name: 'AWS connector' })).toBeChecked();

    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});
