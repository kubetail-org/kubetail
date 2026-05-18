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

import { useMemo, useState } from 'react';

import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/popover';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';

import { useTheme, Theme } from '@/lib/theme';
import { TIMESTAMP_FORMAT_OPTIONS, useTimestampFormat } from '@/lib/timestamp-format';
import { formatTimezoneOffset, TIMEZONES, useTimezone } from '@/lib/timezone';

const SettingsPopoverContent = () => {
  const { theme, setTheme } = useTheme();
  const [timezone, setTimezone] = useTimezone();
  const [timestampFormat, setTimestampFormat] = useTimestampFormat();

  const handleThemeChange = (value: Theme | null) => {
    setTheme(value as Theme);
  };

  const handleTimezoneChange = (value: string | null) => {
    if (value !== null) setTimezone(value);
  };

  const handleTimestampFormatChange = (value: string | null) => {
    if (value !== null) setTimestampFormat(value);
  };

  const offsets = useMemo(() => new Map(TIMEZONES.map((tz) => [tz, formatTimezoneOffset(tz)])), []);

  return (
    <PopoverContent side="top" className="mr-1 min-h-30">
      <table className="w-full border-separate border-spacing-y-2 text-sm">
        <tbody>
          <tr>
            <td>Theme</td>
            <td align="right">
              <Select value={theme} onValueChange={handleThemeChange}>
                <SelectTrigger className="bg-secondary border-0">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false} className="bg-secondary">
                  <SelectGroup>
                    <SelectItem value={Theme.System}>System</SelectItem>
                    <SelectItem value={Theme.Dark}>Dark</SelectItem>
                    <SelectItem value={Theme.Light}>Light</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </td>
          </tr>
          <tr>
            <td>Timezone</td>
            <td align="right">
              <Select value={timezone} onValueChange={handleTimezoneChange}>
                <SelectTrigger className="bg-secondary border-0">
                  <SelectValue>{(value) => value}</SelectValue>
                </SelectTrigger>
                <SelectContent
                  alignItemWithTrigger={false}
                  className="bg-secondary max-h-60 w-auto min-w-(--anchor-width)"
                >
                  <SelectGroup>
                    {TIMEZONES.map((tz) => (
                      <SelectItem key={tz} value={tz}>
                        {tz}
                        <span className="text-muted-foreground ml-auto">{offsets.get(tz)}</span>
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </td>
          </tr>
          <tr>
            <td>Timestamps</td>
            <td align="right">
              <Select value={timestampFormat} onValueChange={handleTimestampFormatChange}>
                <SelectTrigger className="bg-secondary border-0">
                  <SelectValue>
                    {(value) => TIMESTAMP_FORMAT_OPTIONS.find((opt) => opt.value === value)?.label}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false} className="bg-secondary">
                  <SelectGroup>
                    {TIMESTAMP_FORMAT_OPTIONS.map(({ value, label }) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </td>
          </tr>
        </tbody>
      </table>
    </PopoverContent>
  );
};

export const SettingsPopover = ({ children }: React.PropsWithChildren) => {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <PopoverTrigger render={children as React.ReactElement} />
      {isOpen && <SettingsPopoverContent />}
    </Popover>
  );
};
