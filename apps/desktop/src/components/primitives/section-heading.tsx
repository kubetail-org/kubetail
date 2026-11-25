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
import type { LucideIcon } from 'lucide-react';

// A section's heading in a sidebar list: a muted, capitalized label, after an icon when
// given one. The capitals are CSS; the label stays as written.
export function SectionHeading({
  label,
  icon: Icon,
  className,
}: {
  label: string;
  icon?: LucideIcon;
  className?: string;
}) {
  return (
    <h3
      className={cn(
        'mt-2 flex items-center gap-1.5 p-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase',
        className,
      )}
    >
      {Icon && <Icon className="size-3 shrink-0" aria-hidden />}
      <span className="truncate">{label}</span>
    </h3>
  );
}
