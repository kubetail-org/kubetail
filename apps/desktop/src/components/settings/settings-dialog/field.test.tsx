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
import { describe, expect, it, vi } from 'vitest';

import { Field, Select } from './field';

const OPTIONS = [
  { value: 'a', label: 'First' },
  { value: 'b', label: 'prod\u202Eue' },
];

describe('Field', () => {
  it('names its control by its label', () => {
    render(<Field label="Context">{(id) => <input id={id} />}</Field>);
    expect(screen.getByLabelText('Context')).toBeInstanceOf(HTMLInputElement);
  });
});

describe('Select', () => {
  it('answers the value picked', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<Select aria-label="Pick" options={OPTIONS} value="a" onChange={onChange} />);
    await user.selectOptions(screen.getByRole('combobox', { name: 'Pick' }), 'b');
    expect(onChange).toHaveBeenCalledWith('b');
  });

  it('keeps its own pick when given no handler', async () => {
    const user = userEvent.setup();
    render(<Select aria-label="Pick" options={OPTIONS} />);
    await user.selectOptions(screen.getByRole('combobox'), 'b');
    expect(screen.getByRole('combobox')).toHaveValue('b');
  });

  it('spells an option out', () => {
    render(<Select aria-label="Pick" options={OPTIONS} />);
    expect(within(screen.getByRole('combobox')).getAllByRole('option')[1]).toHaveTextContent('prod\\u{202E}ue');
  });
});
