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

// Which target the list shows: all of them, or one by its label.
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';
import { useId } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import type { MonitorTarget } from '@/lib/incidents';

// The filter's value for every target. Target ids are UUIDs, so none is this.
export const ALL_TARGETS = 'all';

export function TargetFilter({
  targets,
  value,
  onPick,
}: {
  targets: MonitorTarget[];
  value: string;
  onPick: (targetID: string) => void;
}) {
  const labelID = useId();
  const pickedTarget = targets.find((t) => t.id === value);
  return (
    <div className="flex items-center gap-2">
      <span id={labelID} className="sr-only">
        Target
      </span>
      <Select value={value} onValueChange={(next) => onPick(next as string)}>
        <SelectTrigger size="sm" aria-labelledby={labelID} className="w-48">
          <SelectValue>{pickedTarget ? <VisibleText text={pickedTarget.label} /> : 'All targets'}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectItem value={ALL_TARGETS}>All targets</SelectItem>
          <SelectSeparator />
          {targets.map((t) => (
            <SelectItem key={t.id} value={t.id}>
              <VisibleText text={t.label} />
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
