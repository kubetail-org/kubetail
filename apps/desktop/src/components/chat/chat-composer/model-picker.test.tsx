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

import { act, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';

import type { Model, ModelPick } from '@/lib/models';
import { modelOf } from '@/lib/models';

import { ModelPicker } from './model-picker';

const fake: Model = {
  provider: { id: 'fake', label: 'Fake' },
  id: 'fake',
  label: 'Fake model',
  efforts: ['low', 'high'],
  defaultEffort: 'high',
};
const plain: Model = {
  provider: { id: 'fake', label: 'Fake' },
  id: 'plain',
  label: 'Plain model',
  efforts: [],
  defaultEffort: '',
};
const opus: Model = {
  provider: { id: 'anthropic', label: 'Anthropic' },
  id: 'claude-opus-5',
  label: 'Opus 5',
  efforts: ['low', 'medium', 'high'],
  defaultEffort: 'medium',
};

// The picker holds no pick of its own, so the harness keeps it as the outbox would.
function Harness({ models, effort = 'high' }: { models: Model[]; effort?: string }) {
  const [pick, setPick] = useState<ModelPick>({ model: { providerID: 'fake', id: 'fake' }, effort });
  return <ModelPicker models={models} pick={pick} picked={modelOf(models, pick.model)} onPick={setPick} />;
}

const trigger = () => screen.getByRole('button', { name: /^Model:/ });

// The popup portals in asynchronously, so wait for it rather than reading it.
async function open(user: ReturnType<typeof userEvent.setup>) {
  await user.click(trigger());
  return screen.findByRole('dialog');
}

const option = (group: string, name: string) =>
  within(screen.getByRole('radiogroup', { name: group })).getByRole('radio', { name });

describe('ModelPicker', () => {
  it('names the model and the effort on one button', () => {
    render(<Harness models={[fake, plain]} />);
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: high');
    expect(trigger()).toHaveTextContent('Fake modelhigh');
  });

  it('picks the effort', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    await open(user);
    expect(option('Effort', 'high')).toBeChecked();

    await user.click(option('Effort', 'low'));
    expect(option('Effort', 'low')).toBeChecked();
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: low');
  });

  // A stored pick can name a level the model has since dropped.
  it('names an effort the model does not list as it is stored', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} effort="medium" />);
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: medium');

    await open(user);
    expect(
      within(screen.getByRole('radiogroup', { name: 'Effort' })).queryByRole('radio', { checked: true }),
    ).toBeNull();
  });

  // The levels are the provider's own words, so the old pick may not exist on the
  // new model.
  it("resets the effort to the new model's default", async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain, opus]} />);
    await open(user);
    await user.click(option('Effort', 'low'));

    await user.click(screen.getByRole('button', { name: 'Anthropic', pressed: false }));
    await user.click(option('Model', 'Opus 5'));
    expect(option('Model', 'Opus 5')).toBeChecked();
    expect(trigger()).toHaveAccessibleName('Model: Opus 5, effort: medium');
  });

  it('keeps the effort when the picked model is picked again', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    await open(user);
    await user.click(option('Effort', 'low'));

    await user.click(option('Model', 'Fake model'));
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: low');
  });

  // Each group is one Tab stop, and the arrows move the pick within it.
  it('moves through each group with the arrow keys', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    await open(user);

    act(() => option('Model', 'Fake model').focus());
    await user.keyboard('{ArrowDown}');
    expect(option('Model', 'Plain model')).toHaveFocus();
    expect(trigger()).toHaveAccessibleName('Model: Plain model');

    await user.keyboard('{ArrowUp}');
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: high');
    await user.tab();
    expect(option('Effort', 'high')).toHaveFocus();
    await user.keyboard('{ArrowLeft}');
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: low');
  });

  it('hides the effort for a model with no such knob', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    await open(user);
    await user.click(option('Model', 'Plain model'));

    expect(trigger()).toHaveAccessibleName('Model: Plain model');
    expect(screen.queryByRole('radiogroup', { name: 'Effort' })).toBeNull();
  });

  // Browsing another provider picks nothing, and the menu opens on the pick's.
  it("shows a provider's models on its tab and reopens on the pick's", async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain, opus]} />);
    const popup = await open(user);
    expect(within(popup).getByRole('button', { name: 'Fake' })).toHaveAttribute('aria-pressed', 'true');
    expect(
      within(screen.getByRole('radiogroup', { name: 'Model' }))
        .getAllByRole('radio')
        .map((r) => r.textContent),
    ).toEqual(['Fake model', 'Plain model']);

    await user.click(within(popup).getByRole('button', { name: 'Anthropic' }));
    expect(
      within(screen.getByRole('radiogroup', { name: 'Model' }))
        .getAllByRole('radio')
        .map((r) => r.textContent),
    ).toEqual(['Opus 5']);
    expect(trigger()).toHaveAccessibleName('Model: Fake model, effort: high');

    await user.keyboard('{Escape}');
    await open(user);
    expect(screen.getByRole('button', { name: 'Fake' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('draws no tabs for a catalog of one provider', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    await open(user);
    expect(screen.queryByRole('group', { name: 'Provider' })).toBeNull();
  });

  it('holds Manage providers in its footer', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    const popup = await open(user);
    expect(within(popup).getByRole('button', { name: 'Manage providers' })).toBeInTheDocument();
  });

  // jsdom has no layout, so this pins the shape: the popup is capped at the room on
  // its side, and the model list is what scrolls.
  it('caps its height and scrolls the model list', async () => {
    const user = userEvent.setup();
    render(<Harness models={[fake, plain]} />);
    const popup = await open(user);
    expect(popup.style.maxHeight).toBe('var(--available-height)');
    expect(screen.getByRole('radiogroup', { name: 'Model' })).toHaveClass('min-h-0', 'overflow-y-auto');
  });
});
