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
import { describe, expect, it, vi } from 'vitest';

const { logViewerMock } = vi.hoisted(() => ({ logViewerMock: vi.fn(() => <div data-testid="log-viewer" />) }));
vi.mock('@/components/logs/log-viewer', () => ({ LogViewer: logViewerMock }));
vi.mock('@/lib/active-cluster', () => ({ useActiveCluster: () => ({ clusterID: 'c1' }) }));

const { ObjectLogsView } = await import('./logs-view');

const OBJECT = {
  apiVersion: 'apps/v1',
  resource: 'deployments',
  kind: 'Deployment',
  uid: 'u1',
  namespace: 'shop',
  name: 'web',
};

describe('ObjectLogsView', () => {
  it('reads every container of the object from its newest line, following', () => {
    render(<ObjectLogsView object={OBJECT} />);
    expect(logViewerMock).toHaveBeenCalledWith(
      expect.objectContaining({
        clusterId: 'c1',
        query: {
          sources: [
            {
              namespace: 'shop',
              kind: 'Deployment',
              name: 'web',
              containers: [],
              allContainers: true,
              previous: false,
            },
          ],
          filters: [],
          grep: '',
          anchor: { kind: 'Tail', at: null },
          pinToEnd: true,
          resolvedAt: '',
        },
        openAt: { kind: 'Tail', at: null },
        pinToEnd: true,
      }),
      undefined,
    );
  });
});
