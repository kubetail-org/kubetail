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

// One row of a detail card's key/value list: a 120px label, then the value, truncated.
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode } from 'react';

type DetailRowProps = {
  label: string;
  title?: string;
  // The value's classes, in place of the foreground colour.
  className?: string;
  onPointerEnter?: () => void;
  children: ReactNode;
};

export function DetailRow({ label, title, className, onPointerEnter, children }: DetailRowProps) {
  return (
    <div className="flex gap-2 text-xs/4.5">
      <dt className="w-30 shrink-0 text-muted-foreground">{label}</dt>
      <dd
        className={cn('min-w-0 flex-1 truncate', className ?? 'text-foreground')}
        title={title}
        onPointerEnter={onPointerEnter}
      >
        {children}
      </dd>
    </div>
  );
}
