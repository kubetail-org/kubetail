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

import * as SelectPrimitive from '@radix-ui/react-select';
import { CheckIcon } from 'lucide-react';
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
import { formatTimezoneOffset, TIMEZONES, useTimezone } from '@/lib/timezone';

const SettingsPopoverContent = () => {
  const { theme, setTheme } = useTheme();
  const [timezone, setTimezone] = useTimezone();

  const handleThemeChange = (value: Theme) => {
    setTheme(value);
  };

  const handleTimezoneChange = (value: string) => {
    setTimezone(value);
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
                <SelectContent className="bg-secondary">
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
                  <SelectValue />
                </SelectTrigger>
                <SelectContent className="bg-secondary max-h-60">
                  <SelectGroup>
                    {TIMEZONES.map((tz) => (
                      <SelectPrimitive.Item
                        key={tz}
                        value={tz}
                        className="focus:bg-accent focus:text-accent-foreground relative flex w-full cursor-default items-center gap-2 rounded-sm py-1.5 pr-8 pl-2 text-sm outline-hidden select-none data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
                      >
                        <span className="absolute right-2 flex size-3.5 items-center justify-center">
                          <SelectPrimitive.ItemIndicator>
                            <CheckIcon className="size-4" />
                          </SelectPrimitive.ItemIndicator>
                        </span>
                        <SelectPrimitive.ItemText>{tz}</SelectPrimitive.ItemText>
                        <span className="text-muted-foreground ml-auto">{offsets.get(tz)}</span>
                      </SelectPrimitive.Item>
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
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      {isOpen && <SettingsPopoverContent />}
    </Popover>
  );
};
