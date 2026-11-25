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

// Fixtures for the kinds' column tests.
import { render } from '@testing-library/react';

import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';

import type { ObjectColumn } from './common';

export function objectWith(rawJSON: unknown): ClusterCachedDataObject {
  return {
    uid: 'u-1',
    namespace: 'default',
    name: 'widget-1',
    creationTimestamp: '',
    rawJSON,
  } as ClusterCachedDataObject;
}

// Render a cell to text, since a column's cell may be an element (a date renders a time-ago).
export function cellText(column: ObjectColumn, o: ClusterCachedDataObject): string {
  const { container } = render(<>{column.cell(o)}</>);
  return container.textContent;
}

// The tone a column's status pill draws, if it draws one.
export function pillTone(column: ObjectColumn, o: ClusterCachedDataObject): string | null | undefined {
  const { container } = render(<>{column.cell(o)}</>);
  return container.querySelector('[data-tone]')?.getAttribute('data-tone');
}
