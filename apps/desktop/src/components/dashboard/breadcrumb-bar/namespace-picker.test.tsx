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

import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { useClusterNamespacesMock, useNamespaceScopeMock, setNamespace } = vi.hoisted(() => ({
  useClusterNamespacesMock: vi.fn(),
  useNamespaceScopeMock: vi.fn(),
  setNamespace: vi.fn(),
}));
vi.mock('@/lib/cluster-namespaces', () => ({ useClusterNamespaces: useClusterNamespacesMock }));
vi.mock('@/lib/namespace-scope', () => ({ useNamespaceScope: useNamespaceScopeMock }));

const { NamespacePicker } = await import('./namespace-picker');

// Helpers -------------------------------------------------------------

function setup({
  namespace,
  namespaces = ['checkout', 'default', 'kube-system'],
  active = true,
  phase = 'live',
}: { namespace?: string; namespaces?: string[]; active?: boolean; phase?: string } = {}) {
  useNamespaceScopeMock.mockReturnValue({ namespace, setNamespace });
  useClusterNamespacesMock.mockReturnValue({ namespaces, active, phase });
}

async function openMenu() {
  const user = userEvent.setup();
  render(<NamespacePicker />);
  await user.click(screen.getByRole('button', { name: /^Namespace:/ }));
  await waitFor(() => expect(screen.getByRole('combobox', { name: 'Filter namespaces' })).toHaveFocus());
  return user;
}

const options = () => screen.getAllByRole('option').map((o) => o.textContent);

beforeEach(() => {
  vi.clearAllMocks();
  Element.prototype.scrollIntoView = vi.fn();
});

// Tests ---------------------------------------------------------------

describe('NamespacePicker', () => {
  it('reads All namespaces with no namespace chosen', () => {
    setup();
    render(<NamespacePicker />);
    expect(screen.getByRole('button', { name: 'Namespace: All namespaces' })).toHaveTextContent('All namespaces');
  });

  it('names the chosen namespace through VisibleText, in its text and its name', () => {
    setup({ namespace: 'a\u200bb' });
    render(<NamespacePicker />);
    expect(screen.getByRole('button', { name: 'Namespace: a\\u{200B}b' })).toHaveTextContent('a\\u{200B}b');
  });

  it('lists All namespaces first, then the cluster’s namespaces', async () => {
    setup({ namespace: 'default' });
    await openMenu();
    expect(options()).toEqual(['All namespaces', 'checkout', 'default', 'kube-system']);
    expect(screen.getByRole('option', { name: 'default' })).toHaveAttribute('data-current');
  });

  it('filters by a substring of the name, keeping All namespaces', async () => {
    setup();
    const user = await openMenu();
    await user.keyboard('KUBE');
    expect(options()).toEqual(['All namespaces', 'kube-system']);
  });

  // Typing clears the highlight; Enter must not fall to the broadest option.
  it('picks the first matching namespace on Enter after a search', async () => {
    setup({ namespace: 'default' });
    const user = await openMenu();
    await user.keyboard('kube-system{Enter}');
    expect(setNamespace).toHaveBeenLastCalledWith('kube-system');
  });

  it('keeps the scope on Enter when the search matches no namespace', async () => {
    setup({ namespace: 'default' });
    const user = await openMenu();
    await user.keyboard('kube-sytsem{Enter}');
    expect(setNamespace).not.toHaveBeenCalled();
    // All namespaces is still there to pick.
    await user.keyboard('{ArrowDown}{Enter}');
    expect(setNamespace).toHaveBeenLastCalledWith(undefined);
  });

  it('keeps the scope on Enter while the namespaces load', async () => {
    setup({ namespace: 'default', namespaces: [], phase: 'connecting' });
    const user = await openMenu();
    await user.keyboard('kube{Enter}');
    expect(setNamespace).not.toHaveBeenCalled();
  });

  // The route takes any nonempty namespace, `*` included.
  it('tells a namespace named * from All namespaces', async () => {
    setup({ namespace: '*' });
    const user = await openMenu();
    expect(options()).toEqual(['All namespaces', '*Not found', 'checkout', 'default', 'kube-system']);
    const ids = screen.getAllByRole('option').map((o) => o.id);
    expect(new Set(ids).size).toBe(ids.length);
    await user.click(screen.getByRole('option', { name: /^\*/ }));
    expect(setNamespace).toHaveBeenLastCalledWith('*');
  });

  it('picks All namespaces on Enter once the search is cleared', async () => {
    setup({ namespace: 'default' });
    const user = await openMenu();
    await user.keyboard('kube{Backspace}{Backspace}{Backspace}{Backspace}{Enter}');
    expect(setNamespace).toHaveBeenLastCalledWith(undefined);
  });

  it('keeps a chosen namespace the cluster does not list, marked Not found', async () => {
    setup({ namespace: 'gone' });
    await openMenu();
    expect(options()).toEqual(['All namespaces', 'goneNot found', 'checkout', 'default', 'kube-system']);
  });

  it('says it is loading before the namespaces land', async () => {
    setup({ namespace: 'checkout', namespaces: [], phase: 'connecting' });
    await openMenu();
    expect(screen.getByText('Loading namespaces…')).toBeInTheDocument();
    // Not *Not found*: the list has not landed.
    expect(options()).toEqual(['All namespaces', 'checkout']);
  });

  // The loading label and a namespace of the same name are siblings in one list.
  it('tells the loading label from a namespace named loading', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    setup({ namespace: 'loading', namespaces: [], phase: 'connecting' });
    await openMenu();
    expect(options()).toEqual(['All namespaces', 'loading']);
    expect(consoleError.mock.calls.some(([message]) => String(message).includes('same key'))).toBe(false);
    consoleError.mockRestore();
  });

  it('holds All namespaces and the chosen one with no active cache', async () => {
    setup({ namespace: 'checkout', namespaces: [], active: false, phase: 'connecting' });
    await openMenu();
    expect(screen.queryByText('Loading namespaces…')).not.toBeInTheDocument();
    expect(options()).toEqual(['All namespaces', 'checkout']);
  });

  it('writes the pick, and All namespaces as no namespace', async () => {
    setup({ namespace: 'default' });
    const user = await openMenu();
    await user.click(screen.getByRole('option', { name: 'checkout' }));
    expect(setNamespace).toHaveBeenLastCalledWith('checkout');

    await user.click(screen.getByRole('button', { name: /^Namespace:/ }));
    await user.click(await screen.findByRole('option', { name: 'All namespaces' }));
    expect(setNamespace).toHaveBeenLastCalledWith(undefined);
  });
});
