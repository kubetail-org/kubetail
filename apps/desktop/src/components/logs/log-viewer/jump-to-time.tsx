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

// The toolbar's jump to a time (kubetail's `DateRangeDropdown`): a moment some minutes, hours,
// days or weeks ago, or a timestamp typed in one of the formats logs print. The viewer draws
// local time, so a timestamp with no offset is read as local time.
import { Alert, AlertDescription, AlertTitle } from '@kubetail/ui/elements/alert';
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { Clock, History } from 'lucide-react';
import { Fragment, useState } from 'react';

import { Input } from '@/components/primitives/text-entry';

export type Unit = 'minutes' | 'hours' | 'days' | 'weeks';

const UNIT_LABELS: Record<Unit, string> = {
  minutes: 'Minutes ago',
  hours: 'Hours ago',
  days: 'Days ago',
  weeks: 'Weeks ago',
};

/** The moment `value` units before `from`, on the local calendar. */
export function ago(value: number, unit: Unit, from = new Date()): Date {
  const d = new Date(from);
  if (unit === 'minutes') d.setMinutes(d.getMinutes() - value);
  else if (unit === 'hours') d.setHours(d.getHours() - value);
  else if (unit === 'days') d.setDate(d.getDate() - value);
  else d.setDate(d.getDate() - value * 7);
  return d;
}

const isValid = (d: Date) => !Number.isNaN(d.getTime());

const MONTHS = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

// Apache's `02/Jan/2006:15:04:05 -0700`, the month a name or a number.
const CLF_RE = /^(\d{2})\/(\d{2}|[a-z]{3})\/(\d{4}):(\d{2}):(\d{2}):(\d{2})(?:\s+([+-])(\d{2}):?(\d{2}))?$/i;

function parseClf(input: string): Date | undefined {
  const m = CLF_RE.exec(input);
  if (!m) return undefined;
  const [, day, month, year, hour, minute, second, sign, offsetHours, offsetMinutes] = m;
  const monthIndex = /^\d+$/.test(month) ? Number(month) - 1 : MONTHS.indexOf(month.toLowerCase());
  const fields = [Number(year), monthIndex, Number(day), Number(hour), Number(minute), Number(second)] as const;
  // `Date.UTC` rolls a 31st of February into March; the round trip refuses it.
  const wall = new Date(Date.UTC(...fields));
  if (wall.getUTCMonth() !== monthIndex || wall.getUTCDate() !== Number(day)) return undefined;
  if (sign === undefined) return new Date(...fields);
  const offsetMs = (Number(offsetHours) * 60 + Number(offsetMinutes)) * 60_000;
  return new Date(wall.getTime() - (sign === '+' ? offsetMs : -offsetMs));
}

/** A typed timestamp as a moment, or undefined when it reads as none. */
export function parseTimestamp(input: string): Date | undefined {
  const trimmed = input.trim();
  if (!trimmed) return undefined;

  // A Unix time: 13 digits or more is milliseconds (2001 on), anything shorter seconds.
  if (/^\d+$/.test(trimmed)) {
    const n = Number(trimmed);
    const d = new Date(trimmed.length >= 13 ? n : n * 1000);
    return isValid(d) ? d : undefined;
  }

  // `Date` reads a date alone as UTC midnight, and WebKit refuses a space between the date and
  // the time, so both are spelled as ISO with no offset, which `Date` reads as local time.
  const iso = /^\d{4}-\d{2}-\d{2}$/.test(trimmed)
    ? `${trimmed}T00:00`
    : trimmed.replace(/^(\d{4}-\d{2}-\d{2}) (?=\d)/, '$1T');
  const native = new Date(iso);
  if (isValid(native)) return native;

  return parseClf(trimmed);
}

const PRESETS: { label: string; unit: Unit; values: number[] }[] = [
  { label: 'Minutes', unit: 'minutes', values: [5, 15, 30] },
  { label: 'Hours', unit: 'hours', values: [1, 2, 6] },
  { label: 'Days', unit: 'days', values: [1, 3, 7] },
];

function RelativeTimeForm({ onApply }: { onApply: (at: Date) => void }) {
  const [value, setValue] = useState('');
  const [unit, setUnit] = useState<Unit>('minutes');
  const [error, setError] = useState('');

  const apply = () => {
    const n = Number(value);
    if (Number.isNaN(n) || n <= 0) {
      setError('Enter a positive number');
      return;
    }
    setError('');
    onApply(ago(n, unit));
  };

  return (
    <div className="flex flex-1 flex-col">
      <div className="flex flex-1 flex-col rounded-md border border-border bg-muted p-3">
        <h3 className="mb-3 text-xs font-semibold tracking-wider text-muted-foreground uppercase">Relative Time</h3>
        <div className="my-auto grid grid-cols-[auto_1fr_1fr_1fr] items-center gap-x-2 gap-y-1">
          {PRESETS.map((group) => (
            <Fragment key={group.label}>
              <span className="pl-3 text-xs text-muted-foreground">{group.label}</span>
              {group.values.map((v) => (
                <Button
                  key={v}
                  variant="ghost"
                  size="sm"
                  className="text-right"
                  onClick={() => onApply(ago(v, group.unit))}
                >
                  {v}
                </Button>
              ))}
            </Fragment>
          ))}
        </div>
        <div className="mt-auto flex gap-2" title={error || undefined}>
          <Input
            type="number"
            min="1"
            value={value}
            placeholder="Value"
            onChange={(ev) => {
              setValue(ev.target.value);
              setError('');
            }}
            onKeyDown={(ev) => ev.key === 'Enter' && apply()}
            className={`w-25 shrink-0 ${error ? 'border-destructive' : ''}`}
          />
          <Select value={unit} onValueChange={(next) => setUnit(next as Unit)}>
            <SelectTrigger className="w-30 flex-1 gap-1 px-2">
              <SelectValue>{(next) => UNIT_LABELS[next as Unit]}</SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              {(Object.keys(UNIT_LABELS) as Unit[]).map((u) => (
                <SelectItem key={u} value={u}>
                  {UNIT_LABELS[u]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <Button className="mt-3 w-full" onClick={apply}>
        Apply
      </Button>
    </div>
  );
}

function AbsoluteTimeForm({ onApply }: { onApply: (at: Date) => void }) {
  const [input, setInput] = useState('');
  const [error, setError] = useState('');

  const apply = () => {
    const at = parseTimestamp(input);
    if (!at) {
      setError('Unable to parse timestamp');
      return;
    }
    setError('');
    onApply(at);
  };

  return (
    <div className="flex flex-1 flex-col">
      <div className="flex flex-1 flex-col rounded-md border border-border bg-muted p-3">
        <h3 className="mb-3 text-xs font-semibold tracking-wider text-muted-foreground uppercase">Absolute Time</h3>
        <Alert className="mb-3 text-xs">
          <AlertTitle className="flex items-center">Supported Formats</AlertTitle>
          <AlertDescription className="mt-1 max-h-20 overflow-y-auto font-mono text-xs">
            <ul className="space-y-1 whitespace-nowrap">
              <li title="ISO 8601 / RFC 3339">&gt; 2006-01-02T15:04:05+07:00</li>
              <li title="RFC 1123 / RFC 2822">&gt; Mon, 02 Jan 2006 15:04:05 -0700</li>
              <li title="Apache CLF">&gt; 02/Jan/2006:15:04:05 -0700</li>
              <li title="Unix timestamp (seconds)">&gt; 1136214245</li>
              <li title="Unix timestamp (milliseconds)">&gt; 1776393600000</li>
            </ul>
          </AlertDescription>
        </Alert>
        <div className="relative mt-auto" title={error || undefined}>
          <Input
            placeholder="Timestamp"
            value={input}
            onChange={(ev) => {
              setInput(ev.target.value);
              setError('');
            }}
            onKeyDown={(ev) => ev.key === 'Enter' && apply()}
            className={`pl-8 ${error ? 'border-destructive' : ''}`}
          />
          <Clock
            aria-hidden
            className={`absolute top-1/2 left-2.5 size-4  -translate-y-1/2 ${error ? 'text-destructive' : 'text-muted-foreground'}`}
          />
        </div>
      </div>
      <Button className="mt-3 w-full" onClick={apply}>
        Apply
      </Button>
    </div>
  );
}

export function JumpToTime({ disabled, onJump }: { disabled: boolean; onJump: (at: Date) => void }) {
  const [open, setOpen] = useState(false);
  const jump = (at: Date) => {
    setOpen(false);
    onJump(at);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={<Button variant="ghost" size="icon-sm" disabled={disabled} />}
        aria-label="Jump to a time"
      >
        <History aria-hidden />
      </PopoverTrigger>
      {/* Side by side where the window has room, stacked where it does not. */}
      <PopoverContent align="start" className="w-auto p-3">
        <div className="flex flex-col items-stretch gap-3 sm:flex-row">
          <RelativeTimeForm onApply={jump} />
          <AbsoluteTimeForm onApply={jump} />
        </div>
      </PopoverContent>
    </Popover>
  );
}
