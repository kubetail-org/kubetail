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

import type {
  AWSActions,
  AWSConnection,
  ChatGPTActions,
  ChatGPTConnection,
  Connector,
  GitHubActions,
  GitHubConnection,
  GoogleActions,
  GoogleConnection,
  OpenRouterActions,
  OpenRouterConnection,
} from '@/lib/connectors';
import type { ProviderCredentialState, ProviderCredentialSource } from '@/lib/provider-credentials';

const { state } = vi.hoisted(() => {
  const shared: {
    connectors: Connector[];
    snapshotComplete: boolean;
    keysLoaded: boolean;
    openRouterSource: ProviderCredentialSource;
    openRouterFailure: ProviderCredentialState['failure'];
  } = { connectors: [], snapshotComplete: true, keysLoaded: true, openRouterSource: 'None', openRouterFailure: null };
  return { state: shared };
});
vi.mock('@/lib/connectors', () => ({
  GOOGLE_ID: 'google',
  OPENROUTER_ID: 'openrouter',
  useConnectors: () => ({ connectors: state.connectors, snapshotComplete: state.snapshotComplete }),
}));
vi.mock('@/lib/provider-credentials', () => ({
  useProviderCredentials: () => ({
    snapshotComplete: state.keysLoaded,
    credentials: [
      {
        id: 'openrouter',
        keyVariable: 'OPENROUTER_API_KEY',
        source: state.openRouterSource,
        failure: state.openRouterFailure,
      },
    ],
  }),
}));

const { ConnectorsSettings } = await import('./connectors');

function connector(name: string, connected: boolean, rest: Partial<Connector> = {}): Connector {
  return { id: name.toLowerCase(), name, detail: `${name} detail`, connected, ...rest };
}

beforeEach(() => {
  state.connectors = [connector('GitHub', true), connector('Google', false)];
  state.snapshotComplete = true;
  state.keysLoaded = true;
  state.openRouterSource = 'None';
  state.openRouterFailure = null;
  localStorage.clear();
  sessionStorage.clear();
});

describe('ConnectorsSettings', () => {
  it('lists every service in one list under its lede, by name over its detail', () => {
    state.connectors = [
      connector('GitHub', true),
      connector('OpenRouter', false),
      connector('AWS', true),
      connector('Google', false),
    ];
    render(<ConnectorsSettings />);

    expect(
      screen.getByText('Accounts Kubetail signs in to. Model providers and tools use their credentials.'),
    ).toBeInTheDocument();
    expect(screen.queryByRole('heading')).toBeNull();
    expect(screen.getAllByRole('listitem').map((r) => r.textContent)).toEqual([
      'AWSAWS detail',
      'GitHubGitHub detail',
      'GoogleGoogle detailComing soon',
      'OpenRouterOpenRouter detailComing soon',
    ]);
  });

  it('spells out control characters in the name and the detail', () => {
    state.connectors = [connector('Git\u001b[2JHub', true, { detail: 'Signed in as \u202eoctocat' })];
    render(<ConnectorsSettings />);

    const row = screen.getByRole('listitem');
    expect(within(row).getAllByTitle('An invisible character')).toHaveLength(2);
    for (const raw of ['\u001b', '\u202e']) expect(row.textContent).not.toContain(raw);
  });

  it('offers a disabled Coming soon on an available service alone', () => {
    render(<ConnectorsSettings />);

    const row = (name: string) => screen.getByText(name).closest('li')!;
    expect(within(row('Google')).getByRole('button', { name: 'Coming soon' })).toBeDisabled();
    expect(within(row('GitHub')).queryByRole('button', { name: 'Coming soon' })).toBeNull();
  });

  describe('row menu', () => {
    async function openMenu(name: string) {
      await userEvent.setup().click(screen.getByRole('button', { name: `Actions for ${name}` }));
      return screen.findByRole('menu');
    }

    it('lists Manage, Reconnect and Disconnect, each disabled', async () => {
      state.connectors = [connector('Claude', true, { manageLabel: 'Manage on Claude' })];
      render(<ConnectorsSettings />);

      const menu = await openMenu('Claude');
      const items = within(menu).getAllByRole('menuitem');
      expect(items.map((i) => i.textContent)).toEqual(['Manage on Claude', 'Reconnect', 'Disconnect']);
      for (const item of items) expect(item).toHaveAttribute('aria-disabled', 'true');
      expect(within(menu).getByRole('separator')).toBeInTheDocument();
      expect(items[2]).toHaveAttribute('data-variant', 'destructive');
    });

    it('leaves out Manage for a service with no manage label', async () => {
      state.connectors = [connector('GitHub', true)];
      render(<ConnectorsSettings />);

      const items = within(await openMenu('GitHub')).getAllByRole('menuitem');
      expect(items.map((i) => i.textContent)).toEqual(['Reconnect', 'Disconnect']);
    });
  });
});

describe('the ChatGPT row', () => {
  const actions: ChatGPTActions = { connect: vi.fn(), cancel: vi.fn(), disconnect: vi.fn(), openUsage: vi.fn() };

  function chatgpt(connection: Partial<ChatGPTConnection>): Connector {
    const c: ChatGPTConnection = {
      phase: 'SignedOut',
      email: null,
      failure: null,
      registered: false,
      revocationUnconfirmed: false,
      ...connection,
    };
    const connected = c.phase === 'Connected';
    return {
      id: 'chatgpt',
      name: 'ChatGPT',
      detail: connected ? (c.email ?? '') : 'OpenAI models on your ChatGPT plan',
      connected,
      chatgpt: { connection: c, actions },
    };
  }

  const row = () => screen.getByRole('listitem');

  beforeEach(() => vi.clearAllMocks());

  it('draws nothing before the watch answers', () => {
    state.snapshotComplete = false;
    const { container } = render(<ConnectorsSettings />);
    expect(container).toBeEmptyDOMElement();
  });

  it('offers Continue with ChatGPT while signed out, with no menu before a registration', async () => {
    state.connectors = [chatgpt({})];
    render(<ConnectorsSettings />);

    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
    expect(actions.connect).toHaveBeenCalledWith(false);
    expect(within(row()).queryByRole('button', { name: 'Actions for ChatGPT' })).toBeNull();
  });

  it('checks the keychain with no action while restoring, then draws its usual row', () => {
    state.connectors = [chatgpt({ phase: 'Restoring', registered: true })];
    const { rerender } = render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).queryByRole('button')).toBeNull();

    state.connectors = [chatgpt({ phase: 'SignedOut' })];
    rerender(<ConnectorsSettings />);
    expect(row()).toHaveTextContent('OpenAI models on your ChatGPT plan');
    expect(within(row()).getByRole('button', { name: 'Continue with ChatGPT' })).toBeEnabled();
  });

  it('waits for the browser while signing in, and cancels on a press', async () => {
    state.connectors = [chatgpt({ phase: 'SigningIn', registered: true })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Waiting for your browser…');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Cancel' }));
    expect(actions.cancel).toHaveBeenCalledOnce();
    expect(within(row()).queryByRole('button', { name: 'Actions for ChatGPT' })).toBeNull();
  });

  it('shows the account once connected, and offers Manage usage and Disconnect', async () => {
    const user = userEvent.setup();
    state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@\u202eexample.com', registered: true })];
    render(<ConnectorsSettings />);

    expect(within(row()).getByTitle('An invisible character')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Actions for ChatGPT' }));
    const items = within(await screen.findByRole('menu')).getAllByRole('menuitem');
    expect(items.map((i) => i.textContent)).toEqual(['Manage usage', 'Disconnect']);
    await user.click(items[0]);
    expect(actions.openUsage).toHaveBeenCalledOnce();
    await user.click(screen.getByRole('button', { name: 'Actions for ChatGPT' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }));
    expect(actions.disconnect).toHaveBeenCalledOnce();
  });

  it.each([
    ['Declined', 'You declined on ChatGPT’s page.'],
    ['SessionEnded', 'ChatGPT ended the session. Connect again.'],
    ['ScopeNotGranted', 'The sign-in did not allow use of your plan.'],
    ['NotSaved', 'This computer could not save the sign-in.'],
  ] as const)('says why it failed (%s), and offers the button again', (failure, text) => {
    state.connectors = [chatgpt({ phase: 'Failed', failure })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    expect(within(row()).getByRole('button', { name: 'Continue with ChatGPT' })).toBeEnabled();
  });

  it('offers Use a different account once registered', async () => {
    const user = userEvent.setup();
    state.connectors = [chatgpt({ phase: 'Failed', failure: 'AccountMismatch', registered: true })];
    render(<ConnectorsSettings />);

    await user.click(screen.getByRole('button', { name: 'Actions for ChatGPT' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Use a different account' }));
    expect(actions.connect).toHaveBeenCalledWith(true);
  });

  it('says when OpenAI did not confirm a sign-out, and opens its settings', async () => {
    state.connectors = [chatgpt({ revocationUnconfirmed: true, registered: true })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Signed out on this computer. ChatGPT did not confirm it.');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Open ChatGPT settings' }));
    expect(actions.openUsage).toHaveBeenCalledOnce();
  });

  describe('the first connection', () => {
    const dialog = () => screen.queryByRole('dialog', { name: "You're using your ChatGPT plan" });

    it('is confirmed once, in the window that connected', async () => {
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const view = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));

      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      view.rerender(<ConnectorsSettings />);

      expect(await screen.findByRole('dialog', { name: "You're using your ChatGPT plan" })).toBeInTheDocument();
      await user.click(screen.getByRole('button', { name: 'Got it' }));
      await waitFor(() => expect(dialog()).toBeNull());
      expect(localStorage.getItem('kubetail:chatgpt-plan-confirmed')).not.toBeNull();
    });

    // A hidden tab's pane unmounts: the sign-in can finish while another tab is open.
    it('is shown when the pane remounts after the sign-in finished elsewhere', async () => {
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const first = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
      first.unmount();

      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      const second = render(<ConnectorsSettings />);
      expect(await screen.findByRole('dialog', { name: "You're using your ChatGPT plan" })).toBeInTheDocument();
      await user.click(screen.getByRole('button', { name: 'Got it' }));
      await waitFor(() => expect(dialog()).toBeNull());
      second.unmount();

      // Dismissed, the window has nothing pending, whatever the shared key says.
      localStorage.clear();
      render(<ConnectorsSettings />);
      expect(dialog()).toBeNull();
    });

    // Cancelled here, then connected elsewhere: the dialog is the other window's.
    it('is not shown once the sign-in sent here has ended', async () => {
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const view = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
      state.connectors = [chatgpt({ phase: 'SigningIn' })];
      view.rerender(<ConnectorsSettings />);
      state.connectors = [chatgpt({ phase: 'SignedOut' })];
      view.rerender(<ConnectorsSettings />);

      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      view.rerender(<ConnectorsSettings />);

      expect(dialog()).toBeNull();
    });

    // The attempt failed while the pane was unmounted: mounting onto it ends it.
    it('is not shown after remounting onto a failed sign-in', async () => {
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const first = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
      first.unmount();

      state.connectors = [chatgpt({ phase: 'Failed', failure: 'Declined' })];
      const second = render(<ConnectorsSettings />);
      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      second.rerender(<ConnectorsSettings />);

      expect(dialog()).toBeNull();
    });

    it('is not shown again once another window has shown it', async () => {
      localStorage.setItem('kubetail:chatgpt-plan-confirmed', '1');
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const view = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));

      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      view.rerender(<ConnectorsSettings />);

      expect(dialog()).toBeNull();
    });

    it('is dismissed by Escape as by Got it', async () => {
      const user = userEvent.setup();
      state.connectors = [chatgpt({})];
      const view = render(<ConnectorsSettings />);
      await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      view.rerender(<ConnectorsSettings />);
      await screen.findByRole('dialog', { name: "You're using your ChatGPT plan" });

      await user.keyboard('{Escape}');

      await waitFor(() => expect(dialog()).toBeNull());
      expect(localStorage.getItem('kubetail:chatgpt-plan-confirmed')).not.toBeNull();
    });

    // Storage that refuses reads and writes cannot say the confirmation was seen,
    // so it is shown.
    it('is shown when storage refuses', async () => {
      const user = userEvent.setup();
      const get = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
        throw new Error('refused');
      });
      const set = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
        throw new Error('refused');
      });
      try {
        state.connectors = [chatgpt({})];
        const view = render(<ConnectorsSettings />);
        await user.click(within(row()).getByRole('button', { name: 'Continue with ChatGPT' }));
        state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
        view.rerender(<ConnectorsSettings />);

        expect(await screen.findByRole('dialog', { name: "You're using your ChatGPT plan" })).toBeInTheDocument();
        await user.click(screen.getByRole('button', { name: 'Got it' }));
      } finally {
        get.mockRestore();
        set.mockRestore();
      }
    });

    it('is not shown by a window that did not connect', () => {
      state.connectors = [chatgpt({})];
      const view = render(<ConnectorsSettings />);

      state.connectors = [chatgpt({ phase: 'Connected', email: 'ada@example.com' })];
      view.rerender(<ConnectorsSettings />);

      expect(dialog()).toBeNull();
      expect(localStorage.getItem('kubetail:chatgpt-plan-confirmed')).toBeNull();
    });
  });
});

describe('the OpenRouter row', () => {
  const actions: OpenRouterActions = { connect: vi.fn(), cancel: vi.fn(), disconnect: vi.fn(), openKeys: vi.fn() };

  function openRouter(connection: Partial<OpenRouterConnection>): Connector {
    const c: OpenRouterConnection = { phase: 'SignedOut', failure: null, ...connection };
    return {
      id: 'openrouter',
      name: 'OpenRouter',
      detail: 'Many providers on one account',
      connected: c.phase === 'Connected',
      openrouter: { connection: c, actions },
    };
  }

  const row = () => screen.getByRole('listitem');
  const menuButton = () => screen.queryByRole('button', { name: 'Actions for OpenRouter' });

  beforeEach(() => vi.clearAllMocks());

  it('offers Connect while signed out, with no menu', async () => {
    state.connectors = [openRouter({})];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Many providers on one account');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
    expect(menuButton()).toBeNull();
  });

  it('offers Connect after a failure with no reason, and draws the offer', () => {
    state.connectors = [openRouter({ phase: 'Failed' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Many providers on one account');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('checks the keychain with no action while restoring, then draws its usual row', () => {
    state.connectors = [openRouter({ phase: 'Restoring' })];
    const { rerender } = render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).queryByRole('button')).toBeNull();

    state.connectors = [openRouter({ phase: 'SignedOut' })];
    rerender(<ConnectorsSettings />);
    expect(row()).toHaveTextContent('Many providers on one account');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('waits for the browser while signing in, and cancels on a press', async () => {
    state.connectors = [openRouter({ phase: 'SigningIn' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Waiting for the browser…');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Cancel' }));
    expect(actions.cancel).toHaveBeenCalledOnce();
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    expect(menuButton()).toBeNull();
  });

  it('names the key once connected, and offers its settings and Disconnect', async () => {
    const user = userEvent.setup();
    state.connectors = [openRouter({ phase: 'Connected' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Key "Kubetail"');
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await user.click(menuButton()!);
    const items = within(await screen.findByRole('menu')).getAllByRole('menuitem');
    expect(items.map((i) => i.textContent)).toEqual(['Open OpenRouter settings', 'Disconnect']);
    expect(items[1]).toHaveAttribute('data-variant', 'destructive');
    await user.click(items[0]);
    expect(actions.openKeys).toHaveBeenCalledOnce();
  });

  describe('Disconnect', () => {
    const confirm = () => screen.queryByRole('dialog', { name: 'Disconnect OpenRouter?' });

    async function openConfirm() {
      const user = userEvent.setup();
      state.connectors = [openRouter({ phase: 'Connected' })];
      render(<ConnectorsSettings />);
      await user.click(menuButton()!);
      await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }));
      return { user, dialog: await screen.findByRole('dialog', { name: 'Disconnect OpenRouter?' }) };
    }

    it('says where the key stays live, and disconnects only once confirmed', async () => {
      const { user, dialog } = await openConfirm();

      expect(dialog).toHaveTextContent(
        'Kubetail will forget this key. It stays active on OpenRouter until you delete the key labelled "Kubetail" in your OpenRouter settings.',
      );
      expect(actions.disconnect).not.toHaveBeenCalled();
      await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }));

      expect(actions.disconnect).toHaveBeenCalledOnce();
      await waitFor(() => expect(confirm()).toBeNull());
    });

    it('opens the settings from the confirm, and Cancel sends nothing', async () => {
      const { user, dialog } = await openConfirm();

      await user.click(within(dialog).getByRole('button', { name: 'Open OpenRouter settings' }));
      expect(actions.openKeys).toHaveBeenCalledOnce();
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

      await waitFor(() => expect(confirm()).toBeNull());
      expect(actions.disconnect).not.toHaveBeenCalled();
    });

    it('is dismissed by Escape, sending nothing', async () => {
      const { user } = await openConfirm();

      await user.keyboard('{Escape}');

      await waitFor(() => expect(confirm()).toBeNull());
      expect(actions.disconnect).not.toHaveBeenCalled();
    });
  });

  it.each([
    ['SavedKey', 'Using a saved key'],
    ['EnvironmentKey', 'Using OPENROUTER_API_KEY'],
    ['None', 'Many providers on one account'],
  ] as const)('names the key that signs while signed out (%s), and offers Connect', (source, text) => {
    state.openRouterSource = source;
    state.connectors = [openRouter({ phase: 'SignedOut' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeInTheDocument();
  });

  it.each([
    ['SavedKey', 'KeyRejected', 'The saved key was rejected. Replace or remove it.'],
    ['EnvironmentKey', 'KeyRejected', 'OPENROUTER_API_KEY was rejected.'],
    ['None', 'NotDeleted', 'Kubetail could not remove the saved key. It returns after a restart.'],
  ] as const)('says what is wrong with the key that signs while signed out (%s, %s)', (source, failure, text) => {
    state.openRouterSource = source;
    state.openRouterFailure = failure;
    state.connectors = [openRouter({ phase: 'SignedOut' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
  });

  it('checks the keychain while signed out and its provider is restoring, and offers Connect', () => {
    state.openRouterSource = 'Restoring';
    state.connectors = [openRouter({ phase: 'SignedOut' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('draws nothing before the credentials load, so the detail never flips', () => {
    state.keysLoaded = false;
    state.openRouterSource = 'SavedKey';
    state.connectors = [openRouter({ phase: 'SignedOut' })];
    const { container } = render(<ConnectorsSettings />);

    expect(container).toBeEmptyDOMElement();
  });

  it.each([
    ['TimedOut', 'The sign-in timed out.'],
    ['Unavailable', 'OpenRouter could not be reached.'],
    ['Refused', 'OpenRouter refused the sign-in.'],
    ['NotSaved', 'This computer could not save the sign-in.'],
    ['KeyRejected', 'OpenRouter no longer accepts the key. Connect again.'],
  ] as const)('says why it failed (%s), and offers Connect again', async (failure, text) => {
    state.connectors = [openRouter({ phase: 'Failed', failure })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
  });

  it('says a key the keyring kept returns on restart, and offers Retry and the settings', async () => {
    const user = userEvent.setup();
    state.connectors = [openRouter({ phase: 'Failed', failure: 'NotDeleted' })];
    render(<ConnectorsSettings />);

    expect(within(row()).getByText('Kubetail could not remove the key from this machine.')).toBeInTheDocument();
    expect(row()).toHaveTextContent(
      'It will be used again after a restart. Retry, or delete the key labelled "Kubetail" on OpenRouter.',
    );
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await user.click(within(row()).getByRole('button', { name: 'Retry' }));
    expect(actions.disconnect).toHaveBeenCalledOnce();
    await user.click(within(row()).getByRole('button', { name: 'Open OpenRouter settings' }));
    expect(actions.openKeys).toHaveBeenCalledOnce();
  });
});

describe('the AWS row', () => {
  const actions: AWSActions = { connect: vi.fn(), cancel: vi.fn(), disconnect: vi.fn() };
  const arn = 'arn:aws:iam::123456789012:user/alice';

  function aws(connection: Partial<AWSConnection>): Connector {
    const c: AWSConnection = { phase: 'SignedOut', arn: null, failure: null, ...connection };
    const connected = c.phase === 'Connected';
    return {
      id: 'aws',
      name: 'AWS',
      detail: (connected && c.arn) || 'Cloud resources and Bedrock models',
      connected,
      aws: { connection: c, actions },
    };
  }

  const row = () => screen.getByRole('listitem');
  const menuButton = () => screen.queryByRole('button', { name: 'Actions for AWS' });

  beforeEach(() => vi.clearAllMocks());

  it('offers Connect while signed out, with no menu', async () => {
    state.connectors = [aws({})];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Cloud resources and Bedrock models');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
    expect(menuButton()).toBeNull();
  });

  it('checks the keychain with no action while restoring, then draws its usual row', () => {
    state.connectors = [aws({ phase: 'Restoring' })];
    const { rerender } = render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).queryByRole('button')).toBeNull();

    state.connectors = [aws({ phase: 'SignedOut' })];
    rerender(<ConnectorsSettings />);
    expect(row()).toHaveTextContent('Cloud resources and Bedrock models');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('waits for the browser while signing in, and cancels on a press', async () => {
    state.connectors = [aws({ phase: 'SigningIn' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Waiting for the browser…');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Cancel' }));
    expect(actions.cancel).toHaveBeenCalledOnce();
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
  });

  it('names the principal once connected, spelling out what is hidden in it', () => {
    state.connectors = [aws({ phase: 'Connected', arn: `${arn}\u202e` })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(arn);
    expect(within(row()).getByTitle('An invisible character')).toBeInTheDocument();
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
  });

  describe('Disconnect', () => {
    const confirm = () => screen.queryByRole('dialog', { name: 'Disconnect AWS?' });

    async function openConfirm() {
      const user = userEvent.setup();
      state.connectors = [aws({ phase: 'Connected', arn })];
      render(<ConnectorsSettings />);
      await user.click(menuButton()!);
      const items = within(await screen.findByRole('menu')).getAllByRole('menuitem');
      expect(items.map((i) => i.textContent)).toEqual(['Disconnect']);
      expect(items[0]).toHaveAttribute('data-variant', 'destructive');
      await user.click(items[0]);
      return { user, dialog: await screen.findByRole('dialog', { name: 'Disconnect AWS?' }) };
    }

    it('says the session stays valid at AWS, and disconnects only once confirmed', async () => {
      const { user, dialog } = await openConfirm();

      expect(dialog).toHaveTextContent(
        'Kubetail will forget this sign-in. AWS keeps the session valid until it expires.',
      );
      expect(actions.disconnect).not.toHaveBeenCalled();
      await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }));

      expect(actions.disconnect).toHaveBeenCalledOnce();
      await waitFor(() => expect(confirm()).toBeNull());
    });

    it('sends nothing on Cancel', async () => {
      const { user, dialog } = await openConfirm();

      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

      await waitFor(() => expect(confirm()).toBeNull());
      expect(actions.disconnect).not.toHaveBeenCalled();
    });
  });

  it.each([
    ['Declined', 'You declined on AWS’s page.'],
    ['TimedOut', 'The sign-in timed out.'],
    ['Unavailable', 'AWS could not be reached.'],
    ['Refused', 'AWS refused the sign-in.'],
    ['NotSaved', 'This computer could not save the sign-in.'],
    ['SessionEnded', 'AWS ended the session. Connect again.'],
  ] as const)('says why it failed (%s), and offers Connect again', async (failure, text) => {
    state.connectors = [aws({ phase: 'Failed', failure })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
  });

  it('offers Connect after a failure with no reason, and draws the offer', () => {
    state.connectors = [aws({ phase: 'Failed' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Cloud resources and Bedrock models');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('says a session the keyring kept returns on restart, and offers Retry alone', async () => {
    state.connectors = [aws({ phase: 'Failed', failure: 'NotDeleted' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('It will be used again after a restart.');
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Retry' }));
    expect(actions.disconnect).toHaveBeenCalledOnce();
  });
});

describe('the GitHub row', () => {
  const actions: GitHubActions = { connect: vi.fn(), cancel: vi.fn(), disconnect: vi.fn(), openSettings: vi.fn() };

  function gitHub(connection: Partial<GitHubConnection>): Connector {
    const c: GitHubConnection = { phase: 'SignedOut', login: null, userCode: null, failure: null, ...connection };
    const connected = c.phase === 'Connected';
    return {
      id: 'github',
      name: 'GitHub',
      detail: (connected && c.login) || 'Repositories for git and gh in the sandbox',
      connected,
      github: { connection: c, actions },
    };
  }

  const row = () => screen.getByRole('listitem');
  const menuButton = () => screen.queryByRole('button', { name: 'Actions for GitHub' });

  beforeEach(() => vi.clearAllMocks());

  it('offers Connect while signed out, with no menu', async () => {
    state.connectors = [gitHub({})];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Repositories for git and gh in the sandbox');
    expect(menuButton()).toBeNull();
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
  });

  it('checks the keychain with no action while restoring, then draws its usual row', () => {
    state.connectors = [gitHub({ phase: 'Restoring' })];
    const { rerender } = render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).queryByRole('button')).toBeNull();

    state.connectors = [gitHub({ phase: 'SignedOut' })];
    rerender(<ConnectorsSettings />);
    expect(row()).toHaveTextContent('Repositories for git and gh in the sandbox');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('offers Connect after a failure with no reason, and draws the offer', () => {
    state.connectors = [gitHub({ phase: 'Failed' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Repositories for git and gh in the sandbox');
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('asks GitHub for a code, then shows it to enter on GitHub, and cancels on a press', async () => {
    state.connectors = [gitHub({ phase: 'SigningIn' })];
    const { rerender } = render(<ConnectorsSettings />);
    expect(row()).toHaveTextContent('Asking GitHub for a code…');

    state.connectors = [gitHub({ phase: 'SigningIn', userCode: 'WDJB-MJHT' })];
    rerender(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Enter WDJB-MJHT on GitHub’s page');
    expect(within(row()).getByText('WDJB-MJHT').tagName).toBe('CODE');
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    expect(menuButton()).toBeNull();
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Cancel' }));
    expect(actions.cancel).toHaveBeenCalledOnce();
  });

  it('spells out control characters in the code', () => {
    state.connectors = [gitHub({ phase: 'SigningIn', userCode: 'WDJB\u202eMJHT' })];
    render(<ConnectorsSettings />);

    expect(within(row()).getByTitle('An invisible character')).toBeInTheDocument();
    expect(row().textContent).not.toContain('\u202e');
  });

  it('names the account once connected, and offers its settings and Disconnect', async () => {
    const user = userEvent.setup();
    state.connectors = [gitHub({ phase: 'Connected', login: 'octocat' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('octocat');
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await user.click(menuButton()!);
    const items = within(await screen.findByRole('menu')).getAllByRole('menuitem');
    expect(items.map((i) => i.textContent)).toEqual(['Open GitHub settings', 'Disconnect']);
    expect(items[1]).toHaveAttribute('data-variant', 'destructive');
    await user.click(items[0]);
    expect(actions.openSettings).toHaveBeenCalledOnce();
  });

  describe('Disconnect', () => {
    const confirm = () => screen.queryByRole('dialog', { name: 'Disconnect GitHub?' });

    async function openConfirm() {
      const user = userEvent.setup();
      state.connectors = [gitHub({ phase: 'Connected', login: 'octocat' })];
      render(<ConnectorsSettings />);
      await user.click(menuButton()!);
      await user.click(await screen.findByRole('menuitem', { name: 'Disconnect' }));
      return { user, dialog: await screen.findByRole('dialog', { name: 'Disconnect GitHub?' }) };
    }

    it('says where the token stays authorized, and disconnects only once confirmed', async () => {
      const { user, dialog } = await openConfirm();

      expect(dialog).toHaveTextContent(
        'Kubetail will forget this token. It stays authorized on GitHub until you revoke Kubetail in your GitHub settings.',
      );
      expect(actions.disconnect).not.toHaveBeenCalled();
      await user.click(within(dialog).getByRole('button', { name: 'Disconnect' }));

      expect(actions.disconnect).toHaveBeenCalledOnce();
      await waitFor(() => expect(confirm()).toBeNull());
    });

    it('opens the settings from the confirm, and Cancel sends nothing', async () => {
      const { user, dialog } = await openConfirm();

      await user.click(within(dialog).getByRole('button', { name: 'Open GitHub settings' }));
      expect(actions.openSettings).toHaveBeenCalledOnce();
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

      await waitFor(() => expect(confirm()).toBeNull());
      expect(actions.disconnect).not.toHaveBeenCalled();
    });
  });

  it.each([
    ['Declined', 'You declined on GitHub’s page.'],
    ['TimedOut', 'The sign-in timed out.'],
    ['Unavailable', 'GitHub could not be reached.'],
    ['Refused', 'GitHub refused the sign-in.'],
    ['NotSaved', 'This computer could not save the sign-in.'],
    ['KeyRejected', 'GitHub no longer accepts the key. Connect again.'],
  ] as const)('says why it failed (%s), and offers Connect again', async (failure, text) => {
    state.connectors = [gitHub({ phase: 'Failed', failure })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledOnce();
  });

  it('says a token the keyring kept returns on restart, and offers Retry and the settings', async () => {
    const user = userEvent.setup();
    state.connectors = [gitHub({ phase: 'Failed', failure: 'NotDeleted' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(
      'It will be used again after a restart. Retry, or revoke Kubetail in your GitHub settings.',
    );
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await user.click(within(row()).getByRole('button', { name: 'Retry' }));
    expect(actions.disconnect).toHaveBeenCalledOnce();
    await user.click(within(row()).getByRole('button', { name: 'Open GitHub settings' }));
    expect(actions.openSettings).toHaveBeenCalledOnce();
  });
});

describe('the Google row', () => {
  const actions: GoogleActions = { connect: vi.fn(), cancel: vi.fn(), disconnect: vi.fn() };
  const offer = 'Gemini models on Vertex AI';

  function google(connection: Partial<GoogleConnection>): Connector {
    const c: GoogleConnection = {
      phase: 'SignedOut',
      email: null,
      project: 'my-project',
      failure: null,
      revocationUnconfirmed: false,
      ...connection,
    };
    const connected = c.phase === 'Connected';
    return {
      id: 'google',
      name: 'Google',
      detail: (connected && c.email) || offer,
      connected,
      google: { connection: c, actions },
    };
  }

  const row = () => screen.getByRole('listitem');
  const menuButton = () => screen.queryByRole('button', { name: 'Actions for Google' });

  beforeEach(() => vi.clearAllMocks());

  it('offers Connect while signed out, with no menu', async () => {
    state.connectors = [google({})];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(offer);
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledWith(false);
    expect(menuButton()).toBeNull();
  });

  it('checks the keychain with no action while restoring, then draws its usual row', () => {
    state.connectors = [google({ phase: 'Restoring' })];
    const { rerender } = render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Checking your keychain…');
    expect(within(row()).queryByRole('button')).toBeNull();

    state.connectors = [google({ phase: 'SignedOut' })];
    rerender(<ConnectorsSettings />);
    expect(row()).toHaveTextContent(offer);
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('waits for the browser while signing in, and cancels on a press', async () => {
    state.connectors = [google({ phase: 'SigningIn' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('Waiting for the browser…');
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Cancel' }));
    expect(actions.cancel).toHaveBeenCalledOnce();
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
  });

  it('shows the account and the project once connected, and offers Disconnect', async () => {
    const user = userEvent.setup();
    state.connectors = [google({ phase: 'Connected', email: 'ada@example.com' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('ada@example.com · my-project');
    expect(within(row()).queryByRole('button', { name: 'Connect' })).toBeNull();
    await user.click(menuButton()!);
    const items = within(await screen.findByRole('menu')).getAllByRole('menuitem');
    expect(items.map((i) => i.textContent)).toEqual(['Disconnect']);
    await user.click(items[0]);
    expect(actions.disconnect).toHaveBeenCalledOnce();
  });

  it('says where to set a project when none was found', () => {
    state.connectors = [google({ phase: 'Connected', email: 'ada@example.com', project: null })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent('ada@example.com');
    expect(row()).toHaveTextContent(
      'No Google Cloud project. Set GOOGLE_CLOUD_PROJECT or gcloud’s default project, then restart Kubetail.',
    );
  });

  it.each([
    ['Declined', 'You declined on Google’s page.'],
    ['ScopeNotGranted', 'The sign-in did not allow access to Google Cloud.'],
    ['SessionEnded', 'Google ended the session. Connect again.'],
    ['NotSaved', 'This computer could not save the sign-in.'],
  ] as const)('says why it failed (%s), and offers Connect again', async (failure, text) => {
    state.connectors = [google({ phase: 'Failed', failure })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(text);
    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Connect' }));
    expect(actions.connect).toHaveBeenCalledWith(false);
  });

  it('offers Connect after a failure with no reason, and draws the offer', () => {
    state.connectors = [google({ phase: 'Failed' })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(offer);
    expect(within(row()).getByRole('button', { name: 'Connect' })).toBeEnabled();
  });

  it('offers Use a different account after another account signed in', async () => {
    state.connectors = [google({ phase: 'Failed', failure: 'AccountMismatch' })];
    render(<ConnectorsSettings />);

    await userEvent.setup().click(within(row()).getByRole('button', { name: 'Use a different account' }));
    expect(actions.connect).toHaveBeenCalledWith(true);
  });

  it('says when Google did not confirm a sign-out', () => {
    state.connectors = [google({ revocationUnconfirmed: true })];
    render(<ConnectorsSettings />);

    expect(row()).toHaveTextContent(
      'Signed out on this computer. Google did not confirm it. To be sure, remove Kubetail in your Google Account’s third-party connections.',
    );
  });
});
