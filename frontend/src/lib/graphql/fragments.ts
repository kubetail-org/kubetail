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

import { gql } from '@/lib/graphql/__generated__/gql';

/**
 * Home list-related fragments
 */

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

export const HOME_NAMESPACES_LIST_ITEM_FRAGMENT = gql(`
  fragment HomeNamespacesListItemFragment on CoreV1Namespace {
    ...HomeGenericListItemFragment
  }
`);

export const HOME_PODS_LIST_ITEM_FRAGMENT = gql(`
  fragment HomePodsListItemFragment on CoreV1Pod {
    ...HomeGenericListItemFragment
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


/**
 * Explorer list-related fragments
 */

export const EXPLORER_GENERIC_LIST_FRAGMENT = gql(`
  fragment ExplorerGenericListFragment on List {
    metadata {
      continue
      resourceVersion
    }
  }
`);

export const EXPLORER_GENERIC_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerGenericListItemFragment on Object {
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

export const EXPLORER_CRONJOBS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerCronJobsListItemFragment on BatchV1CronJob {
    ...ExplorerGenericListItemFragment
    spec {
      schedule
      suspend
    }
    status {
      active {
        __typename
      }
      lastScheduleTime
      lastSuccessfulTime
    }
  }
`);

export const EXPLORER_DAEMONSETS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerDaemonSetsListItemFragment on AppsV1DaemonSet {
    ...ExplorerGenericListItemFragment
    status {
      currentNumberScheduled
      desiredNumberScheduled
    }
  }
`);

export const EXPLORER_DEPLOYMENTS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerDeploymentsListItemFragment on AppsV1Deployment {
    ...ExplorerGenericListItemFragment
    spec {
      replicas
      paused
    }
    status {
      replicas
    }
  }
`);

export const EXPLORER_JOBS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerJobsListItemFragment on BatchV1Job {
    ...ExplorerGenericListItemFragment
  }
`);

export const EXPLORER_PODS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerPodsListItemFragment on CoreV1Pod {
    ...ExplorerGenericListItemFragment
    spec {
      containers {
        name
        image
      }
      nodeName
    }
    status {
      phase
      containerStatuses {
        name
        state {
          running {
            startedAt
          }
          terminated {
            exitCode
          }
        }
        ready
        restartCount
        started
      }
    }
  }
`);

export const EXPLORER_REPLICASETS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerReplicaSetsListItemFragment on AppsV1ReplicaSet {
    ...ExplorerGenericListItemFragment
    spec {
      replicas
    }
    status {
      replicas
    }
  }
`);

export const EXPLORER_STATEFULSETS_LIST_ITEM_FRAGMENT = gql(`
  fragment ExplorerStatefulSetsListItemFragment on AppsV1StatefulSet {
    ...ExplorerGenericListItemFragment

  }
`);


/**
 * Explorer object-related fragments
 */

export const EXPLORER_GENERIC_OBJECT_FRAGMENT = gql(`
  fragment ExplorerGenericObjectFragment on Object {
    id
    metadata {
      creationTimestamp
      deletionTimestamp
      name
      namespace
      labels
      annotations
      ownerReferences {
        apiVersion
        kind
        name
        uid
        controller
      }
      resourceVersion
      uid
    }
  }
`);

export const EXPLORER_CRONJOBS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerCronJobsObjectFragment on BatchV1CronJob {
    ...ExplorerGenericObjectFragment
  }
`);

export const EXPLORER_DAEMONSETS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerDaemonSetsObjectFragment on AppsV1DaemonSet {
    ...ExplorerGenericObjectFragment
  }
`);

export const EXPLORER_DEPLOYMENTS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerDeploymentsObjectFragment on AppsV1Deployment {
    ...ExplorerGenericObjectFragment
    spec {
      replicas
      selector {
        matchLabels
        matchExpressions {
          key
          operator
          values
        }
      }
      paused
    }
    status {
      replicas
    }
  }
`);

export const EXPLORER_JOBS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerJobsObjectFragment on BatchV1Job {
    ...ExplorerGenericObjectFragment
  }
`);

export const EXPLORER_PODS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerPodsObjectFragment on CoreV1Pod {
    ...ExplorerGenericObjectFragment
    spec {
      containers {
        name
        image
      }
      nodeName
      priorityClassName
    }
    status {
      phase
      message
      reason
      containerStatuses {
        name
        state {
          waiting {
            reason
            message
          }
          running {
            startedAt
          }
          terminated {
            exitCode
            signal
            reason
            message
          }
        }
        lastTerminationState {
          waiting {
            reason
            message
          }
          running {
            startedAt
          }
          terminated {
            exitCode
            signal
            reason
            message
          }
        }
        ready
        restartCount
        imageID
        started
      }
    }
  }
`);

export const EXPLORER_REPLICASETS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerReplicaSetsObjectFragment on AppsV1ReplicaSet {
    ...ExplorerGenericObjectFragment
  }
`);

export const EXPLORER_STATEFULSETS_OBJECT_FRAGMENT = gql(`
  fragment ExplorerStatefulSetsObjectFragment on AppsV1StatefulSet {
    ...ExplorerGenericObjectFragment
  }
`);

