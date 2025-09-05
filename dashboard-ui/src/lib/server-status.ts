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

import { useSubscription } from '@apollo/client';
import { useEffect, useState } from 'react';

import { dashboardWSClient } from '@/apollo-client';
import appConfig from '@/app-config';
import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { HealthCheckStatus, type HealthCheckResponse } from '@/lib/graphql/dashboard/__generated__/graphql';

export const enum Status {
  Healthy = 'HEALTHY',
  Unhealthy = 'UNHEALTHY',
  Pending = 'PENDING',
  Degraded = 'DEGRADED',
  Unknown = 'UNKNOWN',
  NotFound = 'NOTFOUND',
}

export class ServerStatus {
  public status: Status = Status.Unknown;

  public message: string | null = null;

  public lastUpdatedAt: Date | null = null;

  public constructor(init?: Partial<ServerStatus>) {
    Object.assign(this, init);
  }

  static fromHealthCheckResponse(healthCheckResponse: HealthCheckResponse | undefined) {
    const ss = new ServerStatus({
      message: healthCheckResponse?.message || null,
      lastUpdatedAt: healthCheckResponse?.timestamp || null,
    });

    switch (healthCheckResponse?.status) {
      case HealthCheckStatus.Success:
        ss.status = Status.Healthy;
        break;
      case HealthCheckStatus.Pending:
        ss.status = Status.Pending;
        break;
      case HealthCheckStatus.Notfound:
        ss.status = Status.NotFound;
        break;
      default:
        ss.status = Status.Unhealthy;
    }

    return ss;
  }
}

export function useDashboardServerStatus() {
  const [status, setStatus] = useState<ServerStatus>(new ServerStatus());

  useEffect(() => {
    const fns = [
      dashboardWSClient.on('connected', () => {
        setStatus(new ServerStatus({ status: Status.Healthy, lastUpdatedAt: new Date() }));
      }),
      dashboardWSClient.on('pong', () => {
        setStatus((prevStatus) => {
          if (prevStatus.status !== Status.Healthy) {
            return new ServerStatus({ status: Status.Healthy, lastUpdatedAt: new Date() });
          }
          return prevStatus;
        });
      }),
      dashboardWSClient.on('closed', () => {
        const newStatus = new ServerStatus({ status: Status.Unhealthy, lastUpdatedAt: new Date() });
        newStatus.message = 'Unable to connect';
        setStatus(newStatus);
      }),
      dashboardWSClient.on('error', () => {
        const newStatus = new ServerStatus({ status: Status.Unhealthy, lastUpdatedAt: new Date() });
        newStatus.message = 'Error while establishing connection';
        setStatus(newStatus);
      }),
    ];

    // cleanup
    return () => fns.forEach((fn) => fn());
  }, []);

  return status;
}

export function useKubernetesAPIServerStatus(kubeContext: string) {
  const [status, setStatus] = useState<ServerStatus>(new ServerStatus());

  useSubscription(dashboardOps.SERVER_STATUS_KUBERNETES_API_HEALTHZ_WATCH, {
    variables: { kubeContext },
    onData: ({ data }) => setStatus(ServerStatus.fromHealthCheckResponse(data.data?.kubernetesAPIHealthzWatch)),
  });

  return status;
}

export function useClusterAPIServerStatus(kubeContext: string) {
  const [status, setStatus] = useState<ServerStatus>(new ServerStatus());

  useSubscription(dashboardOps.SERVER_STATUS_CLUSTER_API_HEALTHZ_WATCH, {
    skip: !appConfig.clusterAPIEnabled,
    variables: { kubeContext },
    onData: ({ data }) => {
      setStatus(ServerStatus.fromHealthCheckResponse(data.data?.clusterAPIHealthzWatch));
    },
    onError: (err) => {
      if (err.message === 'not available') {
        setStatus(
          new ServerStatus({
            status: Status.NotFound,
            message: 'Not available',
            lastUpdatedAt: new Date(),
          }),
        );
      }
    },
  });

  return status;
}
