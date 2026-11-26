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

import { SourceLine } from './log-source-line';

describe('SourceLine', () => {
  it('names the source, its containers and the previous instance, the names spelled out', () => {
    const { container } = render(
      <SourceLine
        source={{
          namespace: 'prod',
          kind: 'Pod',
          name: 'work‮er-1',
          containers: ['app', 'istio-proxy'],
          allContainers: false,
          previous: true,
        }}
      />,
    );
    expect(container).toHaveTextContent('Pod work');
    expect(container).toHaveTextContent('er-1 in prod (app, istio-proxy), previous instance');
    expect(container.querySelector('mark')).not.toBeNull();
    expect(screen.queryByText('‮')).not.toBeInTheDocument();
  });

  it('says every container for one that reads them all, and nothing for the default', () => {
    const source = { namespace: 'prod', kind: 'Pod' as const, name: 'db-0', containers: [], previous: false };
    const { container, rerender } = render(<SourceLine source={{ ...source, allContainers: true }} />);
    expect(container).toHaveTextContent(/^Pod db-0 in prod \(every container\)$/);
    rerender(<SourceLine source={{ ...source, allContainers: false }} />);
    expect(container).toHaveTextContent(/^Pod db-0 in prod$/);
  });
});
