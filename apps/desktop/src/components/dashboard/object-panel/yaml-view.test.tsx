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

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const { useObjectsMock } = vi.hoisted(() => ({ useObjectsMock: vi.fn() }));
vi.mock('@/lib/cluster-cached-data-objects', () => ({ useClusterCachedDataObjects: useObjectsMock }));

const { ObjectYamlView } = await import('./yaml-view');

const POD = { apiVersion: 'v1', resource: 'pods', kind: 'Pod', uid: 'u1', namespace: 'shop', name: 'web-1' };

const row = (phase: string) => ({
  uid: 'u1',
  namespace: 'shop',
  name: 'web-1',
  rawJSON: { apiVersion: 'v1', kind: 'Pod', metadata: { name: 'web-1' }, status: { phase } },
});

const watch = (objects: unknown[], phase = 'live', active = true) =>
  useObjectsMock.mockReturnValue({ objects, active, phase });

const pre = () => document.querySelector('pre');

beforeEach(() => {
  Object.assign(navigator, { clipboard: { writeText: vi.fn().mockResolvedValue(undefined) } });
});

describe('ObjectYamlView', () => {
  it('watches the object’s kind on a connection of its own', () => {
    watch([]);
    render(<ObjectYamlView object={POD} />);
    expect(useObjectsMock).toHaveBeenCalledWith(POD, { unshared: true });
  });

  it('draws the body as YAML, highlighted once the grammar arrives', async () => {
    watch([row('Running')]);
    render(<ObjectYamlView object={POD} />);
    expect(screen.getByText(/managed fields left out/)).toBeInTheDocument();
    expect(pre()).toHaveTextContent('phase: Running');
    await waitFor(() => expect(pre()?.querySelector('.hljs-attr')).toHaveTextContent('apiVersion'));
  });

  it('spells an invisible character in the body', () => {
    watch([{ ...row('Running'), rawJSON: { metadata: { name: 'a​b' } } }]);
    render(<ObjectYamlView object={POD} />);
    expect(pre()?.querySelector('mark')).toHaveAttribute('title', 'An invisible character');
  });

  it('follows the object as it changes', () => {
    watch([row('Pending')]);
    const { rerender } = render(<ObjectYamlView object={POD} />);
    watch([row('Running')]);
    rerender(<ObjectYamlView object={POD} />);
    expect(pre()).toHaveTextContent('phase: Running');
  });

  it('keeps the last version through a reconnect, and marks it once the object is gone', () => {
    watch([row('Running')]);
    const { rerender } = render(<ObjectYamlView object={POD} />);
    watch([], 'reconnecting');
    rerender(<ObjectYamlView object={POD} />);
    expect(pre()).toHaveTextContent('phase: Running');
    expect(screen.queryByText(/Deleted/)).not.toBeInTheDocument();

    watch([]);
    rerender(<ObjectYamlView object={POD} />);
    expect(pre()).toHaveTextContent('phase: Running');
    expect(screen.getByText('Deleted · its last version')).toBeInTheDocument();
  });

  it('waits for the snapshot, then says so when the object is not in it', () => {
    watch([], 'connecting');
    const { rerender } = render(<ObjectYamlView object={POD} />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
    watch([]);
    rerender(<ObjectYamlView object={POD} />);
    expect(screen.getByText('This object is no longer in the cluster.')).toBeInTheDocument();
  });

  it('says so without an active cache', () => {
    watch([], 'connecting', false);
    render(<ObjectYamlView object={POD} />);
    expect(screen.getByText('No synced cache for this cluster.')).toBeInTheDocument();
  });

  it('copies the YAML', () => {
    watch([row('Running')]);
    render(<ObjectYamlView object={POD} />);
    fireEvent.click(screen.getByRole('button', { name: 'Copy YAML' }));
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
      'apiVersion: v1\nkind: Pod\nmetadata:\n  name: web-1\nstatus:\n  phase: Running\n',
    );
  });

  it('offers no copy before a body has arrived', () => {
    watch([], 'connecting');
    render(<ObjectYamlView object={POD} />);
    expect(screen.queryByRole('button', { name: 'Copy YAML' })).not.toBeInTheDocument();
  });
});
