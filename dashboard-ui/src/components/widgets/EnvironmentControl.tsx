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

import { useEffect, useState } from 'react';

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

import Modal from '@/components/elements/Modal';

type APIMode = 'auto' | 'true' | 'false';

export const LOCAL_STORAGE_KEY = 'kubetail:dev:clusterAPIEnabledOverride';

const EnvironmentControl = () => {
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [apiMode, setApiMode] = useState<APIMode>(() => (localStorage.getItem(LOCAL_STORAGE_KEY) || 'auto') as APIMode);

  useEffect(() => {
    if (apiMode === 'auto') {
      localStorage.removeItem(LOCAL_STORAGE_KEY);
    } else {
      localStorage.setItem(LOCAL_STORAGE_KEY, apiMode);
    }
  }, [apiMode]);

  const handleModeChange = (value: APIMode) => {
    setApiMode(value);
    window.location.reload();
  };

  return (
    <div>
      <button
        className="text-xs text-chrome-500 hover:text-chrome-700 pr-3"
        type="button"
        onClick={() => setIsDialogOpen(true)}
      >
        Environment Control
      </button>
      <Modal open={isDialogOpen} onClose={() => setIsDialogOpen(false)} className="max-w-[550px]">
        <Modal.Title>Environment Control</Modal.Title>
        <div className="mt-5 pb-8">
          <div className="text-lg">Switch between Kubernetes API and Kubetail API</div>
          <div className="pt-3">
            <Select value={apiMode} onValueChange={handleModeChange}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="auto">Auto</SelectItem>
                <SelectItem value="true">Kubetail API</SelectItem>
                <SelectItem value="false">Kubernetes API</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </Modal>
    </div>
  );
};

export default EnvironmentControl;
