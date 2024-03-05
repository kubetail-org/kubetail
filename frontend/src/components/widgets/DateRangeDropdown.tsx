// Copyright 2024 Andres Morey
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

import { addMonths, parse, isValid } from 'date-fns';
import { format } from 'date-fns-tz';
import { forwardRef, useImperativeHandle, useRef, useState } from 'react';
import { DateRange } from 'react-day-picker';

import Button from 'kubetail-ui/elements/Button';
import { Calendar } from 'kubetail-ui/elements/Calendar';
import Form from 'kubetail-ui/elements/Form';
import { Popover, PopoverClose, PopoverTrigger, PopoverContent } from 'kubetail-ui/elements/Popover';
import { Tabs, TabsContent, TabsList, TabsTrigger } from 'kubetail-ui/elements/Tabs';

/**
 * Shared types
 */

export enum DurationUnit {
  Minutes = 'minutes',
  Hours = 'hours',
  Days = 'days',
  Weeks = 'weeks',
  Months = 'moths',
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
    }
  }
}

/**
 * Relative time picker component
 */

type RelativeTimePickerHandle = {
  reset: () => void;
  getValue: () => Duration | undefined;
};

const RelativeTimePicker = forwardRef<RelativeTimePickerHandle, {}>((_, ref) => {
  const [durationValue, setDurationValue] = useState('5');
  const [durationUnit, setDurationUnit] = useState(DurationUnit.Minutes);
  const [errorMsg, setErrorMsg] = useState('');

  const validate = () => {
    if (durationValue.trim() === '') {
      setErrorMsg('Please choose a number');
      return undefined;
    }
    return new Duration(Number(durationValue), durationUnit);
  }

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setDurationValue('5');
      setDurationUnit(DurationUnit.Minutes);
    },
    getValue: validate,
  }));

  const DurationButton = ({ value, unit }: { value: number; unit: DurationUnit }) => (
    <Button
      intent="outline"
      size="xs"
      onClick={() => {
        setDurationValue(value.toString());
        setDurationUnit(unit);
      }}
    >
      {value}
    </Button>
  );

  return (
    <>
      <div className="grid grid-cols-6 gap-2 text-sm pt-3 pl-3 pr-3">
        <div className="flex items-center">Minutes</div>
        {[5, 10, 15, 30, 45].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Minutes} />))}
        <div className="flex items-center">Hours</div>
        {[1, 2, 3, 6, 12].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Hours} />))}
        <div className="flex items-center">Days</div>
        {[1, 2, 3, 4, 5].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Days} />))}
        <div className="flex items-center">Weeks</div>
        {[1, 2, 3, 4, 5].map(val => (<DurationButton key={val} value={val} unit={DurationUnit.Weeks} />))}
      </div>
      <div className="grid grid-cols-2 w-full gap-5 mt-5">
        <div>
          <Form.Label>Duration</Form.Label>
          <Form.Control
            type="number"
            min="1"
            value={durationValue}
            onChange={ev => setDurationValue(ev.target.value)}
          />
          {errorMsg && <Form.Control.Feedback>{errorMsg}</Form.Control.Feedback>}
        </div>
        <div>
          <Form.Label>Unit of time</Form.Label>
          <Form.Select
            className="mt-0"
            value={durationUnit}
            onChange={ev => setDurationUnit(ev.target.value as DurationUnit)}
          >
            <Form.Option value={DurationUnit.Minutes}>Minutes ago</Form.Option>
            <Form.Option value={DurationUnit.Hours}>Hours ago</Form.Option>
            <Form.Option value={DurationUnit.Days}>Days ago</Form.Option>
            <Form.Option value={DurationUnit.Weeks}>Weeks ago</Form.Option>
            <Form.Option value={DurationUnit.Months}>Months ago</Form.Option>
          </Form.Select>
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
  getValue: () => DateRange | undefined;
};

const AbsoluteTimePicker = forwardRef<AbsoluteTimePickerHandle, {}>((_, ref) => {
  const today = new Date;
  const dateFmt = Intl.DateTimeFormat().resolvedOptions().locale === 'en-US' ? 'MM/dd/yyyy' : 'dd/MM/yyyy';

  const [calendarDateRange, setCalendarDateRange] = useState<DateRange | undefined>({ from: today, to: today });

  const [manualStartDate, setManualStartDate] = useState(format(today, dateFmt));
  const [manualStartTime, setManualStartTime] = useState('00:00:00');

  const [manualEndDate, setManualEndDate] = useState(format(today, dateFmt));
  const [manualEndTime, setManualEndTime] = useState('23:59:59');

  const [errorMsgs, setErrorMsgs] = useState(new Map<string, string>());

  const validate = () => {
    if (!isValid(parse(manualStartDate, dateFmt, new Date()))) errorMsgs.set('startDate', dateFmt)
    else errorMsgs.delete('startDate');

    if (!isValid(parse(manualStartTime, 'HH:mm:ss', new Date()))) errorMsgs.set('startTime', 'HH:mm:ss')
    else errorMsgs.delete('startTime');

    if (!isValid(parse(manualEndDate, dateFmt, new Date()))) errorMsgs.set('endDate', dateFmt)
    else errorMsgs.delete('endDate');

    if (!isValid(parse(manualEndTime, 'HH:mm:ss', new Date()))) errorMsgs.set('endTime', 'HH:mm:ss')
    else errorMsgs.delete('endTime');

    setErrorMsgs(new Map(errorMsgs));

    // return undefined if validation failed
    if (errorMsgs.size) return undefined;

    // return parsed DateRange
    return {
      from: parse(`${manualStartDate} ${manualStartTime}`, `${dateFmt} HH:mm:ss`, new Date()),
      to: parse(`${manualEndDate} ${manualEndTime}`, `${dateFmt} HH:mm:ss`, new Date()),
    };
  }

  // define handler api
  useImperativeHandle(ref, () => ({
    reset: () => {
      setCalendarDateRange({ from: today, to: today });
      setManualStartDate(format(today, dateFmt));
      setManualStartTime('00:00:00');
      setManualEndDate(format(today, dateFmt));
      setManualEndTime('23:59:59');
      setErrorMsgs(new Map<string, string>());
    },
    getValue: validate
  }));

  const handleCalendarSelect = (value: DateRange | undefined) => {
    if (!value) return;
    setCalendarDateRange(value);
    if (value.from) {
      setManualStartDate(format(value.from, dateFmt))
      setManualStartTime('00:00:00');
    }
    if (value.to) {
      setManualEndDate(format(value.to, dateFmt))
      setManualEndTime('23:59:59');
    }
    setErrorMsgs(new Map<string, string>());
  }

  return (
    <>
      <Calendar
        initialFocus
        mode="range"
        disabled={{ after: today }}
        defaultMonth={today && addMonths(today, -1)}
        selected={calendarDateRange}
        onSelect={handleCalendarSelect}
        numberOfMonths={2}
      />
      <div className="mt-1 flex px-3 justify-between">
        <div className="flex space-x-4">
          <div>
            <Form.Label>Start date</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualStartDate}
              onChange={ev => setManualStartDate(ev.target.value)}
            />
            {errorMsgs.has('startDate') && <Form.Control.Feedback>{errorMsgs.get('startDate')}</Form.Control.Feedback>}
          </div>
          <div>
            <Form.Label>Start time</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualStartTime}
              onChange={ev => setManualStartTime(ev.target.value)}
            />
            {errorMsgs.has('startTime') && <Form.Control.Feedback>{errorMsgs.get('startTime')}</Form.Control.Feedback>}
          </div>
        </div>
        <div className="flex space-x-4">
          <div>
            <Form.Label>End date</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualEndDate}
              onChange={ev => setManualEndDate(ev.target.value)}
            />
            {errorMsgs.has('endDate') && <Form.Control.Feedback>{errorMsgs.get('endDate')}</Form.Control.Feedback>}
          </div>
          <div>
            <Form.Label>End time</Form.Label>
            <Form.Control
              className="w-[100px]"
              value={manualEndTime}
              onChange={ev => setManualEndTime(ev.target.value)}
            />
            {errorMsgs.has('endTime') && <Form.Control.Feedback>{errorMsgs.get('endTime')}</Form.Control.Feedback>}
          </div>
        </div>
      </div>
    </>
  );
});

/**
 * Date range dropdown component
 */

export type DateRangeDropdownOnChangeArgs = {
  since: Date | Duration | null;
  until: Date | null;
}

interface DateRangeDropdownProps extends React.PropsWithChildren {
  onChange: (args: DateRangeDropdownOnChangeArgs) => void;
}

export const DateRangeDropdown = ({ children, onChange }: DateRangeDropdownProps) => {
  const [tabValue, setTabValue] = useState('relative');

  const cancelButtonRef = useRef<HTMLButtonElement>();
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
      if (val.from) args.since = val.from;
      if (val.to) args.until = val.to;
    }

    // close popover and call onChange handler
    closePopover();
    onChange(args);
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        {children}
      </PopoverTrigger>
      <PopoverContent
        className="w-auto p-0 bg-background"
        align="center"
      >
        <Tabs
          className="w-[565px] p-3"
          defaultValue={tabValue}
          onValueChange={(value) => setTabValue(value)}
        >
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
          <Button intent="outline" size="sm" onClick={handleClear}>Clear</Button>
          <div className="flex space-x-2">
            <PopoverClose asChild>
              <Button ref={cancelButtonRef} intent="ghost" size="sm">Cancel</Button>
            </PopoverClose>
            <Button intent="primary" size="sm" onClick={handleApply}>Apply</Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
};
