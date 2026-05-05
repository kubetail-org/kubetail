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
import { useCallback, useEffect, useState } from 'react';

import { Popover, PopoverTrigger, PopoverContent } from '@kubetail/ui/elements/popover';

import appConfig from '@/app-config';
import { useCLIUpdateNotification, useClusterUpdateNotification, useKubeContexts } from '@/lib/update-notifications';

type ClusterUpdateInfo = {
  hasUpdate: boolean;
  currentVersion: string | null;
  latestVersion: string | null;
};

type ClusterUpdateSubscriberProps = {
  kubeContext: string;
  onChange: (kubeContext: string, info: ClusterUpdateInfo) => void;
};

/**
 * Invisible: reads cluster update state for one kubeContext and lifts it to the parent. Mounted
 * outside `PopoverContent` so the subscription survives the popover opening/closing — otherwise
 * the trigger dot would only reflect contexts evaluated since the last open.
 */
const ClusterUpdateSubscriber = ({ kubeContext, onChange }: ClusterUpdateSubscriberProps) => {
  const { updateAvailable, currentVersion, latestVersion } = useClusterUpdateNotification(kubeContext);
  const hasUpdate = updateAvailable && !!latestVersion;

  useEffect(() => {
    onChange(kubeContext, { hasUpdate, currentVersion, latestVersion });
    return () => onChange(kubeContext, { hasUpdate: false, currentVersion: null, latestVersion: null });
  }, [kubeContext, hasUpdate, currentVersion, latestVersion, onChange]);

  return null;
};

export const NotificationsPopover = ({ children }: React.PropsWithChildren) => {
  const [isOpen, setIsOpen] = useState(false);
  const { updateAvailable, currentVersion, latestVersion } = useCLIUpdateNotification();
  const kubeContexts = useKubeContexts();

  // Aggregated cluster update state by kubeContext, populated by long-lived subscribers below.
  const [clusterUpdates, setClusterUpdates] = useState<Record<string, ClusterUpdateInfo>>({});
  const handleClusterUpdateChange = useCallback((kubeContext: string, info: ClusterUpdateInfo) => {
    setClusterUpdates((prev) => {
      const cur = prev[kubeContext];
      if (
        cur &&
        cur.hasUpdate === info.hasUpdate &&
        cur.currentVersion === info.currentVersion &&
        cur.latestVersion === info.latestVersion
      ) {
        return prev;
      }
      return { ...prev, [kubeContext]: info };
    });
  }, []);

  const hasCliUpdate = updateAvailable && !!latestVersion;
  const clustersWithUpdates =
    appConfig.environment === 'desktop' ? kubeContexts.filter((ctx) => clusterUpdates[ctx]?.hasUpdate) : [];
  const hasNotifications = hasCliUpdate || clustersWithUpdates.length > 0;

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <PopoverTrigger asChild>
        <div className="relative h-full">
          {children}
          {hasNotifications && <span className="absolute top-0.5 right-0.5 h-2 w-2 rounded-full bg-blue-500" />}
        </div>
      </PopoverTrigger>
      {/* Subscribers live outside PopoverContent so they stay mounted when the popover is closed,
          keeping the trigger dot accurate across all kubeContexts at all times. */}
      {appConfig.environment === 'desktop' &&
        kubeContexts.map((ctx) => (
          <ClusterUpdateSubscriber key={ctx} kubeContext={ctx} onChange={handleClusterUpdateChange} />
        ))}
      {isOpen && (
        <PopoverContent side="top" className="w-80 mr-1">
          <div className="space-y-2">
            <p className="text-sm font-medium">Notifications</p>
            {hasCliUpdate && (
              <div className="flex items-start gap-2 rounded border border-blue-200 bg-blue-50 p-2 text-sm text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100">
                <ArrowUpCircle className="mt-0.5 h-4 w-4 shrink-0" />
                <p>
                  CLI update: {currentVersion} → {latestVersion}
                </p>
              </div>
            )}
            {clustersWithUpdates.map((ctx) => {
              const info = clusterUpdates[ctx];
              return (
                <div
                  key={ctx}
                  className="flex items-start gap-2 rounded border border-blue-200 bg-blue-50 p-2 text-sm text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100"
                >
                  <ArrowUpCircle className="mt-0.5 h-4 w-4 shrink-0" />
                  <p>
                    Cluster update ({ctx}): {info.currentVersion} → {info.latestVersion}
                  </p>
                </div>
              );
            })}
            {!hasNotifications && <p className="text-sm text-muted-foreground">No new notifications</p>}
          </div>
        </PopoverContent>
      )}
    </Popover>
  );
};
