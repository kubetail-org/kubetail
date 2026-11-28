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
import { describe, expect, it } from 'vitest';

import { ObjectCards } from './registry';
import { objectWith } from './testutil';

const EVENTS = { events: [], phase: 'live' as const };

const titles = () => screen.getAllByRole('heading', { level: 3 }).map((h) => h.textContent);
const CONDITIONS = { status: { conditions: [] } };

describe('ObjectCards', () => {
  it('gives a pod its own cards, in order', () => {
    render(
      <ObjectCards
        gvr={{ apiVersion: 'v1', resource: 'pods' }}
        object={objectWith(CONDITIONS)}
        namespaced
        events={EVENTS}
      />,
    );
    expect(titles()).toEqual(['Details', 'Containers', 'Resources', 'Conditions', 'Events', 'Labels']);
  });

  it('gives a node its own cards, in order', () => {
    render(
      <ObjectCards
        gvr={{ apiVersion: 'v1', resource: 'nodes' }}
        object={objectWith(CONDITIONS, { kind: 'Node', namespace: '' })}
        namespaced={false}
        events={EVENTS}
      />,
    );
    expect(titles()).toEqual(['Details', 'Capacity', 'Conditions', 'Events', 'Labels']);
  });

  it('gives any other kind the generic cards, Conditions only where it keeps them', () => {
    const gvr = { apiVersion: 'example.com/v1', resource: 'pods' };
    const { rerender } = render(<ObjectCards gvr={gvr} object={objectWith({})} namespaced events={EVENTS} />);
    expect(titles()).toEqual(['Details', 'Events', 'Labels']);
    rerender(<ObjectCards gvr={gvr} object={objectWith(CONDITIONS)} namespaced events={EVENTS} />);
    expect(titles()).toEqual(['Details', 'Conditions', 'Events', 'Labels']);
  });
});
