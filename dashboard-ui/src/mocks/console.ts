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

export const mocks: MockedResponse[] = [
  // kubeConfig
  {
    request: {
      query: dashboardOps.KUBE_CONFIG_WATCH,
    },
    result: {
      data: {
        kubeConfigWatch: null,
      },
    },
  },

  // nodes
  {
    request: {
      query: dashboardOps.CONSOLE_NODES_LIST_FETCH,
      variables: { kubeContext: '', continue: '' },
    },
    result: {
      data: {
        coreV1NodesList: {
          metadata: {
            continue: '',
            resourceVersion: 'v1',
          },
          items: [],
        },
      },
    },
  },
  {
    request: {
      query: dashboardOps.CONSOLE_NODES_LIST_WATCH,
      variables: { resourceVersion: 'v1' },
    },
    result: {
      data: {
        coreV1NodesWatch: null,
      },
    },
  },

  // log records
  {
    request: {
      query: dashboardOps.LOG_RECORDS_FETCH,
      variables: { kubeContext: null, sources: [], sourceFilter: { region: [], zone: [], os: [], arch: [], node: [], container: [] }, grep: null, limit: 301, after: undefined, before: undefined, since: undefined, mode: 'TAIL' },
    },
    result: {
      data: {
        logRecordsFetch: {
          records: [],
          nextCursor: null,
        },
      },
    },
  },

  {
    request: {
      query: dashboardOps.LOG_RECORDS_FETCH,
      variables: { kubeContext: null, sources: [], limit: 301, mode: 'TAIL' },
    },
    result: {
      data: {
        logRecordsFetch: {
          records: [],
          nextCursor: null,
        },
      },
    },
  },

  // log sources
  {
    request: {
      query: dashboardOps.LOG_SOURCES_WATCH,
      variables: { kubeContext: null, sources: [] },
    },
    result: {
      data: {
        logSourcesWatch: null,
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
