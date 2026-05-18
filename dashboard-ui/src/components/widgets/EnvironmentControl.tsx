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

import { useEffect, useState } from 'react';

import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from '@kubetail/ui/elements/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

type APIMode = 'auto' | 'true' | 'false';

const API_MODE_LABELS: Record<APIMode, string> = {
  auto: 'Auto',
  true: 'Kubetail API',
  false: 'Kubernetes API',
};

export const LOCAL_STORAGE_KEY = 'kubetail:dev:clusterAPIEnabledOverride';

const EnvironmentControl = () => {
  const [apiMode, setApiMode] = useState<APIMode>(() => (localStorage.getItem(LOCAL_STORAGE_KEY) || 'auto') as APIMode);

  useEffect(() => {
    if (apiMode === 'auto') {
      localStorage.removeItem(LOCAL_STORAGE_KEY);
    } else {
      localStorage.setItem(LOCAL_STORAGE_KEY, apiMode);
    }
  }, [apiMode]);

  const handleModeChange = (value: APIMode | null) => {
    if (value === null) return;
    setApiMode(value);
    window.location.reload();
  };

  return (
    <Dialog>
      <DialogTrigger
        render={
          <button type="button" className="h-full text-xs text-chrome-500 px-1 hover:bg-secondary">
            Environment Control
          </button>
        }
      />
      <DialogContent>
        <DialogTitle>Environment Control</DialogTitle>
        <DialogDescription />
        <div className="mt-5 pb-8">
          <div className="text-lg">Switch between Kubernetes API and Kubetail API</div>
          <div className="pt-3">
            <Select value={apiMode} onValueChange={handleModeChange}>
              <SelectTrigger>
                <SelectValue>{(value) => API_MODE_LABELS[value as APIMode]}</SelectValue>
              </SelectTrigger>
              <SelectContent alignItemWithTrigger={false}>
                <SelectItem value="auto">{API_MODE_LABELS.auto}</SelectItem>
                <SelectItem value="true">{API_MODE_LABELS.true}</SelectItem>
                <SelectItem value="false">{API_MODE_LABELS.false}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
};

export default EnvironmentControl;
