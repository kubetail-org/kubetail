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

import { useState } from 'react';
import { ChevronDownIcon } from 'lucide-react';

import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/popover';
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';
import { Table, TableBody, TableRow, TableCell } from '@kubetail/ui/elements/table';

import { useTheme, UserPreference } from '@/lib/theme';

const SettingsPopoverContent = () => {
  const { userPreference, setUserPreference } = useTheme();

  const handleChange = (value: UserPreference) => {
    setUserPreference(value);
  };

  return (
    <PopoverContent side="top" className="w-80 mr-1">
      <div>Settings</div>
      <Table>
        <TableBody>
          <TableRow>
            <TableCell>Theme</TableCell>
            <TableCell>
              <Select value={userPreference} onValueChange={handleChange}>
                <SelectTrigger asChild>
                  <div className="cursor-pointer flex items-center gap-1">
                    <SelectValue />
                    <ChevronDownIcon className="size-4" />
                  </div>
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectLabel>Theme</SelectLabel>
                    <SelectItem value={UserPreference.System}>system</SelectItem>
                    <SelectItem value={UserPreference.Dark}>dark</SelectItem>
                    <SelectItem value={UserPreference.Light}>light</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </TableCell>
          </TableRow>
        </TableBody>
      </Table>
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
