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

import { DetailRow } from '@/components/dashboard/detail-row';
import { VisibleText } from '@/components/primitives/visible-text';
import { visibleTitle } from '@/lib/visible-text';

// A row of text off the object, a dash for none.
export function TextRow({ label, text, mono = false }: { label: string; text: string | undefined; mono?: boolean }) {
  return (
    <DetailRow
      label={label}
      title={text ? visibleTitle(text) : undefined}
      className={mono ? 'font-mono text-foreground' : undefined}
    >
      <VisibleText text={text || '—'} />
    </DetailRow>
  );
}
