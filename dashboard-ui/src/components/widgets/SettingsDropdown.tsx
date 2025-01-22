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

import { Settings as SettingsIcon } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';

import Button from '@kubetail/ui/elements/Button';
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuPortal,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/DropdownMenu';

import appConfig from '@/app-config';
import { useTheme, UserPreference } from '@/lib/theme';
import { ClusterSettingsDialog } from '@/components/widgets/ClusterSettingsDialog';

const SettingsDropdown = () => {
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const { userPreference, setUserPreference } = useTheme();

  const themes = [UserPreference.Light, UserPreference.Dark, UserPreference.System];

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button intent="outline" size="sm"><SettingsIcon size={18} strokeWidth={1.5} /></Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-[150px]">
          <DropdownMenuGroup>
            <DropdownMenuItem onSelect={() => setIsDialogOpen(true)}>
              Cluster Settings
            </DropdownMenuItem>
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>Theme</DropdownMenuSubTrigger>
              <DropdownMenuPortal>
                <DropdownMenuSubContent>
                  {themes.map((theme) => (
                    <DropdownMenuCheckboxItem
                      key={theme}
                      checked={theme === userPreference}
                      onCheckedChange={() => setUserPreference(theme)}
                    >
                      {theme}
                    </DropdownMenuCheckboxItem>
                  ))}
                </DropdownMenuSubContent>
              </DropdownMenuPortal>
            </DropdownMenuSub>
          </DropdownMenuGroup>
          {appConfig.authMode === 'token' && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem>
                <Link to={`/auth/logout?${new URLSearchParams({ callbackUrl: window.location.pathname + window.location.search })}`}>
                  Sign out
                </Link>
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <ClusterSettingsDialog isOpen={isDialogOpen} onClose={() => setIsDialogOpen(false)} />
    </>
  );
};

export default SettingsDropdown;
