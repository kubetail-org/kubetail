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

import { ArrowUpCircle } from 'lucide-react';
import { useState } from 'react';

import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/popover';

import appConfig from '@/app-config';
import { useUpgradeNotification } from '@/lib/upgrade-notifications';

export const NotificationsPopover = ({ children }: React.PropsWithChildren) => {
  const [isOpen, setIsOpen] = useState(false);
  const { cliStatus, clusterStatus } = useUpgradeNotification();

  const hasCliUpdate = appConfig.environment === 'desktop' && cliStatus?.updateAvailable;
  const hasClusterUpdate = clusterStatus?.updateAvailable;
  const hasNotifications = hasCliUpdate || hasClusterUpdate;

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <PopoverTrigger asChild>
        <div className="relative h-full">
          {children}
          {hasNotifications && <span className="absolute top-0.5 right-0.5 h-2 w-2 rounded-full bg-blue-500" />}
        </div>
      </PopoverTrigger>
      {isOpen && (
        <PopoverContent side="top" className="w-80 mr-1">
          <div className="space-y-2">
            <p className="text-sm font-medium">Notifications</p>
            {hasNotifications ? (
              <div className="flex items-start gap-2 rounded border border-blue-200 bg-blue-50 p-2 text-sm text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100">
                <ArrowUpCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  {hasCliUpdate && cliStatus && (
                    <p>
                      CLI update: {cliStatus.currentVersion} → {cliStatus.latestVersion}
                    </p>
                  )}
                  {hasClusterUpdate && clusterStatus && (
                    <p>
                      Helm chart update: {clusterStatus.currentVersion} → {clusterStatus.latestVersion}
                    </p>
                  )}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No new notifications</p>
            )}
          </div>
        </PopoverContent>
      )}
    </Popover>
  );
};
