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

import { useMutation, useSubscription } from '@apollo/client';
import { PlusCircleIcon } from '@heroicons/react/24/outline';
import { useEffect, useState } from 'react';

import Button from '@kubetail/ui/elements/Button';
import Form from '@kubetail/ui/elements/Form';
import Spinner from '@kubetail/ui/elements/Spinner';

import appConfig from '@/app-config';
import Modal from '@/components/elements/Modal';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import type { ClusterApiServicesListItemFragmentFragment } from '@/lib/graphql/dashboard/__generated__/graphql';
import { useListQueryWithSubscription } from '@/lib/hooks';

type KubeContextPickerProps = {
  className?: string;
  value?: string;
  setValue: (value: string) => void;
};

const KubeContextPicker = ({ className, value, setValue }: KubeContextPickerProps) => {
  const { loading, data } = useSubscription(dashboardOps.KUBE_CONFIG_WATCH);
  const kubeConfig = data?.kubeConfigWatch?.object;

  // Set default value
  useEffect(() => {
    if (value) return;
    const defaultValue = kubeConfig?.currentContext;
    if (defaultValue) setValue(defaultValue);
  }, [loading]);

  return (
    <Form.Select
      className={className}
      value={value}
      onChange={(ev) => setValue(ev.target.value)}
      disabled={loading}
    >
      {loading ? (
        <Form.Option>Loading...</Form.Option>
      ) : (
        kubeConfig && kubeConfig.contexts.map((context) => (
          <Form.Option key={context.name} value={context.name}>{context.name}</Form.Option>
        ))
      )}
    </Form.Select>
  );
};

const generateServiceUrl = (service: ClusterApiServicesListItemFragmentFragment) => {
  const { ports } = service.spec;
  const appProtocol = ports.length ? ports[0].appProtocol : 'http';
  const port = ports.length ? ports[0].port : 'http';
  return `${appProtocol}://${service.metadata.name}.${service.metadata.namespace}.svc:${port}`;
};

type ClusterAPIPickerDesktopProps = {
  kubeContext?: string;
};

const ClusterAPIPickerDesktop = ({ kubeContext }: ClusterAPIPickerDesktopProps) => {
  const { loading, data } = useListQueryWithSubscription({
    skip: kubeContext === undefined,
    query: dashboardOps.CLUSTER_API_SERVICES_LIST_FETCH,
    subscription: dashboardOps.CLUSTER_API_SERVICES_LIST_WATCH,
    queryDataKey: 'clusterAPIServicesList',
    subscriptionDataKey: 'clusterAPIServicesWatch',
    variables: { kubeContext },
  });

  const [install, installMutation] = useMutation(dashboardOps.HELM_INSTALL_LATEST);

  const [installFeedback, setInstallFeedback] = useState<string>();

  const handleInstall = async () => {
    setInstallFeedback('');

    try {
      await install({ variables: { kubeContext } });
    } catch (e: unknown) {
      if (e instanceof Error) {
        setInstallFeedback(e.message);
      } else {
        setInstallFeedback('An unknown error occurred (see console)');
        console.error(e);
      }
    }
  };

  const handleChange = async (ev: React.ChangeEvent<HTMLSelectElement>) => {
    console.log(ev);
  };

  const services = data?.clusterAPIServicesList?.items;

  if (kubeContext === undefined || loading) {
    return <div className="h-10 leading-10">Loading...</div>;
  }

  return (
    <>
      {services?.length ? (
        <Form.Select onChange={handleChange}>
          {services?.map((service) => (
            <Form.Option key={service.metadata.name} value={service.metadata.name}>
              {generateServiceUrl(service)}
            </Form.Option>
          ))}
        </Form.Select>
      ) : (
        <div>
          <Button intent="secondary" size="sm" onClick={handleInstall} disabled={installMutation.loading}>
            {installMutation.loading ? (
              <Spinner size="xs" />
            ) : (
              <PlusCircleIcon className="h-5 w-5 mr-1" />
            )}
            Install
          </Button>
          {installFeedback && <Form.Feedback>{installFeedback}</Form.Feedback>}
        </div>
      )}
    </>
  );
};

const ClusterAPIPickerCluster = () => (
  <Form.Select disabled>
    {appConfig.clusterAPIEndpoint ? (
      <Form.Option>{appConfig.clusterAPIEndpoint}</Form.Option>
    ) : (
      <Form.Option>Disabled</Form.Option>
    )}
  </Form.Select>
);

type ClusterSettingsDialogProps = {
  isOpen?: boolean;
  onClose: (value?: boolean) => void;
  defaultKubeContext?: string;
};

export const ClusterSettingsDialog = ({ isOpen = false, onClose, defaultKubeContext }: ClusterSettingsDialogProps) => {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);

  return (
    <Modal open={isOpen} onClose={onClose} className="!max-w-[550px]">
      <Modal.Title className="flex items-center space-x-3">
        <span>Cluster Settings</span>
        {appConfig.environment === 'desktop' && (
          <KubeContextPicker
            className="w-auto"
            value={kubeContext}
            setValue={setKubeContext}
          />
        )}
      </Modal.Title>
      <div className="mt-5 pb-8">
        <Form.Group>
          <Form.Label>
            Cluster API Endpoint
          </Form.Label>
          {appConfig.environment === 'desktop' ? (
            <ClusterAPIPickerDesktop kubeContext={kubeContext} />
          ) : (
            <ClusterAPIPickerCluster />
          )}
        </Form.Group>
      </div>
    </Modal>
  );
};
