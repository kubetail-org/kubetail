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
 * Console queries
 */

export const CONSOLE_LOGGING_RESOURCES_CRONJOB_GET = gql(`
  query ConsoleLoggingResourcesCronJobGet($kubeContext: String!, $namespace: String!, $name: String!) {
    batchV1CronJobsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesCronJobFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_CRONJOB_WATCH = gql(`
  subscription ConsoleLoggingResourcesCronJobWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    batchV1CronJobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesCronJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DAEMONSET_GET = gql(`
  query ConsoleLoggingResourcesDaemonSetGet($kubeContext: String!, $namespace: String!, $name: String!) {
    appsV1DaemonSetsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesDaemonSetFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DAEMONSET_WATCH = gql(`
  subscription ConsoleLoggingResourcesDaemonSetWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    appsV1DaemonSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesDaemonSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_GET = gql(`
  query ConsoleLoggingResourcesDeploymentGet($kubeContext: String!, $namespace: String!, $name: String!) {
    appsV1DeploymentsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesDeploymentFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_DEPLOYMENT_WATCH = gql(`
  subscription ConsoleLoggingResourcesDeploymentWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    appsV1DeploymentsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesDeploymentFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOB_GET = gql(`
  query ConsoleLoggingResourcesJobGet($kubeContext: String!, $namespace: String!, $name: String!) {
    batchV1JobsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesJobFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOB_WATCH = gql(`
  subscription ConsoleLoggingResourcesJobWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    batchV1JobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_POD_GET = gql(`
  query ConsoleLoggingResourcesPodGet($kubeContext: String!, $namespace: String!, $name: String!) {
    coreV1PodsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesPodFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_POD_WATCH = gql(`
  subscription ConsoleLoggingResourcesPodWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_LIST_FETCH = gql(`
  query ConsolePodsListFetch($kubeContext: String!, $namespace: String!, $continue: String = "") {
    coreV1PodsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
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
  subscription ConsolePodsListWatch($kubeContext: String!, $namespace: String!, $resourceVersion: String = "") {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_REPLICASET_GET = gql(`
  query ConsoleLoggingResourcesReplicaSetGet($kubeContext: String!, $namespace: String!, $name: String!) {
    appsV1ReplicaSetsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesReplicaSetFragment
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_REPLICASET_WATCH = gql(`
  subscription ConsoleLoggingResourcesReplicaSetWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    appsV1ReplicaSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesReplicaSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_STATEFULSET_GET = gql(`
  query ConsoleLoggingResourcesStatefulSetGet($kubeContext: String!, $namespace: String!, $name: String!) {
    appsV1StatefulSetsGet(kubeContext: $kubeContext, namespace: $namespace, name: $name) {
      ...ConsoleLoggingResourcesStatefulSetFragment
    }    
  }
`);

export const CONSOLE_LOGGING_RESOURCES_STATEFULSET_WATCH = gql(`
  subscription ConsoleLoggingResourcesStatefulSetWatch($kubeContext: String!, $namespace: String!, $fieldSelector: String!) {
    appsV1StatefulSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { fieldSelector: $fieldSelector }) {
      type
      object {
        ...ConsoleLoggingResourcesStatefulSetFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_JOBS_FIND = gql(`
  query ConsoleLoggingResourcesJobsFind($kubeContext: String!, $namespace: String!, $continue: String = "") {
    batchV1JobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
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
  subscription ConsoleStreamsJobsWatch($kubeContext: String!, $namespace: String!, $resourceVersion: String = "") {
    batchV1JobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesJobFragment
      }
    }
  }
`);

export const CONSOLE_LOGGING_RESOURCES_PODS_FIND = gql(`
  query ConsoleLoggingResourcesPodsFind($kubeContext: String!, $namespace: String!, $labelSelector: String!, $continue: String = "") {
    coreV1PodsList(kubeContext: $kubeContext, namespace: $namespace, options: { labelSelector: $labelSelector, continue: $continue }) {
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
  subscription ConsoleLoggingResourcesPodsWatch($kubeContext: String!, $namespace: String!, $labelSelector: String!, $resourceVersion: String = "") {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { labelSelector: $labelSelector, resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleLoggingResourcesPodFragment
      }
    }
  }
`);

export const CONSOLE_NODES_LIST_FETCH = gql(`
  query ConsoleNodesListFetch($kubeContext: String!, $continue: String = "") {
    coreV1NodesList(kubeContext: $kubeContext, options: { limit: "50", continue: $continue }) {
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
  subscription ConsoleNodesListWatch($kubeContext: String!, $resourceVersion: String = "") {
    coreV1NodesWatch(kubeContext: $kubeContext, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ConsoleNodesListItemFragment
      }
    }
  }
`);

/**
 * Home page queries
 */

export const HOME_CRONJOBS_LIST_FETCH = gql(`
  query HomeCronJobsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    batchV1CronJobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeCronJobsListItemFragment
      }
    }
  }
`);

export const HOME_CRONJOBS_LIST_WATCH = gql(`
  subscription HomeCronJobsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeCronJobsListItemFragment
      }
    }
  }
`);

export const HOME_DAEMONSETS_LIST_FETCH = gql(`
  query HomeDaemonSetsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    appsV1DaemonSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeDaemonSetsListItemFragment
      }
    }
  }
`);

export const HOME_DAEMONSETS_LIST_WATCH = gql(`
  subscription HomeDaemonSetsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeDaemonSetsListItemFragment
      }
    }
  }
`);

export const HOME_DEPLOYMENTS_LIST_FETCH = gql(`
  query HomeDeploymentsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    appsV1DeploymentsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeDeploymentsListItemFragment
      }
    }
  }
`);

export const HOME_DEPLOYMENTS_LIST_WATCH = gql(`
  subscription HomeDeploymentsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeDeploymentsListItemFragment
      }
    }
  }
`);

export const HOME_JOBS_LIST_FETCH = gql(`
  query HomeJobsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    batchV1JobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeJobsListItemFragment
      }
    }
  }
`);

export const HOME_JOBS_LIST_WATCH = gql(`
  subscription HomeJobsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeJobsListItemFragment
      }
    }
  }
`);

export const HOME_NAMESPACES_LIST_FETCH = gql(`
  query HomeNamespacesListFetch($kubeContext: String, $continue: String = "") {
    coreV1NamespacesList(kubeContext: $kubeContext, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeNamespacesListItemFragment
      }
    }
  }
`);

export const HOME_NAMESPACES_LIST_WATCH = gql(`
  subscription HomeNamespacesListWatch($kubeContext: String, $resourceVersion: String = "") {
    coreV1NamespacesWatch(kubeContext: $kubeContext, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeNamespacesListItemFragment
      }
    }
  }
`);

export const HOME_PODS_LIST_FETCH = gql(`
  query HomePodsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    coreV1PodsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomePodsListItemFragment
      }
    }
  }
`);

export const HOME_PODS_LIST_WATCH = gql(`
  subscription HomePodsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomePodsListItemFragment
      }
    }
  }
`);

export const HOME_REPLICASETS_LIST_FETCH = gql(`
  query HomeReplicaSetsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    appsV1ReplicaSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeReplicaSetsListItemFragment
      }
    }
  }
`);

export const HOME_REPLICASETS_LIST_WATCH = gql(`
  subscription HomeReplicaSetsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeReplicaSetsListItemFragment
      }
    }
  }
`);

export const HOME_STATEFULSETS_LIST_FETCH = gql(`
  query HomeStatefulSetsListFetch($kubeContext: String, $namespace: String = "", $continue: String = "") {
    appsV1StatefulSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) {
      ...HomeGenericListFragment
      items {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);

export const HOME_STATEFULSETS_LIST_WATCH = gql(`
  subscription HomeStatefulSetsListWatch($kubeContext: String, $namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);

/**
 * Cluster API
 */

export const CLUSTER_API_READY_WAIT = gql(`
  subscription ClusterAPIReadyWait($kubeContext: String!, $namespace: String!, $serviceName: String!) {
    clusterAPIReadyWait(kubeContext: $kubeContext, namespace: $namespace, serviceName: $serviceName)
  }
`);

export const CLUSTER_API_SERVICES_LIST_FETCH = gql(`
  query ClusterAPIServicesListFetch($kubeContext: String, $continue: String = "") {
    clusterAPIServicesList(kubeContext: $kubeContext, options: { limit: "50", continue: $continue }) {
      metadata {
        continue
        resourceVersion
      }
      items {
        ...ClusterAPIServicesListItemFragment
      }
    }
  }
`);

export const CLUSTER_API_SERVICES_LIST_WATCH = gql(`
  subscription ClusterAPIServicesListWatch($kubeContext: String, $resourceVersion: String = "") {
    clusterAPIServicesWatch(kubeContext: $kubeContext, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...ClusterAPIServicesListItemFragment
      }
    }    
  }
`);

/**
 * Helm API
 */

export const HELM_INSTALL_LATEST = gql(`
  mutation HelmInstallLatest($kubeContext: String) {
    helmInstallLatest(kubeContext: $kubeContext) {
      ...HelmReleaseFragment
    }
  }
`);

export const HELM_LIST_RELEASES = gql(`
  query HelmListReleases($kubeContext: String) {
    helmListReleases(kubeContext: $kubeContext) {
      ...HelmReleaseFragment
    }
  }
`);

/**
 * KubeConfig queries
 */

export const KUBE_CONFIG_GET = gql(`
  query KubeConfigGet {
    kubeConfigGet {
      ...KubeConfigFragment
    }
  }
`);

export const KUBE_CONFIG_WATCH = gql(`
  subscription KubeConfigWatch {
    kubeConfigWatch {
      type
      object {
        ...KubeConfigFragment
      }
    }
  }
`);

/**
 * Kubernetes API
 */

export const KUBERNETES_API_READY_WAIT = gql(`
  subscription KubernetesAPIReadyWait($kubeContext: String) {
    kubernetesAPIReadyWait(kubeContext: $kubeContext)
  }
`);

/**
 * Log records queries
 */

export const LOG_RECORDS_FETCH = gql(`
  query LogRecordsFetch($kubeContext: String, $sources: [String!]!, $mode: LogRecordsQueryMode, $since: String, $until: String, $after: String, $before: String, $grep: String, $sourceFilter: LogSourceFilter, $limit: Int) {
    logRecordsFetch(kubeContext: $kubeContext, sources: $sources, mode: $mode, since: $since, until: $until, after: $after, before: $before, grep: $grep, sourceFilter: $sourceFilter, limit: $limit) {
      records {
        ...LogRecordsFragment
      }
      nextCursor
    }
  }
`);

export const LOG_RECORDS_FOLLOW = gql(`
  subscription LogRecordsFollow($kubeContext: String, $sources: [String!]!, $since: String, $after: String, $grep: String, $sourceFilter: LogSourceFilter) {
    logRecordsFollow(kubeContext: $kubeContext, sources: $sources, since: $since, after: $after, grep: $grep, sourceFilter: $sourceFilter) {
      ...LogRecordsFragment
    }
  }
`);

/**
 * Log sources queries
 */

export const LOG_SOURCES_WATCH = gql(`
  subscription LogSourcesWatch($kubeContext: String, $sources: [String!]!) {
    logSourcesWatch(kubeContext: $kubeContext, sources: $sources) {
      type
      object {
        ...LogSourceFragment
      }
    }
  }
`);

/**
 * Server status queries
 */

export const SERVER_STATUS_KUBERNETES_API_HEALTHZ_GET = gql(`
  query ServerStatusKubernetesAPIHealthzGet($kubeContext: String!) {
    kubernetesAPIHealthzGet(kubeContext: $kubeContext) {
      ...HealthCheckResponseFragment
    }
  }
`);

export const SERVER_STATUS_KUBERNETES_API_HEALTHZ_WATCH = gql(`
  subscription ServerStatusKubernetesAPIHealthzWatch($kubeContext: String!) {
    kubernetesAPIHealthzWatch(kubeContext: $kubeContext) {
      ...HealthCheckResponseFragment
    }
  }
`);

export const SERVER_STATUS_CLUSTER_API_HEALTHZ_GET = gql(`
  query ServerStatusClusterAPIHealthzGet($kubeContext: String!, $namespace: String, $serviceName: String) {
    clusterAPIHealthzGet(kubeContext: $kubeContext, namespace: $namespace, serviceName: $serviceName) {
      ...HealthCheckResponseFragment
    }
  }
`);

export const SERVER_STATUS_CLUSTER_API_HEALTHZ_WATCH = gql(`
  subscription ServerStatusClusterAPIHealthzWatch($kubeContext: String!, $namespace: String, $serviceName: String) {
    clusterAPIHealthzWatch(kubeContext: $kubeContext, namespace: $namespace, serviceName: $serviceName) {
      ...HealthCheckResponseFragment
    }
  }
`);

/**
 * Source picker queries
 */

export const SOURCE_PICKER_CRONJOBS_COUNT_FETCH = gql(`
  query SourcePickerCronJobsCountFetch($kubeContext: String!, $namespace: String = "") {
    batchV1CronJobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerCronJobsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_COUNT_WATCH = gql(`
  subscription SourcePickerCronJobsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_COUNT_FETCH = gql(`
  query SourcePickerDaemonSetsCountFetch($kubeContext: String!, $namespace: String = "") {
    appsV1DaemonSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerDaemonSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_COUNT_WATCH = gql(`
  subscription SourcePickerDaemonSetsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_COUNT_FETCH = gql(`
  query SourcePickerDeploymentsCountFetch($kubeContext: String!, $namespace: String = "") {
    appsV1DeploymentsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerDeploymentsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_COUNT_WATCH = gql(`
  subscription SourcePickerDeploymentsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_COUNT_FETCH = gql(`
  query SourcePickerJobsCountFetch($kubeContext: String!, $namespace: String = "") {
    batchV1JobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerJobsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_COUNT_WATCH = gql(`
  subscription SourcePickerJobsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_COUNT_FETCH = gql(`
  query SourcePickerPodsCountFetch($kubeContext: String!, $namespace: String = "") {
    coreV1PodsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerPodsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_COUNT_WATCH = gql(`
  subscription SourcePickerPodsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_COUNT_FETCH = gql(`
  query SourcePickerReplicaSetsCountFetch($kubeContext: String!, $namespace: String = "") {
    appsV1ReplicaSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerReplicaSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_COUNT_WATCH = gql(`
  subscription SourcePickerReplicaSetsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_COUNT_FETCH = gql(`
  query SourcePickerStatefulSetsCountFetch($kubeContext: String!, $namespace: String = "") {
    appsV1StatefulSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "1" }) @connection(key: "SourcePickerStatefulSetsCountFetch") {
      ...SourcePickerGenericCounterFragment
      items {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_COUNT_WATCH = gql(`
  subscription SourcePickerStatefulSetsCountWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericCounterItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_LIST_FETCH = gql(`
  query SourcePickerCronJobsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    batchV1CronJobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerCronJobsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_CRONJOBS_LIST_WATCH = gql(`
  subscription SourcePickerCronJobsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    batchV1CronJobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_LIST_FETCH = gql(`
  query SourcePickerDaemonSetsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    appsV1DaemonSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerDaemonSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DAEMONSETS_LIST_WATCH = gql(`
  subscription SourcePickerDaemonSetsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DaemonSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_LIST_FETCH = gql(`
  query SourcePickerDeploymentsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    appsV1DeploymentsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerDeploymentsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_DEPLOYMENTS_LIST_WATCH = gql(`
  subscription SourcePickerDeploymentsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1DeploymentsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_LIST_FETCH = gql(`
  query SourcePickerJobsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    batchV1JobsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerJobsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_JOBS_LIST_WATCH = gql(`
  subscription SourcePickerJobsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    batchV1JobsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_NAMESPACES_LIST_FETCH = gql(`
  query SourcePickerNamespacesListFetch($kubeContext: String!, $continue: String = "") {
    coreV1NamespacesList(kubeContext: $kubeContext, options: { limit: "50", continue: $continue }) {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_NAMESPACES_LIST_WATCH = gql(`
  subscription SourcePickerNamespacesListWatch($kubeContext: String!, $resourceVersion: String = "") {
    coreV1NamespacesWatch(kubeContext: $kubeContext, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_LIST_FETCH = gql(`
  query SourcePickerPodsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    coreV1PodsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerPodsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_PODS_LIST_WATCH = gql(`
  subscription SourcePickerPodsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    coreV1PodsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_LIST_FETCH = gql(`
  query SourcePickerReplicaSetsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    appsV1ReplicaSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerReplicaSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_REPLICASETS_LIST_WATCH = gql(`
  subscription SourcePickerReplicaSetsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1ReplicaSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_LIST_FETCH = gql(`
  query SourcePickerStatefulSetsListFetch($kubeContext: String!, $namespace: String = "", $continue: String = "") {
    appsV1StatefulSetsList(kubeContext: $kubeContext, namespace: $namespace, options: { limit: "50", continue: $continue }) @connection(key: "SourcePickerStatefulSetsListFetch") {
      ...SourcePickerGenericListFragment
      items {
        ...SourcePickerGenericListItemFragment
      }
    }
  }
`);

export const SOURCE_PICKER_STATEFULSETS_LIST_WATCH = gql(`
  subscription SourcePickerStatefulSetsListWatch($kubeContext: String!, $namespace: String = "", $resourceVersion: String = "") {
    appsV1StatefulSetsWatch(kubeContext: $kubeContext, namespace: $namespace, options: { resourceVersion: $resourceVersion }) {
      type
      object {
        ...HomeStatefulSetsListItemFragment
      }
    }
  }
`);
