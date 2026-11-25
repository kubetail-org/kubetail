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

// Four seams: the sandbox query, the clusters watch, the cloud platforms
// offered, and urql, whose query
// answers from state and whose mutations are one mock, told apart by the
// operation they send.
const {
  sandboxState,
  clustersState,
  settingsState,
  queryErrorState,
  mutateMock,
  queryArgs,
  reexecuteMock,
  platformsState,
} = vi.hoisted(() => {
  const sandbox: { current: { available: boolean | undefined } } = { current: { available: true } };
  const queryError: { current: unknown } = { current: undefined };
  const clusters: { current: unknown[] | null } = { current: null };
  const settingsValue: { current: unknown } = { current: undefined };
  const args: unknown[] = [];
  return {
    sandboxState: sandbox,
    clustersState: clusters,
    settingsState: settingsValue,
    queryErrorState: queryError,
    mutateMock: vi.fn(),
    queryArgs: args,
    reexecuteMock: vi.fn(),
    platformsState: { current: ['AWS'] as string[] | undefined },
  };
});
vi.mock('@/lib/sandbox', () => ({ useSandbox: () => ({ ...sandboxState.current, failed: false, retry: () => {} }) }));
vi.mock('@/lib/sandbox-cloud-access', () => ({
  useSandboxCloudAccess: () => ({ platforms: platformsState.current, readFailed: false }),
}));
vi.mock('@/lib/clusters', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/lib/clusters')>()),
  useClusters: () => ({ clusters: clustersState.current, connected: true }),
}));
vi.mock('urql', () => ({
  useQuery: (args: { query: unknown }) => {
    queryArgs.push(args);
    const data = settingsState.current ? { permissionSettingsGet: settingsState.current } : undefined;
    return [{ data, error: queryErrorState.current }, reexecuteMock];
  },
  useMutation: (doc: unknown) => {
    const name = /permission[A-Za-z]*/.exec(JSON.stringify(doc))![0];
    return [{}, (vars: unknown) => mutateMock(name, vars)];
  },
}));

const { PermissionSettings } = await import('./permissions');

const allow = {
  id: 'r1',
  line: 'Allow writes in dev-eks / team-a',
  effect: 'Allow',
  accessLevel: 'Write',
  principal: null,
  target: 'Kubernetes',
  scope: 'c1/team-a',
  service: '*',
  operation: '*',
  resource: '*',
};

// A Kubernetes account of cluster id, as the sidecar lists it.
const clusterMode = (
  id: string,
  mode: string,
  source: string,
): { account: string; name: string; clusterID: string | null; mode: string; source: string } => ({
  account: id,
  name: id,
  clusterID: id,
  mode,
  source,
});

function settings(
  over: Record<string, unknown> = {},
  clusterModes = [clusterMode('c1', 'Ask', 'Default'), clusterMode('c2', 'ReadOnly', 'Entry')],
) {
  return {
    targets: [
      { target: 'Kubernetes', defaultMode: 'Ask', accounts: clusterModes },
      {
        target: 'AWS',
        defaultMode: 'ReadOnly',
        accounts: [{ account: '123456789012', name: '123456789012', clusterID: null, mode: 'Auto', source: 'Entry' }],
      },
      { target: 'GitHub', defaultMode: 'Ask', accounts: [] },
    ],
    rules: [allow],
    alwaysAsks: ['Deleting a namespace'],
    accessControl: ['Any change to roles'],
    ...over,
  };
}

// A record of the clusters watch, named name or, with none, by its context.
const cluster = (id: string, context: string, name: string | null = null) => ({
  id,
  spec: { name, source: { kubeconfig: { context } } },
});

const row = (context: string) => screen.getByRole('listitem', { name: context });
const rules = () => screen.getByRole('group', { name: 'Rules' });
const actions = () => within(rules()).getByRole('button', { name: `Actions for ${allow.line}` });

beforeEach(() => {
  vi.clearAllMocks();
  queryArgs.length = 0;
  sandboxState.current = { available: true };
  platformsState.current = ['AWS'];
  settingsState.current = settings();
  queryErrorState.current = undefined;
  clustersState.current = [cluster('c1', 'dev-eks'), cluster('c2', 'prod-eu', 'Production')];
  mutateMock.mockResolvedValue({ data: {} });
});

describe('PermissionSettings', () => {
  it('says why nothing applies on a machine with no sandbox', () => {
    sandboxState.current = { available: false };
    render(<PermissionSettings />);
    expect(
      screen.getByText(
        'This machine has no sandbox, so every command asks before it runs. Modes and rules apply only to sandboxed commands.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('group')).toBeNull();
  });

  it('draws nothing until the sandbox query answers', () => {
    sandboxState.current = { available: undefined };
    const { container } = render(<PermissionSettings />);
    expect(container).toBeEmptyDOMElement();
  });

  // Another window can change the settings, and the clusters watch can learn a
  // context, so what was read when Settings last opened is not trusted.
  it('asks the sidecar again on opening, and when its window regains focus', () => {
    render(<PermissionSettings />);
    const settingsQuery = queryArgs.find((a) => JSON.stringify(a).includes('permissionSettings'));
    expect(settingsQuery).toMatchObject({ requestPolicy: 'cache-and-network' });

    window.dispatchEvent(new Event('focus'));
    expect(reexecuteMock).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  it('sets the default mode, and says what a mode governs', async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);

    expect(
      screen.getByText(
        'Modes and rules decide what sandboxed commands may change. A chat switched outside the sandbox asks for every command.',
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Ask', selected: true })).toBeInTheDocument();
    const card = screen.getByRole('group', { name: 'Default mode' });
    expect(card).toHaveTextContent('Every change to the cluster, and showing Secret data, asks you first.');
    expect(card).not.toHaveTextContent('Every change to the cluster is refused.');
    await user.click(screen.getByRole('tab', { name: 'Auto' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionDefaultModeSet', { target: 'Kubernetes', mode: 'Auto' });
  });

  it("draws each cluster's mode by its name off the clusters watch, and where it comes from", () => {
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'Clusters' })).toHaveTextContent(
      'A cluster follows the default mode unless you set one for it.',
    );
    expect(row('dev-eks')).toHaveTextContent('Default mode');
    expect(row('Production (prod-eu)')).toHaveTextContent('Set for this cluster');
    expect(within(row('Production (prod-eu)')).getByRole('combobox')).toHaveValue('ReadOnly');
  });

  // The default mode and the rules need no cluster's name, so they draw while
  // the clusters watch reconnects.
  it('draws a mode only once the clusters watch delivers its cluster', () => {
    clustersState.current = null;
    const { unmount } = render(<PermissionSettings />);
    expect(screen.getByRole('tab', { name: 'Ask', selected: true })).toBeInTheDocument();
    expect(rules()).toHaveTextContent(allow.line);
    expect(within(screen.getByRole('group', { name: 'Clusters' })).queryAllByRole('listitem')).toHaveLength(0);
    unmount();

    clustersState.current = [cluster('c2', 'prod-eu', 'Production')];
    render(<PermissionSettings />);
    expect(within(screen.getByRole('group', { name: 'Clusters' })).getAllByRole('listitem')).toHaveLength(1);
  });

  // Display names are the user's and need not be unique.
  it('names each cluster by its context too, so two of one name are told apart', async () => {
    const user = userEvent.setup();
    clustersState.current = [cluster('c1', 'prod-a', 'Prod'), cluster('c2', 'prod-b', 'Prod')];
    render(<PermissionSettings />);
    expect(row('Prod (prod-a)')).toHaveTextContent('prod-a · Default mode');
    await user.selectOptions(screen.getByRole('combobox', { name: 'Mode of Prod (prod-b)' }), 'Auto');
    expect(mutateMock).toHaveBeenCalledWith('permissionModeSet', { target: 'Kubernetes', account: 'c2', mode: 'Auto' });
  });

  it('says why the permissions could not be read, and asks again', async () => {
    const user = userEvent.setup();
    settingsState.current = undefined;
    queryErrorState.current = { graphQLErrors: [{ message: 'no such table: permission_rules' }] };
    render(<PermissionSettings />);
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Kubetail could not read the permissions: no such table: permission_rules',
    );
    await user.click(screen.getByRole('button', { name: 'Try again' }));
    expect(reexecuteMock).toHaveBeenCalledWith({ requestPolicy: 'network-only' });
  });

  it('draws nothing while the permissions are read', () => {
    settingsState.current = undefined;
    const { container } = render(<PermissionSettings />);
    expect(container).toBeEmptyDOMElement();
  });

  // An edit that took, whose answer could not be read, is taken: the form
  // closes, and the pane says what happened.
  it('closes the form when an edit took though its answer could not be read', async () => {
    const user = userEvent.setup();
    mutateMock.mockResolvedValue({
      error: {
        graphQLErrors: [
          {
            message: 'The change was saved, but Kubetail could not read the settings back.',
            extensions: { code: 'KUBETAIL_SAVED_UNREAD' },
          },
        ],
      },
    });
    render(<PermissionSettings />);

    await user.click(actions());
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    await user.type(screen.getByLabelText('Verb'), 'patch');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionRuleUpdate', expect.objectContaining({ id: 'r1' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('The change was saved');
    expect(screen.queryByRole('form', { name: 'Edit rule' })).toBeNull();
  });

  it("sets and clears a cluster's mode by its id", async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);

    await user.selectOptions(within(row('dev-eks')).getByRole('combobox'), 'Auto');
    expect(mutateMock).toHaveBeenCalledWith('permissionModeSet', { target: 'Kubernetes', account: 'c1', mode: 'Auto' });
    await user.click(within(row('Production (prod-eu)')).getByRole('button', { name: 'Clear' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionModeClear', { target: 'Kubernetes', account: 'c2' });
  });

  it('offers Clear for a mode of its own alone', () => {
    render(<PermissionSettings />);
    expect(within(row('dev-eks')).queryByRole('button', { name: 'Clear' })).toBeNull();
  });

  it("spells the cluster's name out", () => {
    clustersState.current = [cluster('c1', 'prod\u202Eue')];
    settingsState.current = settings({}, [clusterMode('c1', 'Ask', 'Default')]);
    render(<PermissionSettings />);

    const marks = row('prod\u202Eue').querySelectorAll('mark');
    expect(marks).toHaveLength(1);
    expect(marks[0]).toHaveTextContent('\\u{202E}');
  });

  // Placeholder: no query yet says what applies to one context.
  it('draws the Showing filter held on All clusters, listing each context', () => {
    // A record naming no context, as the watch can hold, lists none, and two
    // clusters of one context, from two kubeconfig files, list it once.
    clustersState.current = [
      ...clustersState.current!,
      { id: 'c3', spec: { name: 'Cloud', source: {} } },
      cluster('c4', 'dev-eks', 'Dev again'),
    ];
    settingsState.current = settings({}, [
      clusterMode('c1', 'Ask', 'Default'),
      clusterMode('c2', 'ReadOnly', 'Entry'),
      clusterMode('c3', 'Ask', 'Default'),
      clusterMode('c4', 'Ask', 'Default'),
    ]);
    render(<PermissionSettings />);
    const showing = screen.getByRole('combobox', { name: 'Showing' });
    expect(showing).toBeDisabled();
    expect(showing).toHaveValue('');
    expect(
      within(showing)
        .getAllByRole('option')
        .map((o) => o.textContent),
    ).toEqual(['All clusters', 'dev-eks', 'prod-eu']);
  });

  it('lists what always asks, and what asks unless a namespace rule allows it, behind the Default mode info', async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);
    await user.click(screen.getByRole('button', { name: 'What modes and rules cannot change' }));
    const popup = await screen.findByRole('dialog');
    expect(within(popup).getByRole('region', { name: 'Always asks, in every mode' })).toHaveTextContent(
      'Deleting a namespace',
    );
    expect(
      within(popup).getByRole('region', { name: 'Asks unless a rule inside a namespace allows it' }),
    ).toHaveTextContent('Any change to roles');
  });

  // A cloud platform's section shows its default mode, each account with its
  // own, and a form adding one by id; one switched off says so.
  it("draws an offered cloud platform's modes, and adds an account by id", async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'GitHub' })).toHaveTextContent(
      'Off: sandboxed commands do not reach GitHub.',
    );
    const aws = screen.getByRole('group', { name: 'AWS' });
    expect(within(aws).getByRole('combobox', { name: 'Default mode of AWS' })).toHaveValue('ReadOnly');
    expect(within(aws).getByRole('combobox', { name: 'Mode of 123456789012' })).toHaveValue('Auto');

    await user.selectOptions(within(aws).getByRole('combobox', { name: 'Default mode of AWS' }), 'Ask');
    expect(mutateMock).toHaveBeenCalledWith('permissionDefaultModeSet', { target: 'AWS', mode: 'Ask' });
    await user.click(within(aws).getByRole('button', { name: 'Clear' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionModeClear', { target: 'AWS', account: '123456789012' });

    const form = within(aws).getByRole('form', { name: 'Add an account to AWS' });
    expect(within(form).getByRole('button', { name: 'Add account' })).toBeDisabled();
    await user.type(within(form).getByLabelText('Account'), ' 210987654321 ');
    await user.selectOptions(within(form).getByLabelText('Mode'), 'Auto');
    await user.click(within(form).getByRole('button', { name: 'Add account' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionModeSet', {
      target: 'AWS',
      account: '210987654321',
      mode: 'Auto',
    });
    expect(within(form).getByLabelText('Account')).toHaveValue('');
  });

  it("sets an account's own mode, and keeps an account the sidecar refused", async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);
    const aws = screen.getByRole('group', { name: 'AWS' });
    await user.selectOptions(within(aws).getByRole('combobox', { name: 'Mode of 123456789012' }), 'ReadOnly');
    expect(mutateMock).toHaveBeenCalledWith('permissionModeSet', {
      target: 'AWS',
      account: '123456789012',
      mode: 'ReadOnly',
    });

    mutateMock.mockResolvedValue({ error: { graphQLErrors: [{ message: 'an account is one name' }] } });
    const form = within(aws).getByRole('form', { name: 'Add an account to AWS' });
    await user.type(within(form).getByLabelText('Account'), 'a/b');
    await user.click(within(form).getByRole('button', { name: 'Add account' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('an account is one name');
    expect(within(form).getByLabelText('Account')).toHaveValue('a/b');
  });

  // The sidecar always lists Kubernetes, and each of its accounts names a
  // cluster; an answer that does not still draws, with no cluster row.
  it('draws no cluster for an answer naming no Kubernetes account', () => {
    settingsState.current = settings({}, [
      { account: 'x', name: 'x', clusterID: null, mode: 'Ask', source: 'Default' },
    ]);
    const { unmount } = render(<PermissionSettings />);
    expect(screen.queryByRole('listitem', { name: 'x' })).toBeNull();
    unmount();
    const answer = settings();
    settingsState.current = { ...answer, targets: answer.targets.filter((t) => t.target !== 'Kubernetes') };
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'AWS' })).toBeInTheDocument();
  });

  // A platform switched off can be set up before a run reaches it, and a rule
  // for it opens with its target.
  it('shows a cloud platform switched off, and says so', async () => {
    const user = userEvent.setup();
    platformsState.current = [];
    settingsState.current = settings({
      rules: [{ ...allow, line: 'Allow writes in AWS 123456789012', target: 'AWS', scope: '123456789012/**' }],
    });
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'AWS' })).toHaveTextContent('Off: sandboxed commands do not reach AWS.');
    await user.click(within(rules()).getByRole('button', { name: 'Actions for Allow writes in AWS 123456789012' }));
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    expect(screen.getByLabelText('Reaches')).toHaveValue('AWS');
  });

  it("says nothing of a platform's switch it could not read", () => {
    platformsState.current = undefined;
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'AWS' })).not.toHaveTextContent('Off:');
  });

  it("names an offered cloud platform's accounts in the rule form", async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);
    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    await user.selectOptions(screen.getByLabelText('Reaches'), 'AWS');
    await user.click(screen.getByRole('button', { name: 'Add' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionRuleAdd', {
      input: expect.objectContaining({ target: 'AWS', scope: '123456789012/**' }),
    });
  });

  it('draws the chat grants and what ran without asking as coming soon', () => {
    render(<PermissionSettings />);
    expect(screen.getByRole('group', { name: 'Chat grants' })).toHaveTextContent('Coming soon');
    expect(screen.getByRole('group', { name: 'Ran without asking' })).toHaveTextContent('Coming soon');
  });

  it('draws the rules, and removes one from its menu', async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);

    expect(rules()).toHaveTextContent('Allow writes in dev-eks / team-a');
    await user.click(actions());
    await user.click(await screen.findByRole('menuitem', { name: 'Remove' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionRuleRemove', { id: 'r1' });
  });

  it('adds a rule on a cluster it names as the Clusters card does', async () => {
    const user = userEvent.setup();
    render(<PermissionSettings />);

    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    await user.selectOptions(screen.getByLabelText('Cluster'), 'Production (prod-eu)');
    await user.type(screen.getByLabelText('Resource'), 'deployments');
    await user.click(screen.getByRole('button', { name: 'Add' }));
    expect(mutateMock).toHaveBeenCalledWith('permissionRuleAdd', {
      input: expect.objectContaining({
        effect: 'Allow',
        accessLevel: 'Write',
        target: 'Kubernetes',
        scope: 'c2/**',
        resource: 'deployments',
      }),
    });
  });

  // urql's cache gives each rule a __typename, which the rule input refuses.
  it('edits a rule by its id, in one mutation', async () => {
    const user = userEvent.setup();
    settingsState.current = settings({ rules: [{ __typename: 'PermissionRule', ...allow }] });
    render(<PermissionSettings />);

    await user.click(actions());
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    await user.type(screen.getByLabelText('Verb'), 'patch');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(mutateMock.mock.calls).toEqual([
      [
        'permissionRuleUpdate',
        {
          id: 'r1',
          input: {
            effect: 'Allow',
            accessLevel: 'Write',
            principal: null,
            target: 'Kubernetes',
            scope: 'c1/team-a',
            service: '*',
            operation: 'patch',
            resource: '*',
          },
        },
      ],
    ]);
  });

  it('keeps the rule when an edit is refused, and says why', async () => {
    const user = userEvent.setup();
    mutateMock.mockResolvedValue({ error: { graphQLErrors: [{ message: 'rules: a destructive rule cannot allow' }] } });
    render(<PermissionSettings />);

    await user.click(actions());
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }));
    await user.type(screen.getByLabelText('Verb'), 'patch');
    await user.click(screen.getByRole('button', { name: 'Save' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('rules: a destructive rule cannot allow');
    expect(screen.getByRole('form', { name: 'Edit rule' })).toBeInTheDocument();
  });

  it('says why a mutation was refused', async () => {
    const user = userEvent.setup();
    mutateMock.mockResolvedValue({
      error: { graphQLErrors: [{ message: 'rules: names a class its provider does not have' }] },
    });
    render(<PermissionSettings />);

    await user.click(screen.getByRole('button', { name: 'Add rule' }));
    await user.click(screen.getByRole('button', { name: 'Add' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('rules: names a class its provider does not have');
  });
});
