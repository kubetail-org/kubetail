// Copyright 2024 Andres Morey
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

import { useState } from 'react';

import DataTable from '@kubetail/ui/elements/DataTable';

import Modal from '@/components/elements/Modal';
import { Status, useServerStatus, type ServerStatus } from '@/lib/server-status';
import { cn } from '@/lib/utils';

const HealthDot = ({ status }: { status: Status }) => {
  let color;
  switch (status) {
    case Status.Healthy:
      color = 'green';
      break;
    case Status.Unhealthy:
      color = 'red';
      break;
    case Status.Unknown:
      color = 'chrome';
      break;
    default:
      throw new Error('not implemented');
  }

  return (
    <div
      className={cn(
        'inline-block w-[8px] h-[8px] rounded-full',
        {
          'bg-chrome-300': color === 'chrome',
          'bg-red-500': color === 'red',
          'bg-green-500': color === 'green',
        },
      )}
    />
  );
};

const statusMessage = (s: ServerStatus, unknownDefault: string): string => {
  switch (s.status) {
    case Status.Healthy:
      return s.message || 'Ok';
    case Status.Unhealthy:
      return s.message || 'Error';
    case Status.Unknown:
      return s.message || unknownDefault;
    default:
      throw new Error('not implemented');
  }
};

type ServerStatusProps = {
  className?: string;
};

export default ({ className }: ServerStatusProps) => {
  const { status, details } = useServerStatus();
  const [modalIsOpen, setModalIsOpen] = useState(false);

  return (
    <div className="inline-block">
      <div
        className={cn('px-2 rounded-tl flex items-center space-x-1 cursor-pointer', className)}
        onClick={() => setModalIsOpen(true)}
      >
        <div className="text-sm">status:</div>
        <HealthDot status={status} />
      </div>
      <Modal
        className="max-w-[500px]"
        open={modalIsOpen}
        onClose={() => setModalIsOpen(false)}
      >
        <Modal.Title>Server Status</Modal.Title>
        <DataTable>
          <DataTable.Body>
            <DataTable.Row>
              <DataTable.DataCell className="w-[1px]">Kubetail Backend</DataTable.DataCell>
              <DataTable.DataCell className="w-[1px]"><HealthDot status={details.backendServer.status} /></DataTable.DataCell>
              <DataTable.DataCell className="whitespace-normal">{statusMessage(details.backendServer, 'Connecting...')}</DataTable.DataCell>
            </DataTable.Row>
            <DataTable.Row>
              <DataTable.DataCell className="w-[1px]">Kubernetes Livez</DataTable.DataCell>
              <DataTable.DataCell className="w-[1px]"><HealthDot status={details.k8sLivez.status} /></DataTable.DataCell>
              <DataTable.DataCell className="whitespace-normal">{statusMessage(details.k8sLivez, 'Uknown')}</DataTable.DataCell>
            </DataTable.Row>
            <DataTable.Row>
              <DataTable.DataCell className="w-[1px]">Kubernetes Readyz</DataTable.DataCell>
              <DataTable.DataCell className="w-[1px]"><HealthDot status={details.k8sReadyz.status} /></DataTable.DataCell>
              <DataTable.DataCell className="whitespace-normal">{statusMessage(details.k8sReadyz, 'Unknown')}</DataTable.DataCell>
            </DataTable.Row>
          </DataTable.Body>
        </DataTable>
      </Modal>
    </div>
  );
};
