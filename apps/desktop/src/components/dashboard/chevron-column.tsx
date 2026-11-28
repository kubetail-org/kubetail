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

// A table's last column: a chevron on every row that opens it, after any other controls the
// table gives the row.
import { Button } from '@kubetail/ui/elements/button';
import { ChevronRight } from 'lucide-react';
import type { ReactNode } from 'react';

import type { VirtualColumn } from '@/components/dashboard/virtual-table';
import { visibleTitle } from '@/lib/visible-text';

// An `icon-sm` button is 28px; the last cell pads it 6px left and 12px right.
const BUTTON_PX = 28;
const CELL_PADDING_PX = 18;

type ChevronColumnOptions<T> = {
  // The row's name, for the button's label.
  nameOf: (row: T) => string;
  onOpen: (row: T) => void;
  // Controls drawn before the chevron, each one `icon-sm` button wide.
  before?: { buttons: number; cell: (row: T) => ReactNode };
};

export function chevronColumn<T>({ nameOf, onOpen, before }: ChevronColumnOptions<T>): VirtualColumn<T> {
  return {
    key: 'actions',
    // The buttons name themselves; a header would only crowd the narrow column.
    header: '',
    className: 'text-right',
    pinned: true,
    width: BUTTON_PX * (1 + (before?.buttons ?? 0)) + CELL_PADDING_PX,
    cell: (row) => (
      <div className="flex justify-end">
        {before?.cell(row)}
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          aria-label={`Open ${visibleTitle(nameOf(row))}`}
          onClick={() => onOpen(row)}
        >
          <ChevronRight className="size-4 text-muted-foreground" aria-hidden />
        </Button>
      </div>
    ),
  };
}
