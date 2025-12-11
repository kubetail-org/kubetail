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

import { Bell } from 'lucide-react';

import ServerStatus from '@/components/widgets/ServerStatus';
import { SettingsPopover } from '@/components/widgets/SettingsPopover';
import { NotificationsPopover } from '@/components/widgets/NotificationsPopover';
import EnvironmentControl from './EnvironmentControl';

export default function Footer() {
  return (
    <div className="h-6 bg-sidebar border-t border-sidebar-border flex justify-end items-center px-6 space-x-2">
      {import.meta.env.MODE === 'development' && <EnvironmentControl />}
      <SettingsPopover>
        <button type="button" className="h-full hover:bg-chrome-300 px-1 text-xs">
          Settings
        </button>
      </SettingsPopover>
      <NotificationsPopover>
        <button type="button" className="h-full hover:bg-chrome-300 px-1">
          <Bell className="h-4 w-4" />
        </button>
      </NotificationsPopover>
      <ServerStatus className="h-full hover:bg-chrome-300 px-1" />
    </div>
  );
}
