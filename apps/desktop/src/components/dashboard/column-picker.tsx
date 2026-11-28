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

// A table's column picker: a button opening the table's columns in its order, each with a
// checkbox to show or hide it and, at the right edge, a handle to drag it to another place.
// The handle also moves it from the keyboard: Space picks it up, the arrows move it, Space
// drops it.
//
// dnd-kit reads pointer events, not HTML drag and drop: the webview's own file drop
// (Tauri's `dragDropEnabled`) swallows the latter on Windows.
import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors } from '@dnd-kit/core';
import type { DragEndEvent } from '@dnd-kit/core';
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from '@dnd-kit/sortable';
import { CSS } from '@dnd-kit/utilities';
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Label } from '@kubetail/ui/elements/label';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { cn } from '@kubetail/ui/lib/utils';
import { Columns3, GripVertical } from 'lucide-react';

import { VisibleText } from '@/components/primitives/visible-text';

export type PickerColumn = {
  key: string;
  header: string;
  shown: boolean;
  // False for a column the table always shows, such as Name: its box stays checked.
  hideable: boolean;
};

type ColumnPickerProps = {
  // In the table's order.
  columns: readonly PickerColumn[];
  onToggle: (key: string, shown: boolean) => void;
  // Every key, in the new order.
  onReorder: (keys: string[]) => void;
  // Back to the defaults; disabled while nothing differs from them.
  onReset: () => void;
  changed: boolean;
};

export function ColumnPicker({ columns, onToggle, onReorder, onReset, changed }: ColumnPickerProps) {
  const sensors = useSensors(
    useSensor(PointerSensor),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const keys = columns.map((c) => c.key);
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    onReorder(arrayMove(keys, keys.indexOf(String(active.id)), keys.indexOf(String(over.id))));
  };

  return (
    <Popover>
      <PopoverTrigger render={<Button variant="outline" size="icon" />} aria-label="Columns">
        <Columns3 aria-hidden />
      </PopoverTrigger>
      {/* No taller than the room the window leaves it: a kind can have more columns than fit. */}
      <PopoverContent align="end" className="flex max-h-(--available-height) w-auto flex-col gap-1">
        <div className="mb-1 flex items-center justify-between gap-4">
          <p className="text-xs font-medium text-muted-foreground">Columns</p>
          <Button variant="link" size="sm" className="h-auto p-0 text-xs" disabled={!changed} onClick={onReset}>
            Reset
          </Button>
        </div>
        {/* Padded out to its margin, so a scroller does not clip a handle's focus ring. */}
        <div data-testid="column-list" className="-m-1 flex min-h-0 flex-col gap-1 overflow-y-auto p-1">
          <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
            <SortableContext items={keys} strategy={verticalListSortingStrategy}>
              {columns.map((c) => (
                <PickerRow key={c.key} column={c} onToggle={onToggle} />
              ))}
            </SortableContext>
          </DndContext>
        </div>
      </PopoverContent>
    </Popover>
  );
}

function PickerRow({ column, onToggle }: { column: PickerColumn; onToggle: (key: string, shown: boolean) => void }) {
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({
    id: column.key,
  });
  return (
    <div
      ref={setNodeRef}
      data-column={column.key}
      // Up and down alone: the list is one column.
      style={{ transform: CSS.Translate.toString(transform && { ...transform, x: 0 }), transition }}
      className={cn('flex items-center gap-4 rounded-sm bg-popover', isDragging && 'relative z-10 shadow-sm')}
    >
      <Label className="flex-1 font-normal">
        <Checkbox
          checked={column.shown}
          disabled={!column.hideable}
          onCheckedChange={(on) => onToggle(column.key, on)}
        />
        {/* A CRD names its own columns. */}
        <VisibleText text={column.header} />
      </Label>
      <button
        type="button"
        ref={setActivatorNodeRef}
        {...attributes}
        {...listeners}
        aria-label={`Move ${column.header}`}
        className="touch-none rounded-sm text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
      >
        <GripVertical className="size-4" aria-hidden />
      </button>
    </div>
  );
}
