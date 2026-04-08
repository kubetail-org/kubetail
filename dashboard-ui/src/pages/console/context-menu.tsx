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

import { format, toZonedTime } from 'date-fns-tz';
import React from 'react';

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from '@kubetail/ui/elements/context-menu';
import { stripAnsi } from 'fancy-ansi';

import type { LogRecord } from '@/components/widgets/log-viewer';

import { getPlainAttribute } from './selection';
import { ViewerColumn } from './shared';

function copyToClipboard(text: string) {
  navigator.clipboard.writeText(text);
}

function TimestampMenuContent({ record }: { record: LogRecord }) {
  const tsWithTZ = toZonedTime(record.timestamp, 'UTC');
  const displayed = format(tsWithTZ, 'LLL dd, y HH:mm:ss.SSS', { timeZone: 'UTC' });

  return (
    <>
      <ContextMenuItem onSelect={() => copyToClipboard(displayed)}>Copy timestamp</ContextMenuItem>
      <ContextMenuSub>
        <ContextMenuSubTrigger>Copy as...</ContextMenuSubTrigger>
        <ContextMenuSubContent>
          <ContextMenuItem onSelect={() => copyToClipboard(record.timestamp)}>ISO 8601</ContextMenuItem>
          <ContextMenuItem
            onSelect={() => copyToClipboard(String(Math.floor(new Date(record.timestamp).getTime() / 1000)))}
          >
            Unix (seconds)
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => copyToClipboard(String(new Date(record.timestamp).getTime()))}>
            Unix (milliseconds)
          </ContextMenuItem>
          <ContextMenuItem onSelect={() => copyToClipboard(new Date(record.timestamp).toLocaleString())}>
            Local time
          </ContextMenuItem>
        </ContextMenuSubContent>
      </ContextMenuSub>
    </>
  );
}

function MessageMenuContent({ record }: { record: LogRecord }) {
  return (
    <>
      <ContextMenuItem onSelect={() => copyToClipboard(stripAnsi(record.message))}>Copy message</ContextMenuItem>
      <ContextMenuItem onSelect={() => copyToClipboard(record.message)}>Copy message (ANSI)</ContextMenuItem>
    </>
  );
}

function DefaultMenuContent({ record, col }: { record: LogRecord; col: ViewerColumn }) {
  return <ContextMenuItem onSelect={() => copyToClipboard(getPlainAttribute(record, col))}>Copy</ContextMenuItem>;
}

type CellContextMenuProps = {
  col: ViewerColumn;
  record: LogRecord;
  children: React.ReactElement;
};

export function CellContextMenu({ col, record, children }: CellContextMenuProps) {
  if (col === ViewerColumn.ColorDot) {
    return children;
  }

  let content: React.ReactNode;
  if (col === ViewerColumn.Timestamp) {
    content = <TimestampMenuContent record={record} />;
  } else if (col === ViewerColumn.Message) {
    content = <MessageMenuContent record={record} />;
  } else {
    content = <DefaultMenuContent record={record} col={col} />;
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent>{content}</ContextMenuContent>
    </ContextMenu>
  );
}
