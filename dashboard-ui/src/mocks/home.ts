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

import type { MockedResponse } from '@apollo/client/testing';

import * as dashboardOps from '@/lib/graphql/dashboard/ops';

const genericListResponse = (typename: string) => ({
  __typename: typename,
  metadata: {
    continue: '',
    resourceVersion: 'v1',
  },
  items: [],
});

export const mocks: MockedResponse[] = [
  // kubeConfig
  {
    request: {
      query: dashboardOps.KUBE_CONFIG_WATCH,
    },
    result: {
      data: {
        kubeConfigWatch: {
          type: 'ADDED',
          object: {
            contexts: [
              {
                name: 'test1',
              },
              {
                name: 'test2',
              },
            ],
            currentContext: 'test1',
          },
        },
      },
    },
  },

  // cronjobs
  {
    request: {
      query: dashboardOps.HOME_CRONJOBS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        batchV1CronJobsList: genericListResponse('BatchV1CronJobList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_CRONJOBS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        batchV1CronJobsWatch: null,
      },
    },
  },

  // daemonsets
  {
    request: {
      query: dashboardOps.HOME_DAEMONSETS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        appsV1DaemonSetsList: genericListResponse('AppsV1DaemonSetList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_DAEMONSETS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        appsV1DaemonSetsWatch: null,
      },
    },
  },

  // deployments
  {
    request: {
      query: dashboardOps.HOME_DEPLOYMENTS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        appsV1DeploymentsList: genericListResponse('AppsV1DeploymentList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_DEPLOYMENTS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        appsV1DeploymentsWatch: null,
      },
    },
  },

  // jobs
  {
    request: {
      query: dashboardOps.HOME_JOBS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        batchV1JobsList: genericListResponse('BatchV1JobList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_JOBS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        batchV1JobsWatch: null,
      },
    },
  },

  // namespaces
  {
    request: {
      query: dashboardOps.HOME_NAMESPACES_LIST_FETCH,
      variables: { kubeContext: 'test1', continue: '' },
    },
    result: {
      data: {
        coreV1NamespacesList: genericListResponse('CoreV1NamespaceList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_NAMESPACES_LIST_WATCH,
      variables: { kubeContext: 'test1', resourceVersion: 'v1' },
    },
    result: {
      data: {
        coreV1NamespacesWatch: null,
      },
    },
  },

  // pods
  {
    request: {
      query: dashboardOps.HOME_PODS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        coreV1PodsList: genericListResponse('CoreV1PodList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_PODS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        coreV1PodsWatch: null,
      },
    },
  },

  // replicasets
  {
    request: {
      query: dashboardOps.HOME_REPLICASETS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        appsV1ReplicaSetsList: genericListResponse('AppsV1ReplicaSetList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_REPLICASETS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        appsV1ReplicaSetsWatch: null,
      },
    },
  },

  // statefulsets
  {
    request: {
      query: dashboardOps.HOME_STATEFULSETS_LIST_FETCH,
      variables: { kubeContext: 'test1', namespace: '', continue: '' },
    },
    result: {
      data: {
        appsV1StatefulSetsList: genericListResponse('AppsV1StatefulSetList'),
      },
    },
  },
  {
    request: {
      query: dashboardOps.HOME_STATEFULSETS_LIST_WATCH,
      variables: { kubeContext: 'test1', namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        appsV1StatefulSetsWatch: null,
      },
    },
  },

  // Kubernetes API readyWait
  {
    request: {
      query: dashboardOps.KUBERNETES_API_READY_WAIT,
      variables: { kubeContext: 'test1' },
    },
    result: {
      data: {
        kubernetesAPIReadyWait: true,
      },
    },
  },

  // Cluster API readyWait
  {
    request: {
      query: dashboardOps.CLUSTER_API_READY_WAIT,
      variables: { kubeContext: 'test1', namespace: 'kubetail-system', serviceName: 'kubetail-cluster-api' },
    },
    result: {
      data: {
        clusterAPIReadyWait: true,
      },
    },
  },

  // Kubernetes API healthz watch subscriptions
  {
    request: {
      query: dashboardOps.SERVER_STATUS_KUBERNETES_API_HEALTHZ_WATCH,
      variables: { kubeContext: 'test1' },
    },
    result: {
      data: {
        kubernetesAPIHealthzWatch: {
          __typename: 'HealthCheckResponse',
          status: 'SUCCESS',
          message: null,
          timestamp: null,
        },
      },
    },
  },
  {
    request: {
      query: dashboardOps.SERVER_STATUS_KUBERNETES_API_HEALTHZ_WATCH,
      variables: { kubeContext: 'test2' },
    },
    result: {
      data: {
        kubernetesAPIHealthzWatch: {
          __typename: 'HealthCheckResponse',
          status: 'SUCCESS',
          message: null,
          timestamp: null,
        },
      },
    },
  },

  // Cluster API health checks
  {
    request: {
      query: dashboardOps.SERVER_STATUS_CLUSTER_API_HEALTHZ_WATCH,
      variables: { kubeContext: 'test1' },
    },
    result: {
      data: {
        clusterAPIHealthzWatch: {
          __typename: 'HealthCheckResponse',
          status: 'SUCCESS',
          message: null,
          timestamp: null,
        },
      },
    },
  },
  {
    request: {
      query: dashboardOps.SERVER_STATUS_CLUSTER_API_HEALTHZ_WATCH,
      variables: { kubeContext: 'test2' },
    },
    result: {
      data: {
        clusterAPIHealthzWatch: {
          __typename: 'HealthCheckResponse',
          status: 'SUCCESS',
          message: null,
          timestamp: null,
        },
      },
    },
  },
];
