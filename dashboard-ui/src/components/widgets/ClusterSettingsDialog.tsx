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
import { useEffect, useState } from 'react';

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

type ClusterSettingsDialogProps = {
  isOpen?: boolean;
  onClose: (value?: boolean) => void;
};

export const ClusterSettingsDialog = ({ isOpen = false, onClose }: ClusterSettingsDialogProps) => {
  const [kubeContext, setKubeContext] = useState(defaultKubeContext);

  const { loading, data } = useQuery(dashboardOps.HELM_LIST_RELEASES, {
    skip: kubeContext === undefined,
    variables: { kubeContext }
  });

  return (
    <Modal open={isOpen} onClose={onClose} className="!max-w-[550px]">
      <Modal.Title>Cluster Settings</Modal.Title>
      <div>
        {appConfig.environment === 'desktop' && (
          <KubeContextPicker
            className="w-auto"
            value={kubeContext}
            setValue={setKubeContext}
          />
        )}
        <Form className="mt-5">
          <Form.Group>
            <Form.Label>
              Kubernetes API Token
            </Form.Label>
            <Form.Control placeholder="Token..." />
          </Form.Group>
          <Form.Fieldset>
            <Form.Label>
              Kubetail Cluster API
            </Form.Label>
            <div className="flex">
              <Form.Group>
                <Form.Label>Namespace</Form.Label>
                <Form.Control placeholder="Namespace" />
              </Form.Group>
              <Form.Group>
                <Form.Label>Service Name</Form.Label>
                <Form.Control placeholder="Service Name" />
              </Form.Group>
            </div>
          </Form.Fieldset>
        </Form>
      </div>
    </Modal>
  );
};
