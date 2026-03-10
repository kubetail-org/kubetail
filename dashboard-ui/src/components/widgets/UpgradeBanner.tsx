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

import { ArrowUpCircle, X } from 'lucide-react';

import type { VersionStatusData } from '@/lib/upgrade-notifications';

interface UpgradeBannerProps {
  cliStatus: VersionStatusData | null;
  dismiss: () => void;
  dontRemindMe: () => void;
}

export default function UpgradeBanner({ cliStatus, dismiss, dontRemindMe }: UpgradeBannerProps) {
  if (!cliStatus?.updateAvailable) return null;

  return (
    <div
      role="status"
      className="flex items-center justify-between gap-2 border-b border-blue-200 bg-blue-50 px-4 py-2 text-sm text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100"
    >
      <div className="flex items-center gap-2">
        <ArrowUpCircle className="h-4 w-4 shrink-0" />
        <span>
          <strong>Update available:</strong> CLI {cliStatus.currentVersion} → {cliStatus.latestVersion}. Use your
          package manager to upgrade.
        </span>
      </div>
      <div className="flex items-center gap-2 shrink-0">
        <button type="button" onClick={dontRemindMe} className="text-xs underline hover:no-underline">
          Don&apos;t remind me
        </button>
        <button
          type="button"
          onClick={dismiss}
          aria-label="Dismiss"
          className="rounded p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}
