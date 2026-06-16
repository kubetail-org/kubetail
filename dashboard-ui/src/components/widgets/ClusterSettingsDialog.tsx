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

import React, { useState } from 'react';

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@kubetail/ui/elements/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

import appConfig from '@/app-config';
import ClusterAPIInstallButton from '@/components/widgets/ClusterAPIInstallButton';
import KubeContextPicker from '@/components/widgets/KubeContextPicker';
import { Status, useClusterAPIServerStatus } from '@/lib/server-status';

/**
 * ClusterAPIPickerDesktop component
 */

type ClusterAPIPickerDesktopProps = {
  kubeContext: string | null;
};

const ClusterAPIPickerDesktop = ({ kubeContext }: ClusterAPIPickerDesktopProps) => {
  const serverStatus = useClusterAPIServerStatus(kubeContext ?? '');

  if (kubeContext === null) {
    return <div className="h-10 leading-10">Loading...</div>;
  }

  // Unknown is the pre-first-update state; render Loading to avoid flicker.
  if (serverStatus.status === Status.Unknown) {
    return <div className="h-10 leading-10">Loading...</div>;
  }

  if (serverStatus.status === Status.NotFound) {
    return <ClusterAPIInstallButton kubeContext={kubeContext} />;
  }

  return <div>/apis/api.kubetail.com/v1</div>;
};

/**
 * ClusterAPIPickerCluster component
 */

const ClusterAPIPickerCluster = () => (
  <Select value={appConfig.clusterAPIEnabled ? 'enabled' : 'disabled'} disabled>
    <SelectTrigger>
      <SelectValue>{(value) => (value === 'enabled' ? '/apis/api.kubetail.com/v1' : 'Disabled')}</SelectValue>
    </SelectTrigger>
    <SelectContent alignItemWithTrigger={false}>
      {appConfig.clusterAPIEnabled ? (
        <SelectItem value="enabled">/apis/api.kubetail.com/v1</SelectItem>
      ) : (
        <SelectItem value="disabled">Disabled</SelectItem>
      )}
    </SelectContent>
  </Select>
);

/**
 *  ClusterSettingsDialogContent component
 */

interface ClusterSettingsDialogProps extends React.ComponentProps<typeof Dialog> {
  defaultKubeContext: string | null;
}

export const ClusterSettingsDialog = ({ defaultKubeContext, ...props }: ClusterSettingsDialogProps) => {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);

  return (
    <Dialog {...props}>
      <DialogContent>
        <DialogTitle className="flex items-center space-x-3">
          <span>Cluster Settings</span>
          {appConfig.environment === 'desktop' && (
            <KubeContextPicker className="w-auto" value={kubeContext} setValue={setKubeContext} />
          )}
        </DialogTitle>
        <DialogDescription />
        <div className="mt-5 pb-8">
          <div className="text-heading-sm">Cluster API Endpoint</div>
          {appConfig.environment === 'desktop' ? (
            <ClusterAPIPickerDesktop kubeContext={kubeContext} />
          ) : (
            <ClusterAPIPickerCluster />
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};
