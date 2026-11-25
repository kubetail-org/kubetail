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

// The Permissions pane's form controls: a labelled field and the native select.
import { cn } from '@kubetail/ui/lib/utils';
import { type ComponentProps, type ReactNode, useId } from 'react';

import { visibleTitle } from '@/lib/visible-text';

/** A control under its label; children draws the control with the id the label names. */
export function Field({ label, children }: { label: string; children: (id: string) => ReactNode }) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </label>
      {children(id)}
    </div>
  );
}

type SelectProps<T extends string> = Omit<ComponentProps<'select'>, 'onChange' | 'children'> & {
  options: { value: T; label: string }[];
  onChange?: (value: T) => void;
};

export function Select<T extends string>({ options, className, onChange, ...props }: SelectProps<T>) {
  return (
    <select
      className={cn('h-9 rounded-md border bg-transparent px-3 text-sm', className)}
      onChange={(e) => onChange?.(e.target.value as T)}
      {...props}
    >
      {/* An option holds text alone, so a label is spelled out unmarked. */}
      {options.map(({ value, label }) => (
        <option key={value} value={value}>
          {visibleTitle(label)}
        </option>
      ))}
    </select>
  );
}
