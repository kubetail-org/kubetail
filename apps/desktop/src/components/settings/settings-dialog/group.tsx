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

// A titled section of a settings pane: the heading names the group, with an
// optional info control beside it and an action at its right.
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { cn } from '@kubetail/ui/lib/utils';
import { Info } from 'lucide-react';
import { type ReactNode, useId } from 'react';

type GroupProps = {
  title: string;
  lede?: ReactNode;
  info?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
};

export function Group({ title, lede, info, action, children }: GroupProps) {
  const headingID = useId();
  return (
    <div role="group" aria-labelledby={headingID} className="flex flex-col gap-2">
      <div className="flex items-end gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex items-center gap-1">
            <h4 id={headingID} className="text-sm font-medium text-popover-foreground">
              {title}
            </h4>
            {info}
          </div>
          {lede && <p className="text-xs text-muted-foreground">{lede}</p>}
        </div>
        {action}
      </div>
      {children}
    </div>
  );
}

// A pane that is one list, which the pane's own heading names: its lede over its body.
export function Intro({ lede, children }: { lede: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs text-muted-foreground">{lede}</p>
      {children}
    </div>
  );
}

// A row of a group's card: a label, then its note.
export function NoteRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <li className="flex gap-4 px-4 py-3">
      <span className="w-28 shrink-0 text-sm text-popover-foreground">{label}</span>
      <div className="min-w-0 flex-1 text-sm text-muted-foreground">{children}</div>
    </li>
  );
}

// The info icon beside a group's heading, opening a note.
export function InfoPopover({
  label,
  className,
  children,
}: {
  label: string;
  /** Classes for the note's popup. */
  className?: string;
  children: ReactNode;
}) {
  return (
    <Popover>
      <PopoverTrigger
        render={<Button variant="ghost" size="icon" className="size-5 text-muted-foreground" />}
        aria-label={label}
      >
        <Info aria-hidden />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className={cn('flex max-h-(--available-height) w-96 flex-col gap-3 text-xs', className)}
      >
        {children}
      </PopoverContent>
    </Popover>
  );
}
