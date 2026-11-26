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

import { cn } from '@kubetail/ui/lib/utils';

import { TONE } from '@/lib/cluster-health';
import type { Tone } from '@/lib/cluster-health';

// A cluster's overall tone, as the Clusters dialog's leading circle draws it.
export function ClusterDot({ tone, title, className }: { tone: Tone; title?: string; className?: string }) {
  return (
    <span
      data-testid="cluster-dot"
      data-tone={tone}
      title={title}
      className={cn('size-2 shrink-0 rounded-full', TONE[tone].dot, className)}
    />
  );
}
