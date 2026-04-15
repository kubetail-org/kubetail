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

import { parse as parseDate } from 'date-fns';
import { fromZonedTime } from 'date-fns-tz';
import { Clock } from 'lucide-react';
import { Fragment, useRef, useState } from 'react';

import { Alert, AlertDescription, AlertTitle } from '@kubetail/ui/elements/alert';
import { Button } from '@kubetail/ui/elements/button';
import { Input } from '@kubetail/ui/elements/input';
import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

/**
 * Shared types
 */

export enum DurationUnit {
  Minutes = 'minutes',
  Hours = 'hours',
  Days = 'days',
  Weeks = 'weeks',
  Months = 'months',
}

export class Duration {
  value: number;

  unit: DurationUnit;

  constructor(value: number, unit: DurationUnit) {
    this.value = value;
    this.unit = unit;
  }

  toDate(from?: Date) {
    const d = new Date(from ?? Date.now());
    switch (this.unit) {
      case DurationUnit.Minutes:
        d.setMinutes(d.getMinutes() - this.value);
        break;
      case DurationUnit.Hours:
        d.setHours(d.getHours() - this.value);
        break;
      case DurationUnit.Days:
        d.setDate(d.getDate() - this.value);
        break;
      case DurationUnit.Weeks:
        d.setDate(d.getDate() - this.value * 7);
        break;
      case DurationUnit.Months:
        d.setMonth(d.getMonth() - this.value);
        break;
      default:
        throw new Error('not implemented');
    }
    return d;
  }

  toISOString() {
    switch (this.unit) {
      case DurationUnit.Minutes:
        return `PT${this.value}M`;
      case DurationUnit.Hours:
        return `PT${this.value}H`;
      case DurationUnit.Days:
        return `P${this.value}D`;
      case DurationUnit.Weeks:
        return `P${this.value}W`;
      case DurationUnit.Months:
        return `P${this.value}M`;
      default:
        throw new Error('not implemented');
    }
  }
}

/**
 * Relative time form
 */

const presetGroups: { label: string; unit: DurationUnit; values: number[] }[] = [
  { label: 'Minutes', unit: DurationUnit.Minutes, values: [5, 15, 30] },
  { label: 'Hours', unit: DurationUnit.Hours, values: [1, 2, 6] },
  { label: 'Days', unit: DurationUnit.Days, values: [1, 3, 7] },
];

const RelativeTimeForm = ({ onApply }: { onApply: (duration: Duration) => void }) => {
  const [customValue, setCustomValue] = useState('');
  const [customUnit, setCustomUnit] = useState(DurationUnit.Minutes);
  const [error, setError] = useState('');

  const handleApply = () => {
    const num = Number(customValue);
    if (Number.isNaN(num) || num <= 0) {
      setError('Enter a positive number');
      return;
    }
    setError('');
    onApply(new Duration(num, customUnit));
  };

  return (
    <div className="flex flex-col flex-1">
      <div className="border border-border rounded-md p-3 bg-muted flex-1 flex flex-col">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">Relative Time</h3>
        <div className="grid grid-cols-[auto_1fr_1fr_1fr] items-center gap-x-2 gap-y-1 my-auto">
          {presetGroups.map((group) => (
            <Fragment key={group.label}>
              <span className="text-xs text-muted-foreground pl-3">{group.label}</span>
              {group.values.map((v) => (
                <Button
                  key={v}
                  variant="ghost"
                  size="sm"
                  className="text-right"
                  onClick={() => onApply(new Duration(v, group.unit))}
                >
                  {v}
                </Button>
              ))}
            </Fragment>
          ))}
        </div>
        <div className="flex gap-2 mt-auto" title={error || undefined}>
          <Input
            type="number"
            min="1"
            value={customValue}
            placeholder="Value"
            onChange={(ev) => {
              setCustomValue(ev.target.value);
              setError('');
            }}
            onKeyDown={(ev) => ev.key === 'Enter' && handleApply()}
            className={`w-25 shrink-0 ${error ? 'border-destructive' : ''}`}
          />
          <Select value={customUnit} onValueChange={(value) => setCustomUnit(value as DurationUnit)}>
            <SelectTrigger className="flex-1 w-30 px-2 gap-1">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={DurationUnit.Minutes}>Minutes ago</SelectItem>
              <SelectItem value={DurationUnit.Hours}>Hours ago</SelectItem>
              <SelectItem value={DurationUnit.Days}>Days ago</SelectItem>
              <SelectItem value={DurationUnit.Weeks}>Weeks ago</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>
      <Button className="mt-3 w-full" onClick={handleApply}>
        Apply
      </Button>
    </div>
  );
};

/**
 * Absolute time form
 */

function isValidDate(d: Date): boolean {
  return !Number.isNaN(d.getTime());
}

// Matches Z, ±HH:MM, ±HHMM, or ±HH at the end of a string
const TZ_RE = /(?:Z|[+-]\d{2}(?::?\d{2})?)$/i;

export function parseTimestamp(input: string): Date | undefined {
  const trimmed = input.trim();
  if (!trimmed) return undefined;

  // Unix timestamp (milliseconds)
  if (/^\d+$/.test(trimmed)) {
    const ms = Number(trimmed);
    const d = new Date(ms);
    if (isValidDate(d)) return d;
  }

  // Try native Date parser (handles ISO 8601, RFC 2822, and common formats).
  // If the input has no explicit timezone, use fromZonedTime to interpret as UTC.
  const hasTZ = TZ_RE.test(trimmed);
  if (hasTZ) {
    const nativeDate = new Date(trimmed);
    if (isValidDate(nativeDate)) return nativeDate;
  } else {
    const nativeDate = fromZonedTime(trimmed, 'UTC');
    if (isValidDate(nativeDate)) return nativeDate;
  }

  // RFC 2822 fallback (e.g. "Mon, 02 Jan 2006 15:04:05 -0700")
  const rfc2822Date = parseDate(trimmed, 'EEE, dd MMM yyyy HH:mm:ss xx', new Date());
  if (isValidDate(rfc2822Date)) return rfc2822Date;

  // Apache CLF with timezone (e.g. "02/Jan/2006:15:04:05 -0700")
  const clfTzDate = parseDate(trimmed, 'dd/MMM/yyyy:HH:mm:ss xx', new Date());
  if (isValidDate(clfTzDate)) return clfTzDate;

  // Apache CLF without timezone — assume UTC (e.g. "02/Jan/2006:15:04:05")
  const clfDate = parseDate(`${trimmed} +0000`, 'dd/MMM/yyyy:HH:mm:ss xx', new Date());
  if (isValidDate(clfDate)) return clfDate;

  return undefined;
}

const AbsoluteTimeForm = ({ onApply }: { onApply: (date: Date) => void }) => {
  const [input, setInput] = useState('');
  const [error, setError] = useState('');

  const handleApply = () => {
    const date = parseTimestamp(input);
    if (!date) {
      setError('Unable to parse timestamp');
      return;
    }
    setError('');
    onApply(date);
  };

  return (
    <div className="flex flex-col flex-1">
      <div className="border border-border rounded-md p-3 bg-muted flex-1 flex flex-col">
        <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">Absolute Time</h3>
        <Alert className="mb-3 text-xs">
          <AlertTitle className="flex items-center">Supported Formats</AlertTitle>
          <AlertDescription className="font-mono text-xs mt-1">
            <ul className="space-y-1 whitespace-nowrap">
              <li title="ISO 8601">&gt; 2006-01-02T15:04:05+07:00</li>
              <li title="RFC 2822">&gt; Mon, 02 Jan 2006 15:04:05 -0700</li>
              <li title="Apache CLF">&gt; 02/Jan/2006:15:04:05 -0700</li>
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
            onKeyDown={(ev) => ev.key === 'Enter' && handleApply()}
            className={`pl-8 ${error ? 'border-destructive' : ''}`}
          />
          <Clock
            className={`absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 ${error ? 'text-destructive' : 'text-muted-foreground'}`}
          />
        </div>
      </div>
      <Button className="mt-3 w-full" onClick={handleApply}>
        Apply
      </Button>
    </div>
  );
};

/**
 * DateRangeDropdown component
 */

export type DateRangeDropdownOnChangeArgs = {
  since: Date | null;
  until: Date | null;
};

type DateRangeDropdownProps = {
  onChange: (args: DateRangeDropdownOnChangeArgs) => void;
};

export const DateRangeDropdown = ({ onChange, children }: React.PropsWithChildren<DateRangeDropdownProps>) => {
  const closeRef = useRef<HTMLButtonElement>(null);

  const handleApply = (date: Date) => {
    closeRef.current?.click();
    onChange({ since: date, until: null });
  };

  const handleRelativeApply = (duration: Duration) => {
    handleApply(duration.toDate());
  };

  return (
    <Popover>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-auto p-3" align="start">
        <div className="flex items-stretch gap-3">
          <RelativeTimeForm onApply={handleRelativeApply} />
          <AbsoluteTimeForm onApply={handleApply} />
        </div>
        <PopoverClose ref={closeRef} className="hidden" />
      </PopoverContent>
    </Popover>
  );
};
