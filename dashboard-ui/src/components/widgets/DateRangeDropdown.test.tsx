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

import { fireEvent, render, screen } from '@testing-library/react';

import { DateRangeDropdown, Duration, DurationUnit, parseTimestamp } from './DateRangeDropdown';

describe('parseTimestamp', () => {
  describe('empty / whitespace input', () => {
    it('should return undefined for empty string', () => {
      expect(parseTimestamp('')).toBeUndefined();
    });

    it('should return undefined for whitespace-only string', () => {
      expect(parseTimestamp('   ')).toBeUndefined();
    });
  });

  describe('JS timestamps', () => {
    it('should parse default JS timestamps without UTF offset', () => {
      const d = parseTimestamp('Jan 2, 2006 15:04:05');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('should interpret timezone-less input in the given timezone', () => {
      const d = parseTimestamp('Jan 2, 2006 15:04:05', 'America/New_York');
      // 15:04:05 EST = 20:04:05 UTC (January is EST, not EDT)
      expect(d).toEqual(new Date('2006-01-02T20:04:05Z'));
    });
  });

  describe('ISO 8601', () => {
    it('should parse full ISO 8601 with UTC offset', () => {
      const d = parseTimestamp('2006-01-02T15:04:05Z');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('should parse ISO 8601 with numeric timezone offset', () => {
      const d = parseTimestamp('2006-01-02T15:04:05+05:30');
      expect(d).toEqual(new Date('2006-01-02T15:04:05+05:30'));
    });

    it('should parse ISO 8601 with negative timezone offset', () => {
      const d = parseTimestamp('2006-01-02T15:04:05-07:00');
      expect(d).toEqual(new Date('2006-01-02T15:04:05-07:00'));
    });

    it('should parse ISO 8601 with milliseconds', () => {
      const d = parseTimestamp('2006-01-02T15:04:05.123Z');
      expect(d).toEqual(new Date('2006-01-02T15:04:05.123Z'));
    });

    it('should parse date-only ISO 8601 as UTC', () => {
      const d = parseTimestamp('2006-01-02');
      expect(d).toEqual(new Date('2006-01-02T00:00:00Z'));
    });

    it('should parse ISO 8601 without timezone as UTC', () => {
      const d = parseTimestamp('2006-01-02T15:04:05');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('should parse ISO 8601 with milliseconds and no timezone as UTC', () => {
      const d = parseTimestamp('2026-04-15T10:10:03.184');
      expect(d).toEqual(new Date('2026-04-15T10:10:03.184Z'));
    });
  });

  describe('RFC 2822', () => {
    it('should parse standard RFC 2822 format', () => {
      const d = parseTimestamp('Mon, 02 Jan 2006 15:04:05 -0700');
      expect(d).toEqual(new Date('2006-01-02T15:04:05-07:00'));
    });

    it('should parse RFC 2822 with positive offset', () => {
      const d = parseTimestamp('Tue, 03 Jan 2006 08:00:00 +0000');
      expect(d).toEqual(new Date('2006-01-03T08:00:00Z'));
    });
  });

  describe('Apache CLF', () => {
    it('should parse CLF with timezone offset', () => {
      const d = parseTimestamp('02/Jan/2006:15:04:05 -0700');
      expect(d).toEqual(new Date('2006-01-02T15:04:05-07:00'));
    });

    it('should parse CLF with positive timezone offset', () => {
      const d = parseTimestamp('02/Jan/2006:15:04:05 +0000');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('should parse CLF without timezone offset as UTC', () => {
      const d = parseTimestamp('02/Jan/2006:15:04:05');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('should parse CLF with numeric month', () => {
      const d = parseTimestamp('02/01/2006:15:04:05');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });
  });

  it('should parse Nginx ELF with timezone', () => {
    const d = parseTimestamp('2006/01/02 15:04:05');
    expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
  });

  describe('Unix timestamp (milliseconds)', () => {
    it('should parse a Unix timestamp in milliseconds', () => {
      const d = parseTimestamp('1136214245000');
      expect(d).toEqual(new Date(1136214245000));
    });

    it('should parse Unix epoch zero', () => {
      const d = parseTimestamp('0');
      expect(d).toEqual(new Date(0));
    });

    it('should parse a recent timestamp', () => {
      const ms = new Date('2025-06-15T12:00:00Z').getTime();
      const d = parseTimestamp(String(ms));
      expect(d).toEqual(new Date('2025-06-15T12:00:00Z'));
    });
  });

  describe('invalid input', () => {
    it('should return undefined for garbage text', () => {
      expect(parseTimestamp('not-a-date')).toBeUndefined();
    });

    it('should return undefined for partial timestamps', () => {
      expect(parseTimestamp('2006-13-45')).toBeUndefined();
    });
  });

  describe('whitespace handling', () => {
    it('should trim leading and trailing whitespace', () => {
      const d = parseTimestamp('  2006-01-02T15:04:05Z  ');
      expect(d).toEqual(new Date('2006-01-02T15:04:05Z'));
    });
  });
});

describe('Duration', () => {
  describe('toDate', () => {
    const from = new Date('2026-04-15T12:00:00Z');

    it('should subtract minutes', () => {
      const d = new Duration(30, DurationUnit.Minutes).toDate(from);
      expect(d).toEqual(new Date('2026-04-15T11:30:00Z'));
    });

    it('should subtract hours', () => {
      const d = new Duration(2, DurationUnit.Hours).toDate(from);
      expect(d).toEqual(new Date('2026-04-15T10:00:00Z'));
    });

    it('should subtract days', () => {
      const d = new Duration(3, DurationUnit.Days).toDate(from);
      expect(d).toEqual(new Date('2026-04-12T12:00:00Z'));
    });

    it('should subtract weeks', () => {
      const d = new Duration(1, DurationUnit.Weeks).toDate(from);
      expect(d).toEqual(new Date('2026-04-08T12:00:00Z'));
    });

    it('should subtract months', () => {
      const d = new Duration(2, DurationUnit.Months).toDate(from);
      expect(d).toEqual(new Date('2026-02-15T12:00:00Z'));
    });
  });

  describe('toISOString', () => {
    it('should format minutes', () => {
      expect(new Duration(5, DurationUnit.Minutes).toISOString()).toBe('PT5M');
    });

    it('should format hours', () => {
      expect(new Duration(2, DurationUnit.Hours).toISOString()).toBe('PT2H');
    });

    it('should format days', () => {
      expect(new Duration(7, DurationUnit.Days).toISOString()).toBe('P7D');
    });

    it('should format weeks', () => {
      expect(new Duration(1, DurationUnit.Weeks).toISOString()).toBe('P1W');
    });

    it('should format months', () => {
      expect(new Duration(3, DurationUnit.Months).toISOString()).toBe('P3M');
    });
  });
});

describe('DateRangeDropdown', () => {
  const renderDropdown = () => {
    const onChange = vi.fn();
    render(
      <DateRangeDropdown onChange={onChange}>
        <button type="button">Open</button>
      </DateRangeDropdown>,
    );
    fireEvent.click(screen.getByText('Open'));
    return onChange;
  };

  describe('relative time form', () => {
    it('should call onChange with Date when a preset is clicked', () => {
      const now = new Date('2026-04-15T12:00:00Z');
      vi.setSystemTime(now);
      const onChange = renderDropdown();
      fireEvent.click(screen.getByRole('button', { name: '5' }));
      expect(onChange).toHaveBeenCalledWith({
        since: new Date('2026-04-15T11:55:00Z'),
        until: null,
      });
      vi.useRealTimers();
    });

    it('should call onChange with custom value and unit', () => {
      const now = new Date('2026-04-15T12:00:00Z');
      vi.setSystemTime(now);
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Value');
      fireEvent.change(input, { target: { value: '10' } });
      fireEvent.click(screen.getAllByText('Apply')[0]);
      expect(onChange).toHaveBeenCalledWith({
        since: new Date('2026-04-15T11:50:00Z'),
        until: null,
      });
      vi.useRealTimers();
    });

    it('should not call onChange for invalid custom value', () => {
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Value');
      fireEvent.change(input, { target: { value: '0' } });
      fireEvent.click(screen.getAllByText('Apply')[0]);
      expect(onChange).not.toHaveBeenCalled();
    });

    it('should submit on Enter key in value input', () => {
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Value');
      fireEvent.change(input, { target: { value: '15' } });
      fireEvent.keyDown(input, { key: 'Enter' });
      expect(onChange).toHaveBeenCalledOnce();
    });
  });

  describe('absolute time form', () => {
    it('should call onChange with Date for valid timestamp', () => {
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: '2006-01-02T15:04:05Z' } });
      fireEvent.click(screen.getAllByText('Apply')[1]);
      expect(onChange).toHaveBeenCalledWith({
        since: new Date('2006-01-02T15:04:05Z'),
        until: null,
      });
    });

    it('should show error border for invalid timestamp', () => {
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: 'not-a-date' } });
      fireEvent.click(screen.getAllByText('Apply')[1]);
      expect(onChange).not.toHaveBeenCalled();
      expect(input).toHaveClass('border-destructive');
    });

    it('should clear error on new input', () => {
      renderDropdown();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: 'bad' } });
      fireEvent.click(screen.getAllByText('Apply')[1]);
      expect(input).toHaveClass('border-destructive');
      fireEvent.change(input, { target: { value: 'bad1' } });
      expect(input).not.toHaveClass('border-destructive');
    });

    it('should submit on Enter key in timestamp input', () => {
      const onChange = renderDropdown();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: '2006-01-02T15:04:05Z' } });
      fireEvent.keyDown(input, { key: 'Enter' });
      expect(onChange).toHaveBeenCalledOnce();
    });
  });
});
