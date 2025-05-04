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

import type { MockedResponse } from '@apollo/client/testing';

import * as dashboardOps from '@/lib/graphql/dashboard/ops';
import { WorkloadItem } from '@/pages/home';

const genericListResponse = (typename: string) => ({
  __typename: typename,
  metadata: {
    continue: '',
    resourceVersion: 'v1',
  },
  items: [],
});

export const mocks: MockedResponse[] = [
  // cronjobs
  {
    request: {
      query: dashboardOps.HOME_CRONJOBS_LIST_FETCH,
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { continue: '' },
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
      variables: { resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
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
      variables: { namespace: '', continue: '' },
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
      variables: { namespace: '', resourceVersion: 'v1' },
    },
    result: {
      data: {
        appsV1StatefulSetsWatch: null,
      },
    },
  },

  // healthz
  {
    request: {
      query: dashboardOps.SERVER_STATUS_KUBERNETES_API_HEALTHZ_WATCH,
    },
    result: {
      data: {
        livezWatch: {
          __typename: 'HealthCheckResponse',
          status: 'SUCCESS',
          message: null,
          timestamp: null,
        },
      },
    },
  },
];

export const workloadItemMock: WorkloadItem[] = [
  {
    __typename: 'AppsV1DaemonSet',
    id: 'b19915f5-cfcf-442e-b5a9-7eeea9c6de08',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kube-system',
      name: 'kindnet',
      uid: 'b19915f5-cfcf-442e-b5a9-7eeea9c6de08',
      creationTimestamp: '2025-05-03T11:01:32.000Z',
      deletionTimestamp: null,
      resourceVersion: '621',
      ownerReferences: [],
    },
  },
  {
    __typename: 'AppsV1DaemonSet',
    id: 'f816bf39-fd58-44c0-918c-c56e3be62f59',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kube-system',
      name: 'kube-proxy',
      uid: 'f816bf39-fd58-44c0-918c-c56e3be62f59',
      creationTimestamp: '2025-05-03T11:01:31.000Z',
      deletionTimestamp: null,
      resourceVersion: '564',
      ownerReferences: [],
    },
  },
  {
    __typename: 'AppsV1DaemonSet',
    id: 'b7577341-a44c-494e-bb1a-a45200fe57e3',
    metadata: {
      __typename: 'MetaV1ObjectMeta',
      namespace: 'kubetail-system',
      name: 'kubetail-cluster-agent',
      uid: 'b7577341-a44c-494e-bb1a-a45200fe57e3',
      creationTimestamp: '2025-05-03T11:23:29.000Z',
      deletionTimestamp: '2025-05-03T13:01:31.000Z',
      resourceVersion: '12542',
      ownerReferences: [],
    },
  },
];

export const ownerShipMapMock = new Map([
  ['dc8fbace-67c0-43d5-a59d-aaff6dbe2d7a', ['60c83096-174e-4191-a705-1245b52a0e33', '5955f63b-b69b-45de-b2e1-2eb60e4cd15e', '9b72c3fd-c800-45ed-97e4-d7d965d9cf65']],
  ['fada4cb6-0bb4-46f7-99cd-05498ef732f4', ['93edde53-0bb8-44e6-b271-0022abe42100', 'edc816e9-dea5-4133-b499-89984b9ebb14', '750545e3-2266-4891-b6eb-18bd7c5e4f09']],
  ['db03b586-95df-48f3-aaeb-9e0de42d3926', ['3596ec70-0de7-40a9-90a8-d57f8931ae15', '603414f8-cdec-40dd-bbbe-7ada2473d77c', 'd0e68602-898a-44e3-abce-1d98ab2eee44',
    'e7ab0fb4-ae9e-4ffe-82ec-899254e096b0']],
  ['603414f8-cdec-40dd-bbbe-7ada2473d77c', ['fff3a147-8c0e-489d-983a-2a190b4b1999', 'ab79e4af-dccb-4b9b-9063-eea4a1c40e8c', 'dd4ad9ff-622b-4697-bbab-0b22fdb2031e']],
]);
