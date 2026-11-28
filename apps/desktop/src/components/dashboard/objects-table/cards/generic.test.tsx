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
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ConditionsCard, DetailsCard, LabelsCard } from './generic';
import { objectWith } from './testutil';

const NOW = Date.parse('2026-11-22T11:14:00Z');

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(NOW);
});
afterEach(() => vi.useRealTimers());

const card = (title: string) => screen.getByRole('heading', { level: 3, name: title }).closest('section')!;
const value = (label: string) => within(card('Details')).getByText(label).nextElementSibling as HTMLElement;

describe('DetailsCard', () => {
  const owned = { metadata: { ownerReferences: [{ kind: 'ReplicaSet', name: 'worker-6b7c9d8f5', controller: true }] } };

  it('lists the namespace, the direct controller and when it was created', () => {
    render(<DetailsCard object={objectWith(owned)} namespaced />);
    expect(value('Namespace')).toHaveTextContent('default');
    expect(value('Controlled by')).toHaveTextContent('ReplicaSet worker-6b7c9d8f5');
    expect(value('Controlled by')).toHaveClass('font-mono');
    expect(value('Created')).toHaveTextContent('· 2h ago');
  });

  it('leaves out the namespace for a cluster-scoped kind, and dashes a missing controller', () => {
    render(<DetailsCard object={objectWith({}, { namespace: '' })} namespaced={false} />);
    expect(within(card('Details')).queryByText('Namespace')).not.toBeInTheDocument();
    expect(value('Controlled by')).toHaveTextContent('—');
  });
});

describe('ConditionsCard', () => {
  it('draws each condition, dashing what it leaves out', () => {
    render(
      <ConditionsCard
        object={objectWith({
          status: {
            conditions: [
              { type: 'Ready', status: 'True', reason: 'PodReady', lastTransitionTime: '2026-11-22T11:00:00Z' },
              { type: 'Initialized' },
            ],
          },
        })}
      />,
    );
    const rows = within(card('Conditions')).getAllByRole('row');
    expect(rows.map((r) => Array.from(r.children).map((c) => c.textContent))).toEqual([
      ['Type', 'Status', 'Reason', 'Changed'],
      ['Ready', 'True', 'PodReady', '14m'],
      ['Initialized', '—', '—', '—'],
    ]);
    expect(rows[1].children[2]).toHaveClass('font-mono');
  });

  it('draws nothing for a kind without conditions', () => {
    const { container } = render(<ConditionsCard object={objectWith({ status: {} })} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe('LabelsCard', () => {
  it('draws each label as a chip, counted', () => {
    render(<LabelsCard object={objectWith({ metadata: { labels: { app: 'worker', 'a​b': 'x' } } })} />);
    const chips = within(card('Labels')).getAllByRole('listitem');
    expect(chips.map((c) => c.textContent)).toEqual(['app=worker', 'a\\u{200B}b=x']);
    expect(chips[1]).toHaveAttribute('title', 'a\\u{200B}b=x');
    expect(chips[0]).toHaveClass('rounded-md', 'bg-muted', 'font-mono');
    expect(within(card('Labels')).getByText('2')).toBeInTheDocument();
  });

  it('says when there are none', () => {
    render(<LabelsCard object={objectWith({})} />);
    expect(within(card('Labels')).getByText('No labels.')).toBeInTheDocument();
    expect(within(card('Labels')).getByText('0')).toBeInTheDocument();
  });
});
