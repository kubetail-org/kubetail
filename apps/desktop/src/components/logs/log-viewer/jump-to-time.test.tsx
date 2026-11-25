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
import userEvent from '@testing-library/user-event';
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';

import { expectCodeText } from '@/test-utils';

import { JumpToTime, ago, parseTimestamp } from './jump-to-time';

// A zone away from UTC, so a timestamp read as local time and one read as UTC differ. Serial:
// the zone is the process's.
beforeAll(() => {
  vi.stubEnv('TZ', 'America/New_York');
});
afterAll(() => {
  vi.unstubAllEnvs();
});

describe('parseTimestamp', () => {
  it('reads nothing from an empty or blank string', () => {
    expect(parseTimestamp('')).toBeUndefined();
    expect(parseTimestamp('   ')).toBeUndefined();
  });

  it('reads a timestamp with no offset as local time', () => {
    expect(parseTimestamp('Jan 2, 2006 15:04:05')).toEqual(new Date('2006-01-02T15:04:05-05:00'));
    expect(parseTimestamp('2006-01-02T15:04:05')).toEqual(new Date('2006-01-02T15:04:05-05:00'));
    expect(parseTimestamp('2026-04-15T10:10:03.184')).toEqual(new Date('2026-04-15T10:10:03.184-04:00'));
  });

  it('reads a date alone as local midnight', () => {
    expect(parseTimestamp('2006-01-02')).toEqual(new Date('2006-01-02T00:00:00-05:00'));
  });

  describe('ISO 8601', () => {
    it('reads UTC', () => {
      expect(parseTimestamp('2006-01-02T15:04:05Z')).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('reads an offset either side of UTC', () => {
      expect(parseTimestamp('2006-01-02T15:04:05+05:30')).toEqual(new Date('2006-01-02T09:34:05Z'));
      expect(parseTimestamp('2006-01-02T15:04:05-07:00')).toEqual(new Date('2006-01-02T22:04:05Z'));
    });

    it('reads milliseconds', () => {
      expect(parseTimestamp('2006-01-02T15:04:05.123Z')).toEqual(new Date('2006-01-02T15:04:05.123Z'));
    });
  });

  describe('RFC 3339 with a space', () => {
    it('reads an offset', () => {
      expect(parseTimestamp('2026-04-23 14:35:22.123+00:00')).toEqual(new Date('2026-04-23T14:35:22.123Z'));
      expect(parseTimestamp('2026-04-23 10:35:22.123-04:00')).toEqual(new Date('2026-04-23T14:35:22.123Z'));
    });

    it('reads no offset as local time', () => {
      expect(parseTimestamp('2026-04-23 10:35:22')).toEqual(new Date('2026-04-23T10:35:22-04:00'));
    });
  });

  describe('RFC 2822 / RFC 1123', () => {
    it('reads its offset', () => {
      expect(parseTimestamp('Mon, 02 Jan 2006 15:04:05 -0700')).toEqual(new Date('2006-01-02T22:04:05Z'));
      expect(parseTimestamp('Thu, 23 Apr 2026 14:35:22 +0000')).toEqual(new Date('2026-04-23T14:35:22Z'));
    });
  });

  describe('Apache CLF', () => {
    it('reads its offset', () => {
      expect(parseTimestamp('02/Jan/2006:15:04:05 -0700')).toEqual(new Date('2006-01-02T22:04:05Z'));
      expect(parseTimestamp('02/Jan/2006:15:04:05 +0000')).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('reads no offset as local time', () => {
      expect(parseTimestamp('02/Jan/2006:15:04:05')).toEqual(new Date('2006-01-02T15:04:05-05:00'));
    });

    it('reads a numeric month', () => {
      expect(parseTimestamp('02/01/2006:15:04:05 +0000')).toEqual(new Date('2006-01-02T15:04:05Z'));
    });

    it('refuses a day the month does not have', () => {
      expect(parseTimestamp('31/Feb/2006:15:04:05 +0000')).toBeUndefined();
    });
  });

  it('reads Nginx’s error log time as local time', () => {
    expect(parseTimestamp('2006/01/02 15:04:05')).toEqual(new Date('2006-01-02T15:04:05-05:00'));
  });

  describe('Unix time', () => {
    it('reads 13 digits or more as milliseconds', () => {
      expect(parseTimestamp('1136214245000')).toEqual(new Date(1136214245000));
    });

    it('reads fewer as seconds', () => {
      expect(parseTimestamp('1136214245')).toEqual(new Date('2006-01-02T15:04:05Z'));
      expect(parseTimestamp('0')).toEqual(new Date(0));
    });
  });

  it('refuses what is not a time', () => {
    expect(parseTimestamp('not-a-date')).toBeUndefined();
    expect(parseTimestamp('2006-13-45')).toBeUndefined();
  });

  it('trims what is around it', () => {
    expect(parseTimestamp('  2006-01-02T15:04:05Z  ')).toEqual(new Date('2006-01-02T15:04:05Z'));
  });
});

describe('ago', () => {
  const from = new Date('2026-04-15T12:00:00Z');

  it('steps back in each unit', () => {
    expect(ago(30, 'minutes', from)).toEqual(new Date('2026-04-15T11:30:00Z'));
    expect(ago(2, 'hours', from)).toEqual(new Date('2026-04-15T10:00:00Z'));
    expect(ago(3, 'days', from)).toEqual(new Date('2026-04-12T12:00:00Z'));
    expect(ago(1, 'weeks', from)).toEqual(new Date('2026-04-08T12:00:00Z'));
  });

  it('keeps the wall clock across a change of offset', () => {
    // New York moved to summer time on 8 March 2026.
    expect(ago(1, 'weeks', new Date('2026-03-10T12:00:00-04:00'))).toEqual(new Date('2026-03-03T12:00:00-05:00'));
  });
});

describe('JumpToTime', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  function open() {
    const onJump = vi.fn();
    render(<JumpToTime disabled={false} onJump={onJump} />);
    fireEvent.click(screen.getByRole('button', { name: 'Jump to a time' }));
    return onJump;
  }

  const apply = (form: 0 | 1) => fireEvent.click(screen.getAllByRole('button', { name: 'Apply' })[form]);

  it("turns off the webview's suggestions in both fields", () => {
    open();
    expectCodeText(screen.getByPlaceholderText('Value'));
    expectCodeText(screen.getByPlaceholderText('Timestamp'));
  });

  describe('relative time', () => {
    it('jumps to a preset and closes', () => {
      vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-04-15T12:00:00Z') });
      const onJump = open();
      fireEvent.click(screen.getByRole('button', { name: '5' }));
      expect(onJump).toHaveBeenCalledWith(new Date('2026-04-15T11:55:00Z'));
      expect(screen.queryByPlaceholderText('Value')).not.toBeInTheDocument();
    });

    it('jumps to a value typed in minutes', () => {
      vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-04-15T12:00:00Z') });
      const onJump = open();
      fireEvent.change(screen.getByPlaceholderText('Value'), { target: { value: '10' } });
      apply(0);
      expect(onJump).toHaveBeenCalledWith(new Date('2026-04-15T11:50:00Z'));
    });

    it('jumps to a value typed in another unit', async () => {
      vi.useFakeTimers({ toFake: ['Date'], now: new Date('2026-04-15T12:00:00Z') });
      const onJump = open();
      fireEvent.change(screen.getByPlaceholderText('Value'), { target: { value: '2' } });
      const user = userEvent.setup();
      await user.click(screen.getByRole('combobox'));
      await user.click(await screen.findByRole('option', { name: 'Hours ago' }));
      apply(0);
      expect(onJump).toHaveBeenCalledWith(new Date('2026-04-15T10:00:00Z'));
    });

    it('refuses a value that is not positive', () => {
      const onJump = open();
      const input = screen.getByPlaceholderText('Value');
      fireEvent.change(input, { target: { value: '0' } });
      apply(0);
      expect(onJump).not.toHaveBeenCalled();
      expect(input).toHaveClass('border-destructive');
    });

    it('applies on Enter', () => {
      const onJump = open();
      const input = screen.getByPlaceholderText('Value');
      fireEvent.change(input, { target: { value: '15' } });
      fireEvent.keyDown(input, { key: 'Enter' });
      expect(onJump).toHaveBeenCalledOnce();
    });
  });

  describe('absolute time', () => {
    it('jumps to a timestamp it reads', () => {
      const onJump = open();
      fireEvent.change(screen.getByPlaceholderText('Timestamp'), { target: { value: '2006-01-02T15:04:05Z' } });
      apply(1);
      expect(onJump).toHaveBeenCalledWith(new Date('2006-01-02T15:04:05Z'));
    });

    it('marks a timestamp it cannot read until the next keystroke', () => {
      const onJump = open();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: 'bad' } });
      apply(1);
      expect(onJump).not.toHaveBeenCalled();
      expect(input).toHaveClass('border-destructive');
      fireEvent.change(input, { target: { value: 'bad!' } });
      expect(input).not.toHaveClass('border-destructive');
    });

    it('applies on Enter', () => {
      const onJump = open();
      const input = screen.getByPlaceholderText('Timestamp');
      fireEvent.change(input, { target: { value: '2006-01-02T15:04:05Z' } });
      fireEvent.keyDown(input, { key: 'Enter' });
      expect(onJump).toHaveBeenCalledOnce();
    });
  });

  it('is disabled while loading', () => {
    render(<JumpToTime disabled onJump={vi.fn()} />);
    expect(screen.getByRole('button', { name: 'Jump to a time' })).toBeDisabled();
  });
});
