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

import Form from '@kubetail/ui/elements/Form';

import Modal from '@/components/elements/Modal';

type EnvironmentControlProps = {
  className?: string;
};

export const LOCAL_STORAGE_KEY = 'kubetail:dev:clusterAPIEnabledOverride';

type APIMode = '' | 'true' | 'false';

const EnvironmentControl = ({ className }: EnvironmentControlProps) => {
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const [apiMode, setApiMode] = useState<APIMode>(() => (localStorage.getItem(LOCAL_STORAGE_KEY) || '') as APIMode);

  useEffect(() => {
    if (apiMode === '') {
      localStorage.removeItem(LOCAL_STORAGE_KEY);
    } else {
      localStorage.setItem(LOCAL_STORAGE_KEY, apiMode);
    }
  }, [apiMode]);

  const handleModeChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
    const newMode = e.target.value as APIMode;
    setApiMode(newMode);
    window.location.reload();
  };

  return (
    <div>
      <button
        className={`text-xs text-chrome-500 hover:text-chrome-700 pr-3 ${className}`}
        type="button"
        onClick={() => setIsDialogOpen(true)}
      >
        Environment Control
      </button>
      <Modal
        open={isDialogOpen}
        onClose={() => setIsDialogOpen(false)}
        className="max-w-[550px]"
      >
        <Modal.Title>Environment Control</Modal.Title>
        <div className="mt-5 pb-8">
          <Form.Group>
            <Form.Label>
              Switch between Kubernetes API and Kubetail API
            </Form.Label>
            <div className="pt-3">
              <Form.Select
                className={className}
                value={apiMode}
                onChange={handleModeChange}
              >
                <Form.Option value="">Auto</Form.Option>
                <Form.Option value="true">Kubetail API</Form.Option>
                <Form.Option value="false">Kubernetes API</Form.Option>
              </Form.Select>
            </div>
          </Form.Group>
        </div>
      </Modal>
    </div>
  );
};

export default EnvironmentControl;
