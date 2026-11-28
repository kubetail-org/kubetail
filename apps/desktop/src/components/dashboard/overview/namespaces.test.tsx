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

import { screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Namespaces } from './namespaces';
import { bodyRows, renderOnDashboard } from './testutil';

const rows = () => bodyRows('Namespaces');
const row = (name: string) => ({ name, workloads: 2, pods: 5 });

describe('Namespaces', () => {
  it('draws each namespace with its workloads and pods', async () => {
    await renderOnDashboard(<Namespaces rows={[row('shop'), row('ops')]} />);
    expect(screen.getByRole('region', { name: 'Namespaces' })).toHaveTextContent(/^Namespaces2/);
    expect(rows().map((r) => r.textContent)).toEqual(['shop25', 'ops25']);
    expect(screen.queryByRole('link')).not.toBeInTheDocument();
  });

  it('links to the Namespaces page when it has more than it shows', async () => {
    await renderOnDashboard(<Namespaces rows={['a', 'b', 'c', 'd', 'e'].map(row)} />);
    expect(rows()).toHaveLength(3);
    expect(screen.getByRole('link', { name: 'Show all 5' })).toHaveAttribute('href', '/dashboard?resource=namespaces');
  });

  it('says when the scope holds none', async () => {
    await renderOnDashboard(<Namespaces rows={[]} />);
    expect(rows().map((r) => r.textContent)).toEqual(['No namespaces']);
  });

  it('waits behind a spinner', async () => {
    await renderOnDashboard(<Namespaces />);
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });
});
