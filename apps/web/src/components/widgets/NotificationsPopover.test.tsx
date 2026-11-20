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

import { render } from '@testing-library/react';
import type { Mock } from 'vitest';

import { useAllClusterUpdateViews, useCLIUpdateNotification, useHasAnyClusterUpdate } from '@/lib/update-notifications';

import { NotificationsPopover } from './NotificationsPopover';

vi.mock('@/lib/update-notifications', () => ({
  useAllClusterUpdateViews: vi.fn(),
  useCLIUpdateNotification: vi.fn(),
  useHasAnyClusterUpdate: vi.fn(),
}));

function renderPopover() {
  return render(
    <NotificationsPopover>
      <button type="button">Notifications</button>
    </NotificationsPopover>,
  );
}

describe('NotificationsPopover', () => {
  beforeEach(() => {
    (useCLIUpdateNotification as Mock).mockReturnValue({
      updateAvailable: false,
      currentVersion: '1.0.0',
      latestVersion: null,
    });
    (useHasAnyClusterUpdate as Mock).mockReturnValue(false);
    (useAllClusterUpdateViews as Mock).mockReturnValue([]);
  });

  it('shows no dot without notifications', () => {
    const { container } = renderPopover();

    expect(container.querySelector('.bg-blue-500')).toBeNull();
  });

  it('shows a dot when a CLI update is available', () => {
    (useCLIUpdateNotification as Mock).mockReturnValue({
      updateAvailable: true,
      currentVersion: '1.0.0',
      latestVersion: '1.1.0',
    });

    const { container } = renderPopover();

    expect(container.querySelector('.bg-blue-500')).not.toBeNull();
  });
});
