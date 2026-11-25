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

import { createRootRoute, createRoute } from '@tanstack/react-router';
import { act, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { renderWithRouter } from '@/test-utils';

import { useNamespaceScope } from './namespace-scope';

// Helpers -------------------------------------------------------------

function Probe() {
  const { namespace, setNamespace } = useNamespaceScope();
  return (
    <div>
      <div data-testid="namespace">{namespace ?? '(all)'}</div>
      <button type="button" onClick={() => setNamespace('checkout')}>
        pick-checkout
      </button>
      <button type="button" onClick={() => setNamespace(undefined)}>
        pick-all
      </button>
    </div>
  );
}

function buildTree() {
  const root = createRootRoute();
  const page = createRoute({
    getParentRoute: () => root,
    path: '/dashboard',
    validateSearch: (s: Record<string, unknown>) => s as { namespace?: string; resource?: string },
    component: Probe,
  });
  return root.addChildren([page]);
}

// Tests ---------------------------------------------------------------

describe('useNamespaceScope', () => {
  it('reads the namespace param, absent as every namespace', async () => {
    await renderWithRouter(buildTree(), '/dashboard');
    expect(screen.getByTestId('namespace')).toHaveTextContent('(all)');
  });

  it('writes the param and leaves the rest of the search alone', async () => {
    const { router } = await renderWithRouter(buildTree(), '/dashboard?resource=pods');

    await act(async () => screen.getByText('pick-checkout').click());
    expect(router.state.location.search).toEqual({ resource: 'pods', namespace: 'checkout' });
    expect(screen.getByTestId('namespace')).toHaveTextContent('checkout');

    await act(async () => screen.getByText('pick-all').click());
    expect(router.state.location.search).toEqual({ resource: 'pods' });
  });
});
