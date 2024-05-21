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
 * Home list-related queries
 */

export const HOME_CRONJOBS_LIST_FETCH = gql(`
  query HomeCronJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1CronJobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeCronJobsListItemFragment
      }
    }
  }
`);

export const HOME_CRONJOBS_LIST_WATCH = gql(`
  subscription HomeCronJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeCronJobsListItemFragment
      }
    }
  }
`);

export const HOME_DAEMONSETS_LIST_FETCH = gql(`
  query HomeDaemonSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DaemonSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeDaemonSetsListItemFragment
      }
    }
  }
`);

export const HOME_DAEMONSETS_LIST_WATCH = gql(`
  subscription HomeDaemonSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeDaemonSetsListItemFragment
      }
    }
  }
`);

export const HOME_DEPLOYMENTS_LIST_FETCH = gql(`
  query HomeDeploymentsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DeploymentsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeDeploymentsListItemFragment
      }
    }
  }
`);

export const HOME_DEPLOYMENTS_LIST_WATCH = gql(`
  subscription HomeDeploymentsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeDeploymentsListItemFragment
      }
    }
  }
`);

export const HOME_JOBS_LIST_FETCH = gql(`
  query HomeJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1JobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeJobsListItemFragment
      }
    }
  }
`);

export const HOME_JOBS_LIST_WATCH = gql(`
  subscription HomeJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeJobsListItemFragment
      }
    }
  }
`);

export const HOME_NAMESPACES_LIST_FETCH = gql(`
  query HomeNamespacesListFetch($continue: String = "") {
    coreV1NamespacesList(options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeNamespacesListItemFragment
      }
    }
  }
`);

export const HOME_NAMESPACES_LIST_WATCH = gql(`
  subscription HomeNamespacesListWatch($resourceVersion: String = "") {
    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeNamespacesListItemFragment
      }
    }
  }
`);

export const HOME_PODS_LIST_FETCH = gql(`
  query HomePodsListFetch($namespace: String = "", $continue: String = "") {
    coreV1PodsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomePodsListItemFragment
      }
    }
  }
`);

export const HOME_PODS_LIST_WATCH = gql(`
  subscription HomePodsListWatch($namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomePodsListItemFragment
      }
    }
  }
`);

export const HOME_REPLICASETS_LIST_FETCH = gql(`
  query HomeReplicaSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeReplicaSetsListItemFragment
      }
    }
  }
`);

export const HOME_REPLICASETS_LIST_WATCH = gql(`
  subscription HomeReplicaSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeReplicaSetsListItemFragment
      }
    }
  }
`);

export const HOME_STATEFULSETS_LIST_FETCH = gql(`
  query HomeStatefulSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1StatefulSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);

export const HOME_STATEFULSETS_LIST_WATCH = gql(`
  subscription HomeStatefulSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);

/**
 * Console queries
 */

export const CONSOLE_LOGGING_RESOURCES_CRONJOB_GET = gql(`
  query ConsoleLoggingResourcesCronJobGet($namespace: String!, $name: String!) {
    batchV1CronJobsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesCronJobFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_CRONJOB_WATCH = gql(`
  subscription ConsoleLoggingResourcesCronJobWatch($namespace: String!, $fieldSelector: String!) {
    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesCronJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DAEMONSET_GET = gql(`
  query ConsoleLoggingResourcesDaemonSetGet($namespace: String!, $name: String!) {
    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesDaemonSetFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DAEMONSET_WATCH = gql(`
  subscription ConsoleLoggingResourcesDaemonSetWatch($namespace: String!, $fieldSelector: String!) {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesDaemonSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_GET = gql(`
  query ConsoleLoggingResourcesDeploymentGet($namespace: String!, $name: String!) {
    appsV1DeploymentsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesDeploymentFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_WATCH = gql(`
  subscription ConsoleLoggingResourcesDeploymentWatch($namespace: String!, $fieldSelector: String!) {
    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesDeploymentFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOB_GET = gql(`
  query ConsoleLoggingResourcesJobGet($namespace: String!, $name: String!) {
    batchV1JobsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesJobFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOB_WATCH = gql(`
  subscription ConsoleLoggingResourcesJobWatch($namespace: String!, $fieldSelector: String!) {
    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_POD_GET = gql(`
  query ConsoleLoggingResourcesPodGet($namespace: String!, $name: String!) {
    coreV1PodsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesPodFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_POD_WATCH = gql(`
  subscription ConsoleLoggingResourcesPodWatch($namespace: String!, $fieldSelector: String!) {
    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_LIST_FETCH = gql(`
  query ConsolePodsListFetch($namespace: String!, $continue: String = "") {
    coreV1PodsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      metadata {
        continue
        resourceVersion
      }
      items {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_LIST_WATCH = gql(`
  subscription ConsolePodsListWatch($namespace: String!, $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_REPLICASET_GET = gql(`
  query ConsoleLoggingResourcesReplicaSetGet($namespace: String!, $name: String!) {
    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesReplicaSetFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_REPLICASET_WATCH = gql(`
  subscription ConsoleLoggingResourcesReplicaSetWatch($namespace: String!, $fieldSelector: String!) {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesReplicaSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_STATEFULSET_GET = gql(`
  query ConsoleLoggingResourcesStatefulSetGet($namespace: String!, $name: String!) {
    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesStatefulSetFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_STATEFULSET_WATCH = gql(`
  subscription ConsoleLoggingResourcesStatefulSetWatch($namespace: String!, $fieldSelector: String!) {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesStatefulSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOBS_FIND = gql(`
  query ConsoleLoggingResourcesJobsFind($namespace: String!, $continue: String = "") {
    batchV1JobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      metadata {
        continue
        resourceVersion
      }
      items {
        ...ConsoleLoggingResourcesJobsFindFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOBS_WATCH = gql(`
  subscription ConsoleStreamsJobsWatch($namespace: String!, $resourceVersion: String = "") {
    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_FIND = gql(`
  query ConsoleLoggingResourcesPodsFind($namespace: String!, $labelSelector: String!, $continue: String = "") {
    coreV1PodsList(namespace: $namespace, options: { labelSelector: $labelSelector, continue: $continue }) {
      metadata {
        continue
        resourceVersion
      }
      items {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_WATCH = gql(`
  subscription ConsoleLoggingResourcesPodsWatch($namespace: String!, $labelSelector: String!, $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { labelSelector: $labelSelector, resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_NODES_LIST_FETCH = gql(`
  query ConsoleNodesListFetch($continue: String = "") {
    coreV1NodesList(options: { limit: "50", continue: $continue }) {
      metadata {
        continue
        resourceVersion
      }
      items {
        ...ConsoleNodesListItemFragment
      }
    }
  }
`);

export const CONSOLE_NODES_LIST_WATCH = gql(`
  subscription ConsoleNodesListWatch($resourceVersion: String = "") {
    coreV1NodesWatch(options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleNodesListItemFragment
      }
    }
  }
`);

/**
 * Source picker queries
 */

export const SOURCE_PICKER_CRONJOBS_COUNT_FETCH = gql(`
  query SourcePickerCronJobsCountFetch($namespace: String = "") {
    batchV1CronJobsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerCronJobsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_COUNT_WATCH = gql(`
  subscription SourcePickerCronJobsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_COUNT_FETCH = gql(`
  query SourcePickerDaemonSetsCountFetch($namespace: String = "") {
    appsV1DaemonSetsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerDaemonSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_COUNT_WATCH = gql(`
  subscription SourcePickerDaemonSetsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_COUNT_FETCH = gql(`
  query SourcePickerDeploymentsCountFetch($namespace: String = "") {
    appsV1DeploymentsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerDeploymentsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_COUNT_WATCH = gql(`
  subscription SourcePickerDeploymentsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_COUNT_FETCH = gql(`
  query SourcePickerJobsCountFetch($namespace: String = "") {
    batchV1JobsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerJobsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_COUNT_WATCH = gql(`
  subscription SourcePickerJobsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_COUNT_FETCH = gql(`
  query SourcePickerPodsCountFetch($namespace: String = "") {
    coreV1PodsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerPodsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_COUNT_WATCH = gql(`
  subscription SourcePickerPodsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_COUNT_FETCH = gql(`
  query SourcePickerReplicaSetsCountFetch($namespace: String = "") {
    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerReplicaSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_COUNT_WATCH = gql(`
  subscription SourcePickerReplicaSetsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_COUNT_FETCH = gql(`
  query SourcePickerStatefulSetsCountFetch($namespace: String = "") {
    appsV1StatefulSetsList(namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerStatefulSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_COUNT_WATCH = gql(`
  subscription SourcePickerStatefulSetsCountWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_LIST_FETCH = gql(`
  query SourcePickerCronJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1CronJobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerCronJobsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_LIST_WATCH = gql(`
  subscription SourcePickerCronJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_LIST_FETCH = gql(`
  query SourcePickerDaemonSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DaemonSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerDaemonSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_LIST_WATCH = gql(`
  subscription SourcePickerDaemonSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH = gql(`
  query SourcePickerDeploymentsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DeploymentsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerDeploymentsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_LIST_WATCH = gql(`
  subscription SourcePickerDeploymentsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_LIST_FETCH = gql(`
  query SourcePickerJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1JobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerJobsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_LIST_WATCH = gql(`
  subscription SourcePickerJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_NAMESPACES_LIST_FETCH = gql(`
  query SourcePickerNamespacesListFetch($continue: String = "") {
    coreV1NamespacesList(options: { limit: "50", continue: $continue }) {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_NAMESPACES_LIST_WATCH = gql(`
  subscription SourcePickerNamespacesListWatch($resourceVersion: String = "") {
    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_LIST_FETCH = gql(`
  query SourcePickerPodsListFetch($namespace: String = "", $continue: String = "") {
    coreV1PodsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerPodsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_LIST_WATCH = gql(`
  subscription SourcePickerPodsListWatch($namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_LIST_FETCH = gql(`
  query SourcePickerReplicaSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerReplicaSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_LIST_WATCH = gql(`
  subscription SourcePickerReplicaSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_LIST_FETCH = gql(`
  query SourcePickerStatefulSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1StatefulSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerStatefulSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_LIST_WATCH = gql(`
  subscription SourcePickerStatefulSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);

/**
 * Explorer list-related queries
 */

export const EXPLORER_CRONJOBS_LIST_FETCH = gql(`
  query ExplorerCronJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1CronJobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerCronJobsListItemFragment
      }
    }
  }
`);

export const EXPLORER_CRONJOBS_LIST_WATCH = gql(`
  subscription ExplorerCronJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerCronJobsListItemFragment
      }
    }
  }
`);

export const EXPLORER_DAEMONSETS_LIST_FETCH = gql(`
  query ExplorerDaemonSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DaemonSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerDaemonSetsListItemFragment
      }
    }
  }
`);

export const EXPLORER_DAEMONSETS_LIST_WATCH = gql(`
  subscription ExplorerDaemonSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerDaemonSetsListItemFragment
      }
    }
  }
`);

export const EXPLORER_DEPLOYMENTS_LIST_FETCH = gql(`
  query ExplorerDeploymentsListFetch($namespace: String = "", $continue: String = "") {
    appsV1DeploymentsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerDeploymentsListItemFragment
      }
    }
  }
`);

export const EXPLORER_DEPLOYMENTS_LIST_WATCH = gql(`
  subscription ExplorerDeploymentsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerDeploymentsListItemFragment
      }
    }
  }
`);

export const EXPLORER_JOBS_LIST_FETCH = gql(`
  query ExplorerJobsListFetch($namespace: String = "", $continue: String = "") {
    batchV1JobsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerJobsListItemFragment
      }
    }
  }
`);

export const EXPLORER_JOBS_LIST_WATCH = gql(`
  subscription ExplorerJobsListWatch($namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerJobsListItemFragment
      }
    }
  }
`);

export const EXPLORER_PODS_LIST_FETCH = gql(`
  query ExplorerPodsListFetch($namespace: String = "", $continue: String = "") {
    coreV1PodsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerPodsListItemFragment
      }
    }
  }
`);

export const EXPLORER_PODS_LIST_WATCH = gql(`
  subscription ExplorerPodsListWatch($namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerPodsListItemFragment
      }
    }
  }
`);

export const EXPLORER_REPLICASETS_LIST_FETCH = gql(`
  query ExplorerReplicaSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerReplicaSetsListItemFragment
      }
    }
  }
`);

export const EXPLORER_REPLICASETS_LIST_WATCH = gql(`
  subscription ExplorerReplicaSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerReplicaSetsListItemFragment
      }
    }
  }
`);

export const EXPLORER_STATEFULSETS_LIST_FETCH = gql(`
  query ExplorerStatefulSetsListFetch($namespace: String = "", $continue: String = "") {
    appsV1StatefulSetsList(namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...ExplorerGenericListFragment
      items {
        ...ExplorerStatefulSetsListItemFragment
      }
    }
  }
`);

export const EXPLORER_STATEFULSETS_LIST_WATCH = gql(`
  subscription ExplorerStatefulSetsListWatch($namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ExplorerStatefulSetsListItemFragment
      }
    }
  }
`);

/**
 * Explorer object-related queries
 */

export const EXPLORER_CRONJOBS_OBJECT_FETCH = gql(`
  query ExplorerCronJobsObjectFetch($namespace: String!, $name: String!) {
    batchV1CronJobsGet(namespace: $namespace, name: $name) {
      ...ExplorerCronJobsObjectFragment
    }
  }
`);

export const EXPLORER_CRONJOBS_OBJECT_WATCH = gql(`
  subscription ExplorerCronJobsObjectWatch($namespace: String!, $fieldSelector: String!) {
    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerCronJobsObjectFragment
      }
    }
  }
`);

export const EXPLORER_DAEMONSETS_OBJECT_FETCH = gql(`
  query ExplorerDaemonSetsObjectFetch($namespace: String!, $name: String!) {
    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {
      ...ExplorerDaemonSetsObjectFragment
    }
  }
`);

export const EXPLORER_DAEMONSETS_OBJECT_WATCH = gql(`
  subscription ExplorerDaemonSetsObjectWatch($namespace: String!, $fieldSelector: String!) {
    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerDaemonSetsObjectFragment
      }
    }
  }
`);

export const EXPLORER_DEPLOYMENTS_OBJECT_FETCH = gql(`
  query ExplorerDeploymentsObjectFetch($namespace: String!, $name: String!) {
    appsV1DeploymentsGet(namespace: $namespace, name: $name) {
      ...ExplorerDeploymentsObjectFragment
    }
  }
`);

export const EXPLORER_DEPLOYMENTS_OBJECT_WATCH = gql(`
  subscription ExplorerDeploymentsObjectWatch($namespace: String!, $fieldSelector: String!) {
    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerDeploymentsObjectFragment
      }
    }
  }
`);

export const EXPLORER_JOBS_OBJECT_FETCH = gql(`
  query ExplorerJobsObjectFetch($namespace: String!, $name: String!) {
    batchV1JobsGet(namespace: $namespace, name: $name) {
      ...ExplorerJobsObjectFragment
    }
  }
`);

export const EXPLORER_JOBS_OBJECT_WATCH = gql(`
  subscription ExplorerJobsObjectWatch($namespace: String!, $fieldSelector: String!) {
    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerJobsObjectFragment
      }
    }
  }
`);

export const EXPLORER_PODS_OBJECT_FETCH = gql(`
  query ExplorerPodsObjectFetch($namespace: String!, $name: String!) {
    coreV1PodsGet(namespace: $namespace, name: $name) {
      ...ExplorerPodsObjectFragment
    }
  }
`);

export const EXPLORER_PODS_OBJECT_WATCH = gql(`
  subscription ExplorerPodsObjectWatch($namespace: String!, $fieldSelector: String!) {
    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerPodsObjectFragment
      }
    }
  }
`);

export const EXPLORER_REPLICASETS_OBJECT_FETCH = gql(`
  query ExplorerReplicaSetsObjectFetch($namespace: String!, $name: String!) {
    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {
      ...ExplorerReplicaSetsObjectFragment
    }
  }
`);

export const EXPLORER_REPLICASETS_OBJECT_WATCH = gql(`
  subscription ExplorerReplicaSetsObjectWatch($namespace: String!, $fieldSelector: String!) {
    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerReplicaSetsObjectFragment
      }
    }
  }
`);

export const EXPLORER_STATEFULSETS_OBJECT_FETCH = gql(`
  query ExplorerStatefulSetsObjectFetch($namespace: String!, $name: String!) {
    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {
      ...ExplorerStatefulSetsObjectFragment
    }
  }
`);

export const EXPLORER_STATEFULSETS_OBJECT_WATCH = gql(`
  subscription ExplorerStatefulSetsObjectWatch($namespace: String!, $fieldSelector: String!) {
    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ExplorerStatefulSetsObjectFragment
      }
    }
  }
`);

/**
 * Logs
 */

export const HEAD_CONTAINER_LOG = gql(`
  query HeadContainerLog($namespace: String!, $name: String!, $container: String, $after: ID, $since: String, $first: Int) {
    podLogHead(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, first: $first) {
      ...PodLogQueryResponseFragment
    }
  }
`);

export const TAIL_CONTAINER_LOG = gql(`
  query TailContainerLog($namespace: String!, $name: String!, $container: String, $before: ID, $last: Int) {
    podLogTail(namespace: $namespace, name: $name, container: $container, before: $before, last: $last) {
      ...PodLogQueryResponseFragment
    }
  }
`);

export const FOLLOW_CONTAINER_LOG = gql(`
  subscription FollowContainerLog($namespace: String!, $name: String!, $container: String, $after: ID, $since: String) {
    podLogFollow(namespace: $namespace, name: $name, container: $container, after: $after, since: $since) {
      timestamp
      message
    }
  }
`);

/**
 * Health checks
 */

export const LIVEZ_WATCH = gql(`
  subscription LivezWatch {
    livezWatch {
      status
      message
      timestamp
    }
  }
`);

export const READYZ_WATCH = gql(`
  subscription ReadyzWatch {
    readyzWatch {
      status
      message
      timestamp
    }
  }
`);
