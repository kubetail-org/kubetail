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

import { useQuery, useSubscription } from '@apollo/client';
import { PlusCircleIcon } from '@heroicons/react/24/solid';
import { useEffect, useState } from 'react';

import Button from '@kubetail/ui/elements/Button';
import Form from '@kubetail/ui/elements/Form';

import appConfig from '@/app-config';
import Modal from '@/components/elements/Modal';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';

const defaultKubeContext = appConfig.environment === 'cluster' ? '' : undefined;

const KubeContextPicker = ({
  className,
  value,
  setValue,
}: {
  className?: string;
  value?: string;
  setValue: (value: string) => void;
}) => {
  const { loading, data } = useSubscription(dashboardOps.KUBE_CONFIG_WATCH);
  const kubeConfig = data?.kubeConfigWatch?.object;

  // Set default value
  useEffect(() => {
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

type ClusterAPIPickerProps = {
  kubeContext: string;
}

const ClusterAPIPicker = ({ kubeContext }: ClusterAPIPickerProps) => {
  const { loading, data } = useQuery(dashboardOps.HELM_LIST_RELEASES, {
    skip: kubeContext === undefined,
    variables: { kubeContext: kubeContext || '' }
  });

  const releases = data?.helmListReleases;

  if (loading) return <div className="h-10 leading-10">Loading...</div>;

  if (releases) {
    return (
      <Form.Select>
        {releases?.map((release) => (
          <Form.Option key={release.name}>
            {release.name} (namespace: {release.namespace}, chart: {release.chart?.metadata?.version}, app: {release.chart?.metadata?.appVersion})
          </Form.Option>
        ))}
      </Form.Select>
    );
  }

  return (
    <Button intent="ghost" size="sm"><PlusCircleIcon className="h-5 w-5 mr-1" /> Install</Button>
  );
};

type ClusterSettingsDialogProps = {
  isOpen?: boolean;
  onClose: (value?: boolean) => void;
};

export const ClusterSettingsDialog = ({ isOpen = false, onClose }: ClusterSettingsDialogProps) => {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);

  return (
    <Modal open={isOpen} onClose={onClose} className="!max-w-[700px]">
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
      <div>
        <Form className="mt-5">
          <Form.Group>
            <Form.Label>
              Kubernetes API Token
            </Form.Label>
            <Form.Control placeholder="Token..." />
          </Form.Group>
          <Form.Group>
            <Form.Label>
              Kubetail Cluster API
            </Form.Label>
            <ClusterAPIPicker kubeContext={kubeContext || ''} />
          </Form.Group>
        </Form>
        <div className="mt-8">
          <Button>Save</Button>
        </div>
      </div>
    </Modal >
  );
};
