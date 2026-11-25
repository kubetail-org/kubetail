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

// A cell's right-click menu, kubetail's `CellContextMenu` ported: fixed labels, and what each
// copies is the cell's plain text.
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from '@kubetail/ui/elements/context-menu';
import type { ReactElement } from 'react';

import { copyToClipboard } from '@/lib/clipboard';
import type { LogLine } from '@/lib/log-viewer';

import type { Column } from './columns';
import { getPlainAttribute } from './selection';

function items(line: LogLine, col: Column) {
  if (col === 'Timestamp') {
    return (
      <>
        <ContextMenuItem onClick={() => copyToClipboard(getPlainAttribute(line, col))}>Copy timestamp</ContextMenuItem>
        {/* The drawn time leaves out today's date; this is the whole of it. */}
        <ContextMenuItem onClick={() => copyToClipboard(line.timestamp)}>Copy as RFC 3339</ContextMenuItem>
      </>
    );
  }
  if (col === 'Message') {
    return (
      <ContextMenuItem onClick={() => copyToClipboard(getPlainAttribute(line, col))}>Copy message</ContextMenuItem>
    );
  }
  return <ContextMenuItem onClick={() => copyToClipboard(getPlainAttribute(line, col))}>Copy</ContextMenuItem>;
}

export function CellContextMenu({ col, line, children }: { col: Column; line: LogLine; children: ReactElement }) {
  if (col === 'Dot') return children;
  return (
    <ContextMenu>
      <ContextMenuTrigger render={children} />
      <ContextMenuContent>{items(line, col)}</ContextMenuContent>
    </ContextMenu>
  );
}
