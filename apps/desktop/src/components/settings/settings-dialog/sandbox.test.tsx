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

import type { Sandbox } from '@/lib/sandbox';
import type { SandboxExecutable, SandboxExecutables } from '@/lib/sandbox-executables';
import type { SandboxPath, SandboxPathFolder } from '@/lib/sandbox-path';
import { expectCodeText } from '@/test-utils';

const { sandbox, path, executables, everyCluster, setDefaultMock } = vi.hoisted(() => ({
  sandbox: { current: {} as Sandbox },
  path: { current: {} as SandboxPath },
  executables: { current: {} as SandboxExecutables },
  everyCluster: {
    current: { sandboxDisabled: false, clusterOverride: false },
    switching: false,
  },
  setDefaultMock: vi.fn(),
}));
vi.mock('@/lib/sandbox', () => ({ useSandbox: () => sandbox.current }));
vi.mock('@/lib/sandbox-default', () => ({
  useSandboxDefault: (clusterID: string | null) => (clusterID === null ? everyCluster.current : undefined),
  useSandboxDefaultSwitch: (clusterID: string | null) => ({
    switching: everyCluster.switching,
    setSandboxDisabled: (disabled: boolean) => setDefaultMock(clusterID, disabled),
    reset: vi.fn(),
  }),
}));
vi.mock('@/lib/sandbox-path', () => ({ useSandboxPath: () => path.current }));
vi.mock('@/lib/sandbox-executables', () => ({ useSandboxExecutables: () => executables.current }));
// Each section's own behavior is its test's; here a stub says it is drawn.
vi.mock('./sandbox-folders', () => ({
  SandboxFolderGroups: ({ path: drawn }: { path: SandboxPath }) => (
    <p>Folders of {drawn.folders?.length} PATH folders</p>
  ),
}));
vi.mock('./sandbox-network', () => ({ SandboxNetwork: () => <p>Network</p> }));
vi.mock('./sandbox-new-chats', () => ({ SandboxNewChats: () => <p>New chats</p> }));
vi.mock('./sandbox-environment', () => ({ SandboxEnvironment: () => <p>Environment</p> }));
vi.mock('./sandbox-console', () => ({
  SandboxConsole: ({ open }: { open: boolean }) => (open ? <p>The console</p> : null),
}));

const { SandboxSettings } = await import('./sandbox');

const pathFolder = (dir: string): SandboxPathFolder => ({
  path: dir,
  origin: 'Shell',
  target: dir,
  searched: true,
  reason: null,
  addable: false,
  shared: false,
});

function withPath(over: Partial<SandboxPath>) {
  path.current = {
    value: '$PATH',
    refusal: null,
    folders: [],
    fault: null,
    saving: false,
    refreshing: false,
    saveError: null,
    refreshError: null,
    save: vi.fn(async () => true),
    refresh: vi.fn(async () => true),
    ...over,
  };
}

function withExecutables(over: Partial<SandboxExecutables>) {
  executables.current = {
    report: [],
    probing: false,
    changing: false,
    probeError: null,
    registerError: null,
    removeError: null,
    probe: vi.fn(async () => {}),
    register: vi.fn(async () => true),
    remove: vi.fn(async () => {}),
    ...over,
  };
}

const probed = (name: string, extra: Partial<SandboxExecutable> = {}): SandboxExecutable => ({
  name,
  invocation: `${name} --version`,
  registered: false,
  probed: true,
  resolved: `/opt/homebrew/bin/${name}`,
  shim: false,
  target: '',
  ok: true,
  version: `${name} v1`,
  error: '',
  refusal: null,
  ...extra,
});

// refused is a registered executable the shape check refuses, never probed.
const refused = (name: string, invocation: string, refusal: string): SandboxExecutable =>
  probed(name, { invocation, registered: true, probed: false, resolved: '', ok: false, version: '', refusal });

beforeEach(() => {
  sandbox.current = {
    available: true,
    reason: '',
    networkAvailable: true,
    networkReason: '',
    failed: false,
    retry: vi.fn(),
  };
  withPath({});
  withExecutables({});
  everyCluster.current = { sandboxDisabled: false, clusterOverride: false };
  everyCluster.switching = false;
  setDefaultMock.mockClear();
});

describe('SandboxSettings', () => {
  it('draws the status, then New chats, Folders, Network, PATH, Executables and Environment', () => {
    withPath({ folders: [pathFolder('/opt/homebrew/bin')] });
    render(<SandboxSettings />);
    expect(screen.getByText('Sandbox is on for new chats')).toBeInTheDocument();
    // Folders and PATH share one PATH reader, so a refresh holds both.
    expect(screen.getByText('Folders of 1 PATH folders')).toBeInTheDocument();
    expect(
      screen.getByText('New chats').compareDocumentPosition(screen.getByText('Folders of 1 PATH folders')) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    const order = ['Network', 'PATH', 'Executables', 'Environment'].map((name) =>
      name === 'PATH' || name === 'Executables' ? screen.getByRole('group', { name }) : screen.getByText(name),
    );
    order.slice(1).forEach((el, i) => {
      expect(order[i].compareDocumentPosition(el) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    });
  });

  // The card's switch is every cluster's new-chat default.
  it("draws every cluster's default and writes it, behind the confirm going outside", async () => {
    const user = userEvent.setup();
    const view = render(<SandboxSettings />);
    expect(screen.getByRole('switch', { name: 'Sandbox for new chats' })).toBeChecked();
    await user.click(screen.getByRole('switch', { name: 'Sandbox for new chats' }));
    await user.click(
      within(await screen.findByRole('dialog')).getByRole('button', { name: 'Start outside the sandbox' }),
    );
    expect(setDefaultMock).toHaveBeenCalledWith(null, true);

    everyCluster.current = { sandboxDisabled: true, clusterOverride: false };
    view.rerender(<SandboxSettings />);
    await user.click(screen.getByRole('switch', { name: 'Sandbox for new chats' }));
    expect(setDefaultMock).toHaveBeenLastCalledWith(null, false);
  });

  it('holds the switch while a change of the default is in flight', () => {
    everyCluster.switching = true;
    render(<SandboxSettings />);
    expect(screen.getByRole('switch', { name: 'Sandbox for new chats' })).toHaveAttribute('aria-disabled', 'true');
  });

  it('draws the no-sandbox card alone on a machine with none', () => {
    sandbox.current = { ...sandbox.current, available: false, reason: 'bwrap not found' };
    render(<SandboxSettings />);
    expect(screen.getByText('No sandbox on this computer')).toBeInTheDocument();
    expect(screen.queryByRole('group')).toBeNull();
    expect(screen.queryByText('Network')).toBeNull();
  });

  it('draws nothing before the sidecar answers, and a retry after a failure', () => {
    sandbox.current = { ...sandbox.current, available: undefined };
    const { container, unmount } = render(<SandboxSettings />);
    expect(container).toBeEmptyDOMElement();
    unmount();

    sandbox.current = { ...sandbox.current, failed: true };
    render(<SandboxSettings />);
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    expect(screen.queryByRole('group')).toBeNull();
  });

  it('opens the console from Try a command', async () => {
    render(<SandboxSettings />);
    expect(screen.queryByText('The console')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Try a command' }));
    expect(screen.getByText('The console')).toBeInTheDocument();
  });

  it("turns off the webview's suggestions in the executable's fields", () => {
    render(<SandboxSettings />);
    expectCodeText(screen.getByRole('textbox', { name: 'Executable name' }));
    expectCodeText(screen.getByRole('textbox', { name: 'Invocation' }));
  });
});

describe('SandboxSettings: Executables', () => {
  const executableRows = () => within(screen.getByRole('list', { name: 'Executables' })).getAllByRole('listitem');

  it('draws each executable: its path or why it has none, a shim, its version, its tag and its error', () => {
    withExecutables({
      report: [
        probed('kubectl', { shim: true, target: '/Users/ren/.asdf/installs/kubectl/1.31/bin/kubectl' }),
        probed('helm', { ok: false, error: 'Exit code 1', version: 'Error: plugin failed' }),
        probed('kustomize', { resolved: '', ok: false, version: '', error: "not found on the sandbox's PATH" }),
        probed('git', { probed: false, resolved: '', ok: false, version: '', error: 'not probed yet' }),
        probed('jq', {
          probed: false,
          resolved: '',
          ok: false,
          version: '',
          error: 'could not start: the disk did not answer in time',
        }),
      ],
    });
    render(<SandboxSettings />);
    const [kubectl, helm, kustomize, git, jq] = executableRows();

    expect(within(kubectl).getByTestId('resolved').textContent).toBe('/opt/homebrew/bin/kubectl');
    expect(within(kubectl).getByText('shim →')).toBeInTheDocument();
    expect(within(kubectl).getByTestId('target').textContent).toBe(
      '/Users/ren/.asdf/installs/kubectl/1.31/bin/kubectl',
    );
    expect(within(kubectl).getByText('kubectl v1')).toBeInTheDocument();
    expect(within(kubectl).getByText('ok')).toBeInTheDocument();

    expect(within(helm).getByText('failed')).toBeInTheDocument();
    expect(within(helm).getByText('Exit code 1')).toBeInTheDocument();
    expect(within(helm).getByText('Error: plugin failed')).toBeInTheDocument();

    expect(within(kustomize).getByText("not found on the sandbox's PATH")).toBeInTheDocument();
    expect(within(kustomize).queryByText('failed')).toBeNull();

    expect(within(git).getByText('not probed yet')).toBeInTheDocument();
    expect(within(git).queryByText('failed')).toBeNull();

    expect(within(jq).getByText('could not start: the disk did not answer in time')).toBeInTheDocument();
    expect(within(jq).queryByText('failed')).toBeNull();
    expect(
      screen.getByText(
        'Kubetail runs each executable once in the sandbox, with no cluster and no network, to see what it needs.',
      ),
    ).toBeInTheDocument();
  });

  it('says plainly when kubectl was not found, and only then', () => {
    const line = "kubectl was not found on the sandbox's PATH. Install it, or include its folder above.";
    withExecutables({
      report: [probed('kubectl', { resolved: '', ok: false, error: "not found on the sandbox's PATH" })],
    });
    const { unmount } = render(<SandboxSettings />);
    expect(screen.getByText(line)).toBeInTheDocument();

    unmount();
    withExecutables({ report: [probed('kubectl')] });
    const found = render(<SandboxSettings />);
    expect(screen.queryByText(line)).toBeNull();

    found.unmount();
    withExecutables({ report: [probed('kubectl', { probed: false, resolved: '', error: 'not probed yet' })] });
    const unprobed = render(<SandboxSettings />);
    expect(screen.queryByText(line)).toBeNull();

    // A probe that could not start says nothing about whether kubectl is there.
    unprobed.unmount();
    withExecutables({
      report: [
        probed('kubectl', { probed: false, resolved: '', error: 'could not start: the disk did not answer in time' }),
      ],
    });
    render(<SandboxSettings />);
    expect(screen.queryByText(line)).toBeNull();
  });

  it('probes again, holding the button while it runs, and draws a refused probe', async () => {
    withExecutables({ report: [probed('kubectl')] });
    const { unmount } = render(<SandboxSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Probe again' }));
    expect(executables.current.probe).toHaveBeenCalled();

    unmount();
    withExecutables({ report: [probed('kubectl')], probing: true, probeError: 'This machine has no sandbox.' });
    render(<SandboxSettings />);
    expect(screen.getByRole('button', { name: /Probing…/ })).toBeDisabled();
    expect(screen.getByText('This machine has no sandbox.')).toBeInTheDocument();
  });

  it('probes once Import from shell answers, and not when it is refused', async () => {
    const { unmount } = render(<SandboxSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Import from shell' }));
    expect(executables.current.probe).toHaveBeenCalledTimes(1);

    unmount();
    withPath({ refresh: vi.fn(async () => false) });
    withExecutables({});
    render(<SandboxSettings />);
    await userEvent.click(screen.getByRole('button', { name: 'Import from shell' }));
    expect(executables.current.probe).not.toHaveBeenCalled();
  });

  it('lists the registered executables with Remove, and adds one', async () => {
    withExecutables({ report: [probed('kubectl'), probed('k9s', { registered: true })] });
    render(<SandboxSettings />);
    const registered = within(screen.getByRole('list', { name: 'Registered executables' })).getAllByRole('listitem');
    expect(registered).toHaveLength(1);
    await userEvent.click(within(registered[0]).getByRole('button', { name: 'Remove' }));
    expect(executables.current.remove).toHaveBeenCalledWith('k9s');

    const name = screen.getByRole('textbox', { name: 'Executable name' });
    const invocation = screen.getByRole('textbox', { name: 'Invocation' });
    expect(invocation).toHaveAttribute('placeholder', '<name> --version');
    await userEvent.type(name, 'stern');
    await userEvent.click(screen.getByRole('button', { name: 'Add executable' }));
    expect(executables.current.register).toHaveBeenCalledWith('stern', '');
    expect(name).toHaveValue('');
  });

  it('registers an executable with the invocation typed', async () => {
    withExecutables({});
    render(<SandboxSettings />);
    const invocation = screen.getByRole('textbox', { name: 'Invocation' });
    await userEvent.type(screen.getByRole('textbox', { name: 'Executable name' }), 'stern');
    await userEvent.type(invocation, 'stern version');
    await userEvent.click(screen.getByRole('button', { name: 'Add executable' }));
    expect(executables.current.register).toHaveBeenCalledWith('stern', 'stern version');
    expect(invocation).toHaveValue('');
  });

  it('keeps the form and draws why a register was refused', async () => {
    withExecutables({ register: vi.fn(async () => false), registerError: 'Kubetail probes kubectl already.' });
    render(<SandboxSettings />);
    await userEvent.type(screen.getByRole('textbox', { name: 'Executable name' }), 'kubectl');
    await userEvent.click(screen.getByRole('button', { name: 'Add executable' }));
    expect(screen.getByRole('textbox', { name: 'Executable name' })).toHaveValue('kubectl');
    expect(screen.getByText('Kubetail probes kubectl already.')).toBeInTheDocument();
  });

  it("draws a refused executable's reason in place of a probe result, and removes it", async () => {
    const reason = "An invocation starts with the executable's name.";
    withExecutables({ report: [probed('kubectl'), refused('stern', 'other --version', reason)] });
    render(<SandboxSettings />);
    const stern = executableRows()[1];
    expect(within(stern).getByText(reason)).toBeInTheDocument();
    expect(within(stern).queryByText('failed')).not.toBeInTheDocument();

    const registered = within(screen.getByRole('list', { name: 'Registered executables' })).getAllByRole('listitem');
    await userEvent.click(within(registered[0]).getByRole('button', { name: 'Remove' }));
    expect(executables.current.remove).toHaveBeenCalledWith('stern');
  });

  it('draws a registered executable beside the curated one of its name, and removes the registered one', async () => {
    const duplicateKey = vi.spyOn(console, 'error').mockImplementation(() => {});
    withExecutables({
      report: [probed('kubectl'), refused('kubectl', 'kubectl version', 'Kubetail probes kubectl already.')],
    });
    render(<SandboxSettings />);
    const [curated, registeredKubectl] = executableRows();
    expect(within(curated).getByText('ok')).toBeInTheDocument();
    expect(within(registeredKubectl).getByText('Kubetail probes kubectl already.')).toBeInTheDocument();

    const registered = within(screen.getByRole('list', { name: 'Registered executables' })).getAllByRole('listitem');
    expect(registered).toHaveLength(1);
    await userEvent.click(within(registered[0]).getByRole('button', { name: 'Remove' }));
    expect(executables.current.remove).toHaveBeenCalledWith('kubectl');
    expect(duplicateKey).not.toHaveBeenCalled();
    duplicateKey.mockRestore();
  });

  it('holds Add and Remove while a change is in flight', () => {
    withExecutables({ report: [probed('k9s', { registered: true })], changing: true });
    render(<SandboxSettings />);
    expect(screen.getByRole('button', { name: 'Remove' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Add executable' })).toBeDisabled();
  });
});
