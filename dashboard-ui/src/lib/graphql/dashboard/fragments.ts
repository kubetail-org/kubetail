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

import { gql } from '@/lib/graphql/dashboard/__generated__/gql';

/**
 * Console fragments
 */

export const CONSOLE_LOGGING_RESOURCES_GENERIC_OBJECT_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesGenericObjectFragment on Object {
    id
    metadata {
      namespace
      name
      uid
      creationTimestamp
      deletionTimestamp
      resourceVersion
      ownerReferences {
        name
        uid
        controller
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_CRONJOB_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesCronJobFragment on BatchV1CronJob {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      jobTemplate {
        spec {
          selector {
            matchLabels
          }
        }
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DAEMONSET_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesDaemonSetFragment on AppsV1DaemonSet {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesDeploymentFragment on AppsV1Deployment {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOB_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesJobFragment on BatchV1Job {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_POD_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesPodFragment on CoreV1Pod {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      containers {
        name
      }
      nodeName
    }
    status {
      containerStatuses {
        name
        started
        state {
          running {
            startedAt
          }
          terminated {
            exitCode
          }
        }
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_REPLICASET_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesReplicaSetFragment on AppsV1ReplicaSet {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_STATEFULSET_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesStatefulSetFragment on AppsV1StatefulSet {
    ...ConsoleLoggingResourcesGenericObjectFragment
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOBS_FIND_FRAGMENT = gql(`
  fragment ConsoleLoggingResourcesJobsFindFragment on BatchV1Job {
    id
    metadata {
      namespace
      name
      uid
      deletionTimestamp
      resourceVersion
      ownerReferences {
        name
        uid
        controller
      }
    }
    spec {
      selector {
        matchLabels
      }
    }
  }
`);

export const CONSOLE_NODES_LIST_ITEM_FRAGMENT = gql(`
  fragment ConsoleNodesListItemFragment on CoreV1Node {
    id
    metadata {
      name
      uid
      creationTimestamp
      deletionTimestamp
      resourceVersion
      labels
      annotations
    }
  }
`);

/**
 * Health check fragments
 */

export const HEALTH_CHECK_RESPONSE_FRAGMENT = gql(`
  fragment HealthCheckResponseFragment on HealthCheckResponse {
    status
    message
    timestamp
  }
`);

/**
 * Helm fragments
 */

export const HELM_RELEASE_FRAGMENT = gql(`
  fragment HelmReleaseFragment on HelmRelease {
    name
    version
    namespace
    chart {
      metadata {
        name
        version
        appVersion
      }
    }
  }
`);

/**
 * Home page fragments
 */

export const HOME_CRONJOBS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeCronJobsListItemFragment on BatchV1CronJob {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_DAEMONSETS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeDaemonSetsListItemFragment on AppsV1DaemonSet {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_DEPLOYMENTS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeDeploymentsListItemFragment on AppsV1Deployment {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_JOBS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeJobsListItemFragment on BatchV1Job {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_GENERIC_LIST_FRAGMENT = gql(`
  fragment HomeGenericListFragment on List {
    metadata {
      continue
      resourceVersion
    }
  }
`);

export const HOME_GENERIC_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeGenericListItemFragment on Object {
    id
    metadata {
      namespace
      name
      uid
      creationTimestamp
      deletionTimestamp
      resourceVersion
      ownerReferences {
        name
        uid
        controller
      }
    }
  }
`);

export const HOME_NAMESPACES_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeNamespacesListItemFragment on CoreV1Namespace {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_PODS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomePodsListItemFragment on CoreV1Pod {
    ...HomeGenericListItemFragment
    status {
      containerStatuses {
        containerID
        started
      }
    }
  }
`);

export const HOME_REPLICASETS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeReplicaSetsListItemFragment on AppsV1ReplicaSet {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_STATEFULSETS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeStatefulSetsListItemFragment on AppsV1StatefulSet {
    ...HomeGenericListItemFragment
  }
`);

/**
 * KubeConfig fragments
 */

export const KUBE_CONFIG_FRAGMENT = gql(`
  fragment KubeConfigFragment on KubeConfig {
    currentContext
    contexts {
      name
      cluster
      namespace
    }
  }
`);

/**
 * Pod log fragments
 */

export const POD_LOG_QUERY_RESPONSE_FRAGMENT = gql(`
  fragment PodLogQueryResponseFragment on PodLogQueryResponse {
    results {
      timestamp
      message
    }
    pageInfo {
      hasPreviousPage
      hasNextPage
      startCursor
      endCursor
    }
  }
`);

/**
 * Source-Picker fragments
 */

export const SOURCE_PICKER_GENERIC_COUNTER_FRAGMENT = gql(`
  fragment SourcePickerGenericCounterFragment on List {
    metadata {
      remainingItemCount
      resourceVersion
    }
    items {
      ...SourcePickerGenericCounterItemFragment
    }
  }
`);

export const SOURCE_PICKER_GENERIC_COUNTER_ITEM_FRAGMENT = gql(`
  fragment SourcePickerGenericCounterItemFragment on Object {
    id
    metadata {
      resourceVersion
    }
  }
`);

export const SOURCE_PICKER_GENERIC_LIST_FRAGMENT = gql(`
  fragment SourcePickerGenericListFragment on List {
    metadata {
      continue
      resourceVersion
    }
  }
`);

export const SOURCE_PICKER_GENERIC_LIST_ITEM_FRAGMENT = gql(`
  fragment SourcePickerGenericListItemFragment on Object {
    id
    metadata {
      namespace
      name
      uid
      creationTimestamp
      deletionTimestamp
      resourceVersion
    }
  }
`);
