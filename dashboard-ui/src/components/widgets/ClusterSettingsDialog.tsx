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

import React, { useState } from 'react';

import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@kubetail/ui/elements/dialog';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';

import appConfig from '@/app-config';
import ClusterAPIInstallButton from '@/components/widgets/ClusterAPIInstallButton';
import KubeContextPicker from '@/components/widgets/KubeContextPicker';
import { CLUSTER_API_SERVICES_LIST_FETCH, CLUSTER_API_SERVICES_LIST_WATCH } from '@/lib/graphql/dashboard/ops';
import type { ClusterApiServicesListItemFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useListQueryWithSubscription } from '@/lib/hooks';

/**
 * generateServiceUrl
 */

const generateServiceUrl = (service: ClusterApiServicesListItemFragmentFragment) => {
  const { ports } = service.spec;
  const appProtocol = ports.length ? ports[0].appProtocol : 'http';
  const port = ports.length ? ports[0].port : 'http';
  return `${appProtocol}://${service.metadata.name}.${service.metadata.namespace}.svc:${port}`;
};

/**
 * ClusterAPIPickerDesktop component
 */

type ClusterAPIPickerDesktopProps = {
  kubeContext: string | null;
};

const ClusterAPIPickerDesktop = ({ kubeContext }: ClusterAPIPickerDesktopProps) => {
  const { loading, data } = useListQueryWithSubscription({
    skip: kubeContext === null,
    query: CLUSTER_API_SERVICES_LIST_FETCH,
    subscription: CLUSTER_API_SERVICES_LIST_WATCH,
    queryDataKey: 'clusterAPIServicesList',
    subscriptionDataKey: 'clusterAPIServicesWatch',
    variables: { kubeContext },
  });

  const services = data?.clusterAPIServicesList?.items;

  if (kubeContext === null || loading) {
    return <div className="h-10 leading-10">Loading...</div>;
  }

  // Check if kubetail is installed in default place
  let clusterAPIEndpoint = '';
  services?.forEach((service) => {
    if (service.metadata.namespace === 'kubetail-system' && service.metadata.name === 'kubetail-cluster-api') {
      clusterAPIEndpoint = generateServiceUrl(service);
    }
  });

  return (
    <>
      {clusterAPIEndpoint ? (
        <div>{clusterAPIEndpoint}</div>
      ) : (
        <div>
          <ClusterAPIInstallButton kubeContext={kubeContext} />
        </div>
      )}
    </>
  );
};

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
