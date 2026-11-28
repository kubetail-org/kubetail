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

// A status as a dot and a word on a muted pill (the mockups' `K8s/StatusPill`).
import { cn } from '@kubetail/ui/lib/utils';

import { VisibleText } from '@/components/primitives/visible-text';
import { HEALTH_TONE, TONE } from '@/lib/cluster-health';
import type { StatusTone } from '@/lib/cluster-health';
import { visibleTitle } from '@/lib/visible-text';

export function StatusPill({ label, tone }: { label: string; tone: StatusTone }) {
  return (
    <span
      data-tone={tone}
      title={visibleTitle(label)}
      className="inline-flex max-w-full items-center gap-1.5 rounded-md bg-muted px-2 py-0.5 text-xs font-medium"
    >
      <span aria-hidden className={cn('size-1.75 shrink-0 rounded-full', TONE[HEALTH_TONE[tone]].dot)} />
      <span className={cn('truncate', tone === 'failing' && 'text-destructive')}>
        <VisibleText text={label} />
      </span>
    </span>
  );
}
