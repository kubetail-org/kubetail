// Copyright 2024-2026 The Kubetail Authors
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

import { useSubscription } from '@apollo/client/react';
import { Fragment, useEffect } from 'react';
import { atom, useAtomValue, useSetAtom } from 'jotai';

import { Dialog, DialogContent, DialogDescription, DialogTitle, DialogTrigger } from '@kubetail/ui/elements/dialog';
import { Table, TableBody, TableCell, TableRow } from '@kubetail/ui/elements/table';

import appConfig from '@/app-config';
import ClusterAPIInstallButton from '@/components/widgets/ClusterAPIInstallButton';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import {
  ServerStatus,
  Status,
  useDashboardServerStatus,
  useKubernetesAPIServerStatus,
  useClusterAPIServerStatus,
} from '@/lib/server-status';
import { cn } from '@/lib/util';

const kubernetesAPIServerStatusMapState = atom(new Map<string, ServerStatus>());

const clusterAPIServerStatusMapState = atom(new Map<string, ServerStatus>());

const HealthDot = ({ status }: { status: Status }) => {
  let color;
  switch (status) {
    case Status.Healthy:
      color = 'green';
      break;
    case Status.Unhealthy:
      color = 'red';
      break;
    case Status.Pending:
      color = 'yellow';
      break;
    case Status.Degraded:
      color = 'yellow';
      break;
    case Status.NotFound:
      color = 'yellow';
      break;
    case Status.Unknown:
      color = 'chrome';
      break;
    default:
      throw new Error('not implemented');
  }

  return (
    <div
      className={cn('inline-block w-[8px] h-[8px] rounded-full', {
        'bg-chrome-300': color === 'chrome',
        'bg-red-500': color === 'red',
        'bg-green-500': color === 'green',
        'bg-yellow-500': color === 'yellow',
      })}
    />
  );
};

const statusMessage = (s: ServerStatus, unknownDefault: string): string => {
  switch (s.status) {
    case Status.Healthy:
      return s.message || 'Ok';
    case Status.Unhealthy:
      return s.message || 'Error';
    case Status.Pending:
      return s.message || 'Pending';
    case Status.NotFound:
      return s.message || 'Not found';
    case Status.Unknown:
      return s.message || unknownDefault;
    default:
      throw new Error('not implemented');
  }
};

function useEffectServerStatus(
  kubeContext: string,
  setServerStatusMap: (update: (prev: Map<string, ServerStatus>) => Map<string, ServerStatus>) => void,
  serverStatus: ServerStatus,
) {
  useEffect(() => {
    setServerStatusMap((currVal) => {
      const newVal = new Map(currVal);
      newVal.set(kubeContext, serverStatus);
      return newVal;
    });

    // Clean up
    return () => {
      setServerStatusMap((currVal) => {
        const newVal = new Map(currVal);
        newVal.set(kubeContext, new ServerStatus());
        return newVal;
      });
    };
  }, [serverStatus.status]);
}

const KubernetesAPIServerStatusFetcher = ({ kubeContext }: { kubeContext: string }) => {
  const serverStatus = useKubernetesAPIServerStatus(kubeContext);

  // Update map
  const setServerStatusMap = useSetAtom(kubernetesAPIServerStatusMapState);
  useEffectServerStatus(kubeContext, setServerStatusMap, serverStatus);

  return null;
};

const ClusterAPIServerStatusFetcher = ({ kubeContext }: { kubeContext: string }) => {
  const serverStatus = useClusterAPIServerStatus(kubeContext);

  // Update map
  const setServerStatusMap = useSetAtom(clusterAPIServerStatusMapState);
  useEffectServerStatus(kubeContext, setServerStatusMap, serverStatus);

  return null;
};

type ServerStatusCellsProps = {
  serverStatus: ServerStatus;
  defaultMessage?: string;
};

const ServerStatusCells = ({ serverStatus, defaultMessage }: ServerStatusCellsProps) => (
  <>
    <TableCell className="w-px">
      <HealthDot status={serverStatus.status} />
    </TableCell>
    <TableCell className="whitespace-normal">{statusMessage(serverStatus, defaultMessage || 'Uknown')}</TableCell>
  </>
);

type ServerStatusRowProps = {
  kubeContext: string;
  dashboardServerStatus: ServerStatus;
};

const KubernetesAPIServerStatusRow = ({ kubeContext, dashboardServerStatus }: ServerStatusRowProps) => {
  const serverStatusMap = useAtomValue(kubernetesAPIServerStatusMapState);
  const serverStatus = serverStatusMap.get(kubeContext) || new ServerStatus();

  return (
    <TableRow>
      <TableCell className="w-px">Kubernetes API</TableCell>
      {dashboardServerStatus.status === Status.Unhealthy ? (
        <ServerStatusCells serverStatus={new ServerStatus()} />
      ) : (
        <ServerStatusCells serverStatus={serverStatus} />
      )}
    </TableRow>
  );
};

const ClusterAPIServerStatusRow = ({ kubeContext, dashboardServerStatus }: ServerStatusRowProps) => {
  const serverStatusMap = useAtomValue(clusterAPIServerStatusMapState);
  const serverStatus = serverStatusMap.get(kubeContext) || new ServerStatus();

  return (
    <TableRow>
      <TableCell className="w-px">Kubetail Cluster API</TableCell>
      {dashboardServerStatus.status === Status.Unhealthy ? (
        <ServerStatusCells serverStatus={new ServerStatus()} />
      ) : (
        <>
          <TableCell className="w-px">
            <HealthDot status={serverStatus.status} />
          </TableCell>
          <TableCell className="whitespace-normal flex justify-between items-center">
            {statusMessage(serverStatus, 'Uknown')}
            {appConfig.environment === 'desktop' && serverStatus.status === Status.NotFound && (
              <ClusterAPIInstallButton kubeContext={kubeContext} />
            )}
          </TableCell>
        </>
      )}
    </TableRow>
  );
};

const StatusTable = ({ children }: React.PropsWithChildren) => (
  <div className="rounded-md border-1 shadow-xs">
    <Table>
      <TableBody>{children}</TableBody>
    </Table>
  </div>
);

type ServerStatusWidgetProps = {
  className?: string;
};

const ServerStatusWidget = ({ className }: ServerStatusWidgetProps) => {
  const { data } = useSubscription(dashboardOps.KUBE_CONFIG_WATCH, { skip: appConfig.environment === 'cluster' });

  const kubeContexts = new Array<string>();
  if (appConfig.environment === 'cluster') {
    kubeContexts.push('');
  } else {
    data?.kubeConfigWatch?.object?.contexts.map((context) => kubeContexts.push(context.name));
  }

  const dashboardServerStatus = useDashboardServerStatus();
  const kubernetesAPIServertatusMap = useAtomValue(kubernetesAPIServerStatusMapState);
  const clusterAPIServerStatusMap = useAtomValue(clusterAPIServerStatusMapState);

  // Determine overall status
  let overallStatus = Status.Unknown;
  if (dashboardServerStatus.status === Status.Unhealthy) {
    overallStatus = Status.Unhealthy;
  } else {
    const all = [dashboardServerStatus, ...kubernetesAPIServertatusMap.values(), ...clusterAPIServerStatusMap.values()];
    if (all.every((item) => item.status === Status.Healthy)) overallStatus = Status.Healthy;
    else overallStatus = Status.Degraded;
  }

  return (
    <div className="inline-block">
      <Dialog>
        <DialogTrigger asChild>
          <button
            type="button"
            className={cn('px-2 rounded-tl-sm flex items-center space-x-1 cursor-pointer', className)}
          >
            <div className="text-sm">status:</div>
            <HealthDot status={overallStatus} />
          </button>
        </DialogTrigger>
        <DialogContent className="max-h-[calc(100vh-4rem)] overflow-y-auto">
          <DialogTitle>Health Status</DialogTitle>
          <DialogDescription />
          <StatusTable>
            <TableRow>
              <TableCell className="w-px">Dashboard Backend</TableCell>
              <ServerStatusCells serverStatus={dashboardServerStatus} defaultMessage="Connecting..." />
            </TableRow>
          </StatusTable>
          {kubeContexts.map((kubeContext) => (
            <Fragment key={kubeContext}>
              <div className="mt-8 ml-3 mb-1">{kubeContext || 'Cluster'}</div>
              <StatusTable>
                <KubernetesAPIServerStatusRow kubeContext={kubeContext} dashboardServerStatus={dashboardServerStatus} />
                <ClusterAPIServerStatusRow kubeContext={kubeContext} dashboardServerStatus={dashboardServerStatus} />
              </StatusTable>
            </Fragment>
          ))}
        </DialogContent>
      </Dialog>
      {kubeContexts.map((kubeContext) => (
        <Fragment key={kubeContext}>
          <KubernetesAPIServerStatusFetcher kubeContext={kubeContext} />
          <ClusterAPIServerStatusFetcher kubeContext={kubeContext} />
        </Fragment>
      ))}
    </div>
  );
};

const ServerStatusWidgetWrapper = (props: ServerStatusWidgetProps) => <ServerStatusWidget {...props} />;

export default ServerStatusWidgetWrapper;
