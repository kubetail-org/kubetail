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
import { createStore, Provider } from 'jotai';

import { timezoneAtom } from '@/lib/timezone';

import AdaptiveTimeAgo from './AdaptiveTimeAgo';

describe('AdaptiveTimeAgo', () => {
  it('shows UTC-formatted tooltip by default', () => {
    const date = new Date('2024-06-15T10:30:00Z');
    render(<AdaptiveTimeAgo date={date} />);
    const el = screen.getByTitle(/Jun 15, 2024/);
    expect(el.getAttribute('title')).toBe('Jun 15, 2024 10:30:00 (UTC)');
  });

  it('shows tooltip formatted in the selected timezone', () => {
    const store = createStore();
    store.set(timezoneAtom, 'America/New_York');

    const date = new Date('2024-06-15T10:30:00Z');
    render(
      <Provider store={store}>
        <AdaptiveTimeAgo date={date} />
      </Provider>,
    );
    const el = screen.getByTitle(/Jun 15, 2024/);
    expect(el.getAttribute('title')).toBe('Jun 15, 2024 06:30:00 (EDT)');
  });
});
