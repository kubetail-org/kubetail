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

import { Fragment } from 'react';

import { visibleSegments } from '@/lib/visible-text';

// Text the user must read as it is: every invisible or reordering character
// spelled out, and marked so it cannot pass for the text's own.
export function VisibleText({ text, trailing }: { text: string; trailing?: 'lines' | 'end' | 'edges' }) {
  return visibleSegments(text, trailing).map((segment, i) =>
    segment.spelled ? (
      // oxlint-disable-next-line react/no-array-index-key -- a text's segments are cut afresh and never reorder
      <mark key={i} className="rounded-sm bg-destructive/20 px-0.5 text-destructive" title="An invisible character">
        {segment.text}
      </mark>
    ) : (
      // oxlint-disable-next-line react/no-array-index-key -- a text's segments are cut afresh and never reorder
      <Fragment key={i}>{segment.text}</Fragment>
    ),
  );
}
