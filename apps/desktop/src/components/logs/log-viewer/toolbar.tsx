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

// The log viewer's controls: jump to a time, to the start or to the end, pause and play, a grep
// box, and a View popover for the columns and wrap. The pin has no button: scrolling pins and unpins.
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Label } from '@kubetail/ui/elements/label';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Columns3, Pause, Play, SkipBack, SkipForward } from 'lucide-react';
import { useId, useState } from 'react';

import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';

import { COLUMNS, type Column } from './columns';
import { JumpToTime } from './jump-to-time';

export type LogViewerToolbarProps = {
  loading: boolean;
  paused: boolean;
  columns: Column[];
  wrap: boolean;
  /** The grep in effect; it seeds the box. */
  grep: string;
  /** Why the last submitted grep was refused, null while none is. */
  grepError: string | null;
  onJumpTo: (at: Date) => void;
  onStart: () => void;
  onEnd: () => void;
  onPausedChange: (paused: boolean) => void;
  onColumnsChange: (columns: Column[]) => void;
  onWrapChange: (wrap: boolean) => void;
  /** The box's text on Enter; empty clears the grep. */
  onGrepChange: (grep: string) => void;
};

/** The columns the user picks: every one but the message, which is always on. */
const PICKABLE = COLUMNS.filter((col) => col !== 'Message');

// The draft stays as typed after a refusal, so the user can fix it.
function GrepBox({ grep, error, onSubmit }: { grep: string; error: string | null; onSubmit: (grep: string) => void }) {
  const [draft, setDraft] = useState(grep);
  const errorId = useId();
  const invalid = error !== null;
  return (
    <>
      <Input
        aria-label="Grep"
        aria-invalid={invalid}
        aria-describedby={invalid ? errorId : undefined}
        placeholder="Grep"
        value={draft}
        onChange={(ev) => setDraft(ev.target.value)}
        onKeyDown={(ev) => ev.key === 'Enter' && !ev.nativeEvent.isComposing && onSubmit(draft)}
        className="ml-2 h-7 w-48 font-mono text-xs"
      />
      {invalid && (
        <p id={errorId} className="min-w-0 truncate text-xs text-destructive">
          <VisibleText text={error} />
        </p>
      )}
    </>
  );
}

function Option({ label, checked, onChange }: { label: string; checked: boolean; onChange: (on: boolean) => void }) {
  return (
    <Label className="font-normal">
      <Checkbox checked={checked} onCheckedChange={(on) => onChange(on)} />
      {label}
    </Label>
  );
}

export function LogViewerToolbar({
  loading,
  paused,
  columns,
  wrap,
  grep,
  grepError,
  onJumpTo,
  onStart,
  onEnd,
  onPausedChange,
  onColumnsChange,
  onWrapChange,
  onGrepChange,
}: LogViewerToolbarProps) {
  const toggle = (col: Column, on: boolean) => {
    const picked = new Set(columns);
    if (on) picked.add(col);
    else picked.delete(col);
    // Column order, whatever order they were picked in.
    onColumnsChange(COLUMNS.filter((c) => picked.has(c)));
  };

  return (
    <div className="flex shrink-0 items-center gap-1 border-b px-2 py-1">
      <JumpToTime disabled={loading} onJump={onJumpTo} />
      <Button variant="ghost" size="icon-sm" aria-label="Jump to the start" disabled={loading} onClick={onStart}>
        <SkipBack aria-hidden />
      </Button>
      {paused ? (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Play"
          disabled={loading}
          onClick={() => onPausedChange(false)}
        >
          <Play aria-hidden />
        </Button>
      ) : (
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Pause"
          disabled={loading}
          onClick={() => onPausedChange(true)}
        >
          <Pause aria-hidden />
        </Button>
      )}
      <Button variant="ghost" size="icon-sm" aria-label="Jump to the end" disabled={loading} onClick={onEnd}>
        <SkipForward aria-hidden />
      </Button>
      <GrepBox grep={grep} error={grepError} onSubmit={onGrepChange} />
      <Popover>
        <PopoverTrigger
          render={<Button variant="ghost" size="icon-sm" className="ml-auto" disabled={loading} />}
          aria-label="View"
        >
          <Columns3 aria-hidden />
        </PopoverTrigger>
        <PopoverContent align="end" className="flex w-auto flex-col gap-2">
          <p className="text-xs font-medium text-muted-foreground">Columns</p>
          {PICKABLE.map((col) => (
            <Option key={col} label={col} checked={columns.includes(col)} onChange={(on) => toggle(col, on)} />
          ))}
          <p className="mt-1 text-xs font-medium text-muted-foreground">Options</p>
          <Option label="Wrap" checked={wrap} onChange={onWrapChange} />
        </PopoverContent>
      </Popover>
    </div>
  );
}
