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

// One folder of a Settings folder list: the folder, drawn through VisibleText,
// what is said of it, and its control.
import { cn } from '@kubetail/ui/lib/utils';
import type { ReactNode } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';

export function FolderRow({
  dir,
  muted = false,
  control,
  children,
}: {
  dir: string;
  muted?: boolean;
  control?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <li className="flex items-center gap-3 px-4 py-2">
      <div className="min-w-0 flex-1 text-xs">
        <p
          className={cn('font-mono text-sm break-all whitespace-pre-wrap', muted && 'text-muted-foreground')}
          data-testid="folder"
        >
          <VisibleText text={dir} trailing="end" />
        </p>
        {children}
      </div>
      {control}
    </li>
  );
}
