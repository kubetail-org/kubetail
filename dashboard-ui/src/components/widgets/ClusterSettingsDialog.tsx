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

import { useForm } from 'react-hook-form';

import { Form, FormControl, FormField, FormItem, FormLabel } from '@kubetail/ui/elements/form';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

import appConfig from '@/app-config';
import Modal from '@/components/elements/Modal';

/**
 * ClusterAPIPickerCluster component
 */

const ClusterAPIPickerCluster = () => (
  <Select value={appConfig.clusterAPIEndpoint ? 'enabled' : 'disabled'} disabled>
    <SelectTrigger>
      <SelectValue />
    </SelectTrigger>
    <SelectContent>
      {appConfig.clusterAPIEndpoint ? (
        <SelectItem value="enabled">{appConfig.clusterAPIEndpoint}</SelectItem>
      ) : (
        <SelectItem value="disabled">Disabled</SelectItem>
      )}
    </SelectContent>
  </Select>
);

/**
 *  ClusterSettingsDialog component
 */

type ClusterSettingsDialogProps = {
  isOpen?: boolean;
  onClose: (value?: boolean) => void;
  defaultKubeContext?: string;
};

export const ClusterSettingsDialog = ({ isOpen = false, onClose }: ClusterSettingsDialogProps) => {
  // const [kubeContext, setKubeContext] = useState(defaultKubeContext);

  const form = useForm();

  return (
    <Modal open={isOpen} onClose={onClose} className="max-w-[550px]!">
      <Modal.Title className="flex items-center space-x-3">
        <span>Cluster Settings</span>
        {/* appConfig.environment === 'desktop' &&
            <KubeContextPicker className="w-auto" value={kubeContext} setValue={setKubeContext} />
        */}
      </Modal.Title>
      <div className="mt-5 pb-8">
        <Form {...form}>
          <FormField
            control={form.control}
            name="kubeContext"
            render={() => (
              <FormItem>
                <FormLabel>Cluster API Endpoint</FormLabel>
                <FormControl>
                  <ClusterAPIPickerCluster />
                </FormControl>
              </FormItem>
            )}
          />
        </Form>
        {/*
        <Form.Group>
          <Form.Label>Cluster API Endpoint</Form.Label>
          {appConfig.environment === 'desktop' ? (
            <ClusterAPIPickerDesktop kubeContext={kubeContext} />
          ) : (
            <ClusterAPIPickerCluster />
          )}
        </Form.Group>
        */}
      </div>
    </Modal>
  );
};
