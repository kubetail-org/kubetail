// Copyright 2024-2025 Andres Morey
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

import { parse, isValid } from 'date-fns';
import { formatInTimeZone, fromZonedTime } from 'date-fns-tz';
import { forwardRef, useImperativeHandle, useRef, useState } from 'react';

import { Button } from '@kubetail/ui/elements/button';
import { Calendar } from '@kubetail/ui/elements/calendar';
import { Popover, PopoverClose, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';

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

/*
 * Duration button component
 */

type DurationButtonProps = {
  value: string;
  unit: DurationUnit;
  setDurationValue: React.Dispatch<string>;
  setDurationUnit: React.Dispatch<DurationUnit>;
};

const DurationButton = ({ value, unit, setDurationValue, setDurationUnit }: DurationButtonProps) => (
  <Button
    variant="outline"
    size="sm"
    onClick={() => {
      setDurationValue(value);
      setDurationUnit(unit);
    }}
  >
    {value}
  </Button>
);

/**
 * Relative time picker component
 */

type RelativeTimePickerHandle = {
  reset: () => void;
  getValue: () => Duration | undefined;
};

const RelativeTimePicker = forwardRef<RelativeTimePickerHandle, unknown>((_, ref) => {
  const [durationValue, setDurationValue] = useState('5');
  const [durationUnit, setDurationUnit] = useState(DurationUnit.Minutes);
  const [errorMsg, setErrorMsg] = useState('');

  const validate = () => {
    if (durationValue.trim() === '') {
      setErrorMsg('Please choose a number');
      return undefined;
    }
    return new Duration(Number(durationValue), durationUnit);
  };

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setDurationValue('5');
      setDurationUnit(DurationUnit.Minutes);
    },
    getValue: validate,
  }));

  const buttonArgs = { setDurationValue, setDurationUnit };

  return (
    <>
      <div className="grid grid-cols-6 gap-2 text-sm pt-3 pl-3 pr-3">
        <div className="flex items-center">Minutes</div>
        {[5, 10, 15, 30, 45].map((val) => (
          <DurationButton key={val} value={val.toString()} unit={DurationUnit.Minutes} {...buttonArgs} />
        ))}
        <div className="flex items-center">Hours</div>
        {[1, 2, 3, 6, 12].map((val) => (
          <DurationButton key={val} value={val.toString()} unit={DurationUnit.Hours} {...buttonArgs} />
        ))}
        <div className="flex items-center">Days</div>
        {[1, 2, 3, 4, 5].map((val) => (
          <DurationButton key={val} value={val.toString()} unit={DurationUnit.Days} {...buttonArgs} />
        ))}
        <div className="flex items-center">Weeks</div>
        {[1, 2, 3, 4, 5].map((val) => (
          <DurationButton key={val} value={val.toString()} unit={DurationUnit.Weeks} {...buttonArgs} />
        ))}
      </div>
      <div className="grid grid-cols-2 w-full gap-5 mt-5">
        <div>
          {/* eslint-disable-next-line jsx-a11y/label-has-associated-control */}
          <label>
            Duration
            <input type="number" min="1" value={durationValue} onChange={(ev) => setDurationValue(ev.target.value)} />
          </label>
          {errorMsg && <div>{errorMsg}</div>}
        </div>
        <div>
          {/* eslint-disable-next-line jsx-a11y/label-has-associated-control */}
          <label>
            Unit
            <Select value={durationUnit} onValueChange={(value) => setDurationUnit(value as DurationUnit)}>
              <SelectTrigger className="mt-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={DurationUnit.Minutes}>Minutes ago</SelectItem>
                <SelectItem value={DurationUnit.Hours}>Hours ago</SelectItem>
                <SelectItem value={DurationUnit.Days}>Days ago</SelectItem>
                <SelectItem value={DurationUnit.Weeks}>Weeks ago</SelectItem>
                <SelectItem value={DurationUnit.Months}>Months ago</SelectItem>
              </SelectContent>
            </Select>
          </label>
        </div>
      </div>
    </>
  );
});

/**
 * Absolute time picker component
 */

type AbsoluteTimePickerHandle = {
  reset: () => void;
  getValue: () => Date | undefined;
};

const AbsoluteTimePicker = forwardRef<AbsoluteTimePickerHandle, unknown>((_, ref) => {
  const today = new Date();
  const dateFmt = Intl.DateTimeFormat().resolvedOptions().locale === 'en-US' ? 'MM/dd/yyyy' : 'dd/MM/yyyy';

  const [calendarDate, setCalendarDate] = useState<Date | undefined>();

  const [manualStartDate, setManualStartDate] = useState(formatInTimeZone(today, 'UTC', dateFmt));
  const [manualStartTime, setManualStartTime] = useState('00:00:00');

  const [errorMsgs, setErrorMsgs] = useState(new Map<string, string>());

  const validate = () => {
    if (!isValid(parse(manualStartDate, dateFmt, new Date()))) errorMsgs.set('startDate', dateFmt);
    else errorMsgs.delete('startDate');

    if (!isValid(parse(manualStartTime, 'HH:mm:ss', new Date()))) errorMsgs.set('startTime', 'HH:mm:ss');
    else errorMsgs.delete('startTime');

    setErrorMsgs(new Map(errorMsgs));

    // return undefined if validation failed
    if (errorMsgs.size) return undefined;

    // parse
    const localDate = parse(`${manualStartDate} ${manualStartTime}`, `${dateFmt} HH:mm:ss`, new Date());

    // return as UTC time
    return fromZonedTime(localDate, 'UTC');
  };

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setCalendarDate(today);
      setManualStartDate(formatInTimeZone(today, 'UTC', dateFmt));
      setManualStartTime('00:00:00');
      setErrorMsgs(new Map<string, string>());
    },
    getValue: validate,
  }));

  const handleCalendarSelect = (value: Date | undefined) => {
    if (!value) return;
    setCalendarDate(value);
    setManualStartDate(formatInTimeZone(value, 'UTC', dateFmt));
    setManualStartTime('00:00:00');
    setErrorMsgs(new Map<string, string>());
  };

  return (
    <div className="flex flex-col items-center">
      <Calendar
        autoFocus
        mode="single"
        disabled={{ after: today }}
        defaultMonth={today}
        selected={calendarDate}
        onSelect={handleCalendarSelect}
        numberOfMonths={1}
        timeZone="UTC"
      />
      <div className="flex space-x-4 mt-1">
        <div>
          {/* eslint-disable-next-line jsx-a11y/label-has-associated-control */}
          <label>
            Start date
            <input
              className="w-[110px]"
              value={manualStartDate}
              onChange={(ev) => setManualStartDate(ev.target.value)}
            />
          </label>
          {errorMsgs.has('startDate') && <div>{errorMsgs.get('startDate')}</div>}
        </div>
        <div>
          {/* eslint-disable-next-line jsx-a11y/label-has-associated-control */}
          <label>
            Start time
            <input
              className="w-[110px]"
              value={manualStartTime}
              onChange={(ev) => setManualStartTime(ev.target.value)}
            />
          </label>
          {errorMsgs.has('startTime') && <div>{errorMsgs.get('startTime')}</div>}
        </div>
      </div>
    </div>
  );
});

/**
 * DateRangeDropdown component
 */

export type DateRangeDropdownOnChangeArgs = {
  since: Date | Duration | null;
  until: Date | null;
};

type DateRangeDropdownProps = {
  onChange: (args: DateRangeDropdownOnChangeArgs) => void;
};

export const DateRangeDropdown = ({ onChange, children }: React.PropsWithChildren<DateRangeDropdownProps>) => {
  const [tabValue, setTabValue] = useState('relative');

  const cancelButtonRef = useRef<HTMLButtonElement>(null);
  const relativePickerRef = useRef<RelativeTimePickerHandle>(null);
  const absolutePickerRef = useRef<AbsoluteTimePickerHandle>(null);

  const closePopover = () => {
    cancelButtonRef.current?.click();
  };

  const handleClear = () => {
    if (tabValue === 'relative') relativePickerRef.current?.reset();
    else if (tabValue === 'absolute') absolutePickerRef.current?.reset();
  };

  const handleApply = () => {
    const args: DateRangeDropdownOnChangeArgs = { since: null, until: null };

    if (tabValue === 'relative') {
      const val = relativePickerRef.current?.getValue();
      if (!val) return;
      args.since = val;
    } else {
      const val = absolutePickerRef.current?.getValue();
      if (!val) return;
      args.since = val;
    }

    // close popover and call onChange handler
    closePopover();
    onChange(args);
  };

  return (
    <Popover>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent className="w-auto p-0 bg-background" align="start">
        <Tabs className="w-[400px] p-3" defaultValue={tabValue} onValueChange={(value) => setTabValue(value)}>
          <TabsList className="grid w-full grid-cols-2">
            <TabsTrigger value="relative">Relative</TabsTrigger>
            <TabsTrigger value="absolute">Absolute</TabsTrigger>
          </TabsList>
          <TabsContent value="relative">
            <RelativeTimePicker ref={relativePickerRef} />
          </TabsContent>
          <TabsContent value="absolute">
            <AbsoluteTimePicker ref={absolutePickerRef} />
          </TabsContent>
        </Tabs>
        <div className="flex justify-between mt-4 p-3 border-t">
          <Button variant="outline" size="sm" onClick={handleClear}>
            Clear
          </Button>
          <div className="flex space-x-2">
            <PopoverClose asChild>
              <Button ref={cancelButtonRef} variant="ghost" size="sm">
                Cancel
              </Button>
            </PopoverClose>
            <Button size="sm" onClick={handleApply}>
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};
