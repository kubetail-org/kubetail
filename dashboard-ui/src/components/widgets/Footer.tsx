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

import { ChevronDownIcon } from 'lucide-react';

import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from '@kubetail/ui/elements/select';

import ServerStatus from '@/components/widgets/ServerStatus';
import { useTheme, UserPreference } from '@/lib/theme';
import EnvironmentControl from './EnvironmentControl';

export default function Footer() {
  const { userPreference, setUserPreference } = useTheme();

  const handleChange = (value: UserPreference) => {
    setUserPreference(value);
  };

  return (
    <div className="h-[22px] bg-chrome-100 border-t border-chrome-divider text-sm flex justify-between items-center pl-[10px]">
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
      <div className="flex">
        {import.meta.env.MODE === 'development' && <EnvironmentControl />}
        <ServerStatus />
      </div>
    </div>
  );
}
