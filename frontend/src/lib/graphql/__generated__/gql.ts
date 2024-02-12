/* eslint-disable */
import * as types from './graphql';
import { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';

/**
 * Map of all GraphQL operations in the project.
 *
 * This map has several performance disadvantages:
 * 1. It is not tree-shakeable, so it will include all operations in the project.
 * 2. It is not minifiable, so the string of a GraphQL query will be multiple times inside the bundle.
 * 3. It does not support dead code elimination, so it will add unused operations.
 *
 * Therefore it is highly recommended to use the babel or swc plugin for production.
 */
const documents = {
    "\n  fragment HomeGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n": types.HomeGenericListFragmentFragmentDoc,
    "\n  fragment HomeGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n": types.HomeGenericListItemFragmentFragmentDoc,
    "\n  fragment HomeCronJobsListItemFragment on BatchV1CronJob {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeCronJobsListItemFragmentFragmentDoc,
    "\n  fragment HomeDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeDaemonSetsListItemFragmentFragmentDoc,
    "\n  fragment HomeDeploymentsListItemFragment on AppsV1Deployment {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeDeploymentsListItemFragmentFragmentDoc,
    "\n  fragment HomeJobsListItemFragment on BatchV1Job {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeJobsListItemFragmentFragmentDoc,
    "\n  fragment HomeNamespacesListItemFragment on CoreV1Namespace {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeNamespacesListItemFragmentFragmentDoc,
    "\n  fragment HomePodsListItemFragment on CoreV1Pod {\n    ...HomeGenericListItemFragment\n  }\n": types.HomePodsListItemFragmentFragmentDoc,
    "\n  fragment HomeReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeReplicaSetsListItemFragmentFragmentDoc,
    "\n  fragment HomeStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...HomeGenericListItemFragment\n  }\n": types.HomeStatefulSetsListItemFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesGenericObjectFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n": types.ConsoleLoggingResourcesGenericObjectFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesCronJobFragment on BatchV1CronJob {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      jobTemplate {\n        spec {\n          selector {\n            matchLabels\n          }\n        }\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesCronJobFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesDaemonSetFragment on AppsV1DaemonSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesDaemonSetFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesDeploymentFragment on AppsV1Deployment {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesDeploymentFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesJobFragment on BatchV1Job {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesJobFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesPodFragment on CoreV1Pod {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      containers {\n        name\n      }\n      nodeName\n    }\n    status {\n      containerStatuses {\n        name\n        started\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesPodFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesReplicaSetFragment on AppsV1ReplicaSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesReplicaSetFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesStatefulSetFragment on AppsV1StatefulSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesStatefulSetFragmentFragmentDoc,
    "\n  fragment ConsoleLoggingResourcesJobsFindFragment on BatchV1Job {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesJobsFindFragmentFragmentDoc,
    "\n  fragment ConsoleNodesListItemFragment on CoreV1Node {\n    id\n    metadata {\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      labels\n      annotations\n    }\n  }\n": types.ConsoleNodesListItemFragmentFragmentDoc,
    "\n  fragment SourcePickerGenericCounterFragment on List {\n    metadata {\n      remainingItemCount\n      resourceVersion\n    }\n    items {\n      ...SourcePickerGenericCounterItemFragment\n    }\n  }\n": types.SourcePickerGenericCounterFragmentFragmentDoc,
    "\n  fragment SourcePickerGenericCounterItemFragment on Object {\n    id\n    metadata {\n      resourceVersion\n    }\n  }\n": types.SourcePickerGenericCounterItemFragmentFragmentDoc,
    "\n  fragment SourcePickerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n": types.SourcePickerGenericListFragmentFragmentDoc,
    "\n  fragment SourcePickerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n": types.SourcePickerGenericListItemFragmentFragmentDoc,
    "\n  fragment ExplorerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n": types.ExplorerGenericListFragmentFragmentDoc,
    "\n  fragment ExplorerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n": types.ExplorerGenericListItemFragmentFragmentDoc,
    "\n  fragment ExplorerCronJobsListItemFragment on BatchV1CronJob {\n    ...ExplorerGenericListItemFragment\n    spec {\n      schedule\n      suspend\n    }\n    status {\n      active {\n        __typename\n      }\n      lastScheduleTime\n      lastSuccessfulTime\n    }\n  }\n": types.ExplorerCronJobsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...ExplorerGenericListItemFragment\n    status {\n      currentNumberScheduled\n      desiredNumberScheduled\n    }\n  }\n": types.ExplorerDaemonSetsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerDeploymentsListItemFragment on AppsV1Deployment {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n": types.ExplorerDeploymentsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerJobsListItemFragment on BatchV1Job {\n    ...ExplorerGenericListItemFragment\n  }\n": types.ExplorerJobsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerPodsListItemFragment on CoreV1Pod {\n    ...ExplorerGenericListItemFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n    }\n    status {\n      phase\n      containerStatuses {\n        name\n        state {\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n          }\n        }\n        ready\n        restartCount\n        started\n      }\n    }\n  }\n": types.ExplorerPodsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n    }\n    status {\n      replicas\n    }\n  }\n": types.ExplorerReplicaSetsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...ExplorerGenericListItemFragment\n\n  }\n": types.ExplorerStatefulSetsListItemFragmentFragmentDoc,
    "\n  fragment ExplorerGenericObjectFragment on Object {\n    id\n    metadata {\n      creationTimestamp\n      deletionTimestamp\n      name\n      namespace\n      labels\n      annotations\n      ownerReferences {\n        apiVersion\n        kind\n        name\n        uid\n        controller\n      }\n      resourceVersion\n      uid\n    }\n  }\n": types.ExplorerGenericObjectFragmentFragmentDoc,
    "\n  fragment ExplorerCronJobsObjectFragment on BatchV1CronJob {\n    ...ExplorerGenericObjectFragment\n  }\n": types.ExplorerCronJobsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerDaemonSetsObjectFragment on AppsV1DaemonSet {\n    ...ExplorerGenericObjectFragment\n  }\n": types.ExplorerDaemonSetsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerDeploymentsObjectFragment on AppsV1Deployment {\n    ...ExplorerGenericObjectFragment\n    spec {\n      replicas\n      selector {\n        matchLabels\n        matchExpressions {\n          key\n          operator\n          values\n        }\n      }\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n": types.ExplorerDeploymentsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerJobsObjectFragment on BatchV1Job {\n    ...ExplorerGenericObjectFragment\n  }\n": types.ExplorerJobsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerPodsObjectFragment on CoreV1Pod {\n    ...ExplorerGenericObjectFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n      priorityClassName\n    }\n    status {\n      phase\n      message\n      reason\n      containerStatuses {\n        name\n        state {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        lastTerminationState {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        ready\n        restartCount\n        imageID\n        started\n      }\n    }\n  }\n": types.ExplorerPodsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerReplicaSetsObjectFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericObjectFragment\n  }\n": types.ExplorerReplicaSetsObjectFragmentFragmentDoc,
    "\n  fragment ExplorerStatefulSetsObjectFragment on AppsV1StatefulSet {\n    ...ExplorerGenericObjectFragment\n  }\n": types.ExplorerStatefulSetsObjectFragmentFragmentDoc,
    "\n  query HomeCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n": types.HomeCronJobsListFetchDocument,
    "\n  subscription HomeCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n": types.HomeCronJobsListWatchDocument,
    "\n  query HomeDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n": types.HomeDaemonSetsListFetchDocument,
    "\n  subscription HomeDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n": types.HomeDaemonSetsListWatchDocument,
    "\n  query HomeDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n": types.HomeDeploymentsListFetchDocument,
    "\n  subscription HomeDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n": types.HomeDeploymentsListWatchDocument,
    "\n  query HomeJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n": types.HomeJobsListFetchDocument,
    "\n  subscription HomeJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n": types.HomeJobsListWatchDocument,
    "\n  query HomeNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n": types.HomeNamespacesListFetchDocument,
    "\n  subscription HomeNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n": types.HomeNamespacesListWatchDocument,
    "\n  query HomePodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n": types.HomePodsListFetchDocument,
    "\n  subscription HomePodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n": types.HomePodsListWatchDocument,
    "\n  query HomeReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n": types.HomeReplicaSetsListFetchDocument,
    "\n  subscription HomeReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n": types.HomeReplicaSetsListWatchDocument,
    "\n  query HomeStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n": types.HomeStatefulSetsListFetchDocument,
    "\n  subscription HomeStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n": types.HomeStatefulSetsListWatchDocument,
    "\n  query ConsoleLoggingResourcesCronJobGet($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesCronJobFragment\n    }\n  }\n": types.ConsoleLoggingResourcesCronJobGetDocument,
    "\n  subscription ConsoleLoggingResourcesCronJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesCronJobFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesCronJobWatchDocument,
    "\n  query ConsoleLoggingResourcesDaemonSetGet($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDaemonSetFragment\n    }    \n  }\n": types.ConsoleLoggingResourcesDaemonSetGetDocument,
    "\n  subscription ConsoleLoggingResourcesDaemonSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDaemonSetFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesDaemonSetWatchDocument,
    "\n  query ConsoleLoggingResourcesDeploymentGet($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDeploymentFragment\n    }    \n  }\n": types.ConsoleLoggingResourcesDeploymentGetDocument,
    "\n  subscription ConsoleLoggingResourcesDeploymentWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDeploymentFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesDeploymentWatchDocument,
    "\n  query ConsoleLoggingResourcesJobGet($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesJobFragment\n    }\n  }\n": types.ConsoleLoggingResourcesJobGetDocument,
    "\n  subscription ConsoleLoggingResourcesJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesJobWatchDocument,
    "\n  query ConsoleLoggingResourcesPodGet($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesPodFragment\n    }\n  }\n": types.ConsoleLoggingResourcesPodGetDocument,
    "\n  subscription ConsoleLoggingResourcesPodWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesPodWatchDocument,
    "\n  query ConsoleLoggingResourcesReplicaSetGet($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesReplicaSetFragment\n    }\n  }\n": types.ConsoleLoggingResourcesReplicaSetGetDocument,
    "\n  subscription ConsoleLoggingResourcesReplicaSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesReplicaSetFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesReplicaSetWatchDocument,
    "\n  query ConsoleLoggingResourcesStatefulSetGet($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesStatefulSetFragment\n    }    \n  }\n": types.ConsoleLoggingResourcesStatefulSetGetDocument,
    "\n  subscription ConsoleLoggingResourcesStatefulSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesStatefulSetFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesStatefulSetWatchDocument,
    "\n  query ConsoleLoggingResourcesJobsFind($namespace: String!, $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesJobsFindFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesJobsFindDocument,
    "\n  subscription ConsoleStreamsJobsWatch($namespace: String!, $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n": types.ConsoleStreamsJobsWatchDocument,
    "\n  query ConsoleLoggingResourcesPodsFind($namespace: String!, $labelSelector: String!, $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { labelSelector: $labelSelector, continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesPodsFindDocument,
    "\n  subscription ConsoleLoggingResourcesPodsWatch($namespace: String!, $labelSelector: String!, $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { labelSelector: $labelSelector, resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n": types.ConsoleLoggingResourcesPodsWatchDocument,
    "\n  query ConsoleNodesListFetch($continue: String = \"\") {\n    coreV1NodesList(options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n": types.ConsoleNodesListFetchDocument,
    "\n  subscription ConsoleNodesListWatch($resourceVersion: String = \"\") {\n    coreV1NodesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n": types.ConsoleNodesListWatchDocument,
    "\n  query SourcePickerCronJobsCountFetch($namespace: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerCronJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerCronJobsCountFetchDocument,
    "\n  subscription SourcePickerCronJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerCronJobsCountWatchDocument,
    "\n  query SourcePickerDaemonSetsCountFetch($namespace: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDaemonSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerDaemonSetsCountFetchDocument,
    "\n  subscription SourcePickerDaemonSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerDaemonSetsCountWatchDocument,
    "\n  query SourcePickerDeploymentsCountFetch($namespace: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDeploymentsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerDeploymentsCountFetchDocument,
    "\n  subscription SourcePickerDeploymentsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerDeploymentsCountWatchDocument,
    "\n  query SourcePickerJobsCountFetch($namespace: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerJobsCountFetchDocument,
    "\n  subscription SourcePickerJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerJobsCountWatchDocument,
    "\n  query SourcePickerPodsCountFetch($namespace: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerPodsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerPodsCountFetchDocument,
    "\n  subscription SourcePickerPodsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerPodsCountWatchDocument,
    "\n  query SourcePickerReplicaSetsCountFetch($namespace: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerReplicaSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerReplicaSetsCountFetchDocument,
    "\n  subscription SourcePickerReplicaSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerReplicaSetsCountWatchDocument,
    "\n  query SourcePickerStatefulSetsCountFetch($namespace: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerStatefulSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerStatefulSetsCountFetchDocument,
    "\n  subscription SourcePickerStatefulSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n": types.SourcePickerStatefulSetsCountWatchDocument,
    "\n  query SourcePickerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerCronJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerCronJobsListFetchDocument,
    "\n  subscription SourcePickerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerCronJobsListWatchDocument,
    "\n  query SourcePickerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDaemonSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerDaemonSetsListFetchDocument,
    "\n  subscription SourcePickerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerDaemonSetsListWatchDocument,
    "\n  query SourcePickerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDeploymentsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerDeploymentsListFetchDocument,
    "\n  subscription SourcePickerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerDeploymentsListWatchDocument,
    "\n  query SourcePickerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerJobsListFetchDocument,
    "\n  subscription SourcePickerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerJobsListWatchDocument,
    "\n  query SourcePickerNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerNamespacesListFetchDocument,
    "\n  subscription SourcePickerNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerNamespacesListWatchDocument,
    "\n  query SourcePickerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerPodsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerPodsListFetchDocument,
    "\n  subscription SourcePickerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerPodsListWatchDocument,
    "\n  query SourcePickerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerReplicaSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerReplicaSetsListFetchDocument,
    "\n  subscription SourcePickerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerReplicaSetsListWatchDocument,
    "\n  query SourcePickerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerStatefulSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n": types.SourcePickerStatefulSetsListFetchDocument,
    "\n  subscription SourcePickerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n": types.SourcePickerStatefulSetsListWatchDocument,
    "\n  query ExplorerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n": types.ExplorerCronJobsListFetchDocument,
    "\n  subscription ExplorerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n": types.ExplorerCronJobsListWatchDocument,
    "\n  query ExplorerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerDaemonSetsListFetchDocument,
    "\n  subscription ExplorerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerDaemonSetsListWatchDocument,
    "\n  query ExplorerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n": types.ExplorerDeploymentsListFetchDocument,
    "\n  subscription ExplorerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n": types.ExplorerDeploymentsListWatchDocument,
    "\n  query ExplorerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n": types.ExplorerJobsListFetchDocument,
    "\n  subscription ExplorerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n": types.ExplorerJobsListWatchDocument,
    "\n  query ExplorerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n": types.ExplorerPodsListFetchDocument,
    "\n  subscription ExplorerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n": types.ExplorerPodsListWatchDocument,
    "\n  query ExplorerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerReplicaSetsListFetchDocument,
    "\n  subscription ExplorerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerReplicaSetsListWatchDocument,
    "\n  query ExplorerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerStatefulSetsListFetchDocument,
    "\n  subscription ExplorerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n": types.ExplorerStatefulSetsListWatchDocument,
    "\n  query ExplorerCronJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerCronJobsObjectFragment\n    }\n  }\n": types.ExplorerCronJobsObjectFetchDocument,
    "\n  subscription ExplorerCronJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerCronJobsObjectFragment\n      }\n    }\n  }\n": types.ExplorerCronJobsObjectWatchDocument,
    "\n  query ExplorerDaemonSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDaemonSetsObjectFragment\n    }\n  }\n": types.ExplorerDaemonSetsObjectFetchDocument,
    "\n  subscription ExplorerDaemonSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDaemonSetsObjectFragment\n      }\n    }\n  }\n": types.ExplorerDaemonSetsObjectWatchDocument,
    "\n  query ExplorerDeploymentsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDeploymentsObjectFragment\n    }\n  }\n": types.ExplorerDeploymentsObjectFetchDocument,
    "\n  subscription ExplorerDeploymentsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDeploymentsObjectFragment\n      }\n    }\n  }\n": types.ExplorerDeploymentsObjectWatchDocument,
    "\n  query ExplorerJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerJobsObjectFragment\n    }\n  }\n": types.ExplorerJobsObjectFetchDocument,
    "\n  subscription ExplorerJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerJobsObjectFragment\n      }\n    }\n  }\n": types.ExplorerJobsObjectWatchDocument,
    "\n  query ExplorerPodsObjectFetch($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ExplorerPodsObjectFragment\n    }\n  }\n": types.ExplorerPodsObjectFetchDocument,
    "\n  subscription ExplorerPodsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerPodsObjectFragment\n      }\n    }\n  }\n": types.ExplorerPodsObjectWatchDocument,
    "\n  query ExplorerReplicaSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerReplicaSetsObjectFragment\n    }\n  }\n": types.ExplorerReplicaSetsObjectFetchDocument,
    "\n  subscription ExplorerReplicaSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerReplicaSetsObjectFragment\n      }\n    }\n  }\n": types.ExplorerReplicaSetsObjectWatchDocument,
    "\n  query ExplorerStatefulSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerStatefulSetsObjectFragment\n    }\n  }\n": types.ExplorerStatefulSetsObjectFetchDocument,
    "\n  subscription ExplorerStatefulSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerStatefulSetsObjectFragment\n      }\n    }\n  }\n": types.ExplorerStatefulSetsObjectWatchDocument,
    "\n  query QueryContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogQuery(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n": types.QueryContainerLogDocument,
    "\n  subscription TailContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogTail(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n": types.TailContainerLogDocument,
    "\n  subscription LivezWatch {\n    livezWatch {\n      status\n      message\n      timestamp\n    }\n  }\n": types.LivezWatchDocument,
    "\n  subscription ReadyzWatch {\n    readyzWatch {\n      status\n      message\n      timestamp\n    }\n  }\n": types.ReadyzWatchDocument,
};

/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 *
 *
 * @example
 * ```ts
 * const query = gql(`query GetUser($id: ID!) { user(id: $id) { name } }`);
 * ```
 *
 * The query argument is unknown!
 * Please regenerate the types.
 */
export function gql(source: string): unknown;

/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment HomeGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment HomeGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeCronJobsListItemFragment on BatchV1CronJob {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeCronJobsListItemFragment on BatchV1CronJob {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeDeploymentsListItemFragment on AppsV1Deployment {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeDeploymentsListItemFragment on AppsV1Deployment {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeJobsListItemFragment on BatchV1Job {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeJobsListItemFragment on BatchV1Job {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeNamespacesListItemFragment on CoreV1Namespace {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeNamespacesListItemFragment on CoreV1Namespace {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomePodsListItemFragment on CoreV1Pod {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomePodsListItemFragment on CoreV1Pod {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment HomeStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...HomeGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment HomeStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...HomeGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesGenericObjectFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesGenericObjectFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesCronJobFragment on BatchV1CronJob {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      jobTemplate {\n        spec {\n          selector {\n            matchLabels\n          }\n        }\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesCronJobFragment on BatchV1CronJob {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      jobTemplate {\n        spec {\n          selector {\n            matchLabels\n          }\n        }\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesDaemonSetFragment on AppsV1DaemonSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesDaemonSetFragment on AppsV1DaemonSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesDeploymentFragment on AppsV1Deployment {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesDeploymentFragment on AppsV1Deployment {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesJobFragment on BatchV1Job {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesJobFragment on BatchV1Job {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesPodFragment on CoreV1Pod {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      containers {\n        name\n      }\n      nodeName\n    }\n    status {\n      containerStatuses {\n        name\n        started\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesPodFragment on CoreV1Pod {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      containers {\n        name\n      }\n      nodeName\n    }\n    status {\n      containerStatuses {\n        name\n        started\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesReplicaSetFragment on AppsV1ReplicaSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesReplicaSetFragment on AppsV1ReplicaSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesStatefulSetFragment on AppsV1StatefulSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesStatefulSetFragment on AppsV1StatefulSet {\n    ...ConsoleLoggingResourcesGenericObjectFragment\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleLoggingResourcesJobsFindFragment on BatchV1Job {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleLoggingResourcesJobsFindFragment on BatchV1Job {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n    spec {\n      selector {\n        matchLabels\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ConsoleNodesListItemFragment on CoreV1Node {\n    id\n    metadata {\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      labels\n      annotations\n    }\n  }\n"): (typeof documents)["\n  fragment ConsoleNodesListItemFragment on CoreV1Node {\n    id\n    metadata {\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      labels\n      annotations\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment SourcePickerGenericCounterFragment on List {\n    metadata {\n      remainingItemCount\n      resourceVersion\n    }\n    items {\n      ...SourcePickerGenericCounterItemFragment\n    }\n  }\n"): (typeof documents)["\n  fragment SourcePickerGenericCounterFragment on List {\n    metadata {\n      remainingItemCount\n      resourceVersion\n    }\n    items {\n      ...SourcePickerGenericCounterItemFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment SourcePickerGenericCounterItemFragment on Object {\n    id\n    metadata {\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment SourcePickerGenericCounterItemFragment on Object {\n    id\n    metadata {\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment SourcePickerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment SourcePickerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment SourcePickerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment SourcePickerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerGenericListFragment on List {\n    metadata {\n      continue\n      resourceVersion\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerGenericListItemFragment on Object {\n    id\n    metadata {\n      namespace\n      name\n      uid\n      creationTimestamp\n      deletionTimestamp\n      resourceVersion\n      ownerReferences {\n        name\n        uid\n        controller\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerCronJobsListItemFragment on BatchV1CronJob {\n    ...ExplorerGenericListItemFragment\n    spec {\n      schedule\n      suspend\n    }\n    status {\n      active {\n        __typename\n      }\n      lastScheduleTime\n      lastSuccessfulTime\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerCronJobsListItemFragment on BatchV1CronJob {\n    ...ExplorerGenericListItemFragment\n    spec {\n      schedule\n      suspend\n    }\n    status {\n      active {\n        __typename\n      }\n      lastScheduleTime\n      lastSuccessfulTime\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...ExplorerGenericListItemFragment\n    status {\n      currentNumberScheduled\n      desiredNumberScheduled\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerDaemonSetsListItemFragment on AppsV1DaemonSet {\n    ...ExplorerGenericListItemFragment\n    status {\n      currentNumberScheduled\n      desiredNumberScheduled\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerDeploymentsListItemFragment on AppsV1Deployment {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerDeploymentsListItemFragment on AppsV1Deployment {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerJobsListItemFragment on BatchV1Job {\n    ...ExplorerGenericListItemFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerJobsListItemFragment on BatchV1Job {\n    ...ExplorerGenericListItemFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerPodsListItemFragment on CoreV1Pod {\n    ...ExplorerGenericListItemFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n    }\n    status {\n      phase\n      containerStatuses {\n        name\n        state {\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n          }\n        }\n        ready\n        restartCount\n        started\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerPodsListItemFragment on CoreV1Pod {\n    ...ExplorerGenericListItemFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n    }\n    status {\n      phase\n      containerStatuses {\n        name\n        state {\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n          }\n        }\n        ready\n        restartCount\n        started\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n    }\n    status {\n      replicas\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerReplicaSetsListItemFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericListItemFragment\n    spec {\n      replicas\n    }\n    status {\n      replicas\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...ExplorerGenericListItemFragment\n\n  }\n"): (typeof documents)["\n  fragment ExplorerStatefulSetsListItemFragment on AppsV1StatefulSet {\n    ...ExplorerGenericListItemFragment\n\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerGenericObjectFragment on Object {\n    id\n    metadata {\n      creationTimestamp\n      deletionTimestamp\n      name\n      namespace\n      labels\n      annotations\n      ownerReferences {\n        apiVersion\n        kind\n        name\n        uid\n        controller\n      }\n      resourceVersion\n      uid\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerGenericObjectFragment on Object {\n    id\n    metadata {\n      creationTimestamp\n      deletionTimestamp\n      name\n      namespace\n      labels\n      annotations\n      ownerReferences {\n        apiVersion\n        kind\n        name\n        uid\n        controller\n      }\n      resourceVersion\n      uid\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerCronJobsObjectFragment on BatchV1CronJob {\n    ...ExplorerGenericObjectFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerCronJobsObjectFragment on BatchV1CronJob {\n    ...ExplorerGenericObjectFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerDaemonSetsObjectFragment on AppsV1DaemonSet {\n    ...ExplorerGenericObjectFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerDaemonSetsObjectFragment on AppsV1DaemonSet {\n    ...ExplorerGenericObjectFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerDeploymentsObjectFragment on AppsV1Deployment {\n    ...ExplorerGenericObjectFragment\n    spec {\n      replicas\n      selector {\n        matchLabels\n        matchExpressions {\n          key\n          operator\n          values\n        }\n      }\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerDeploymentsObjectFragment on AppsV1Deployment {\n    ...ExplorerGenericObjectFragment\n    spec {\n      replicas\n      selector {\n        matchLabels\n        matchExpressions {\n          key\n          operator\n          values\n        }\n      }\n      paused\n    }\n    status {\n      replicas\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerJobsObjectFragment on BatchV1Job {\n    ...ExplorerGenericObjectFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerJobsObjectFragment on BatchV1Job {\n    ...ExplorerGenericObjectFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerPodsObjectFragment on CoreV1Pod {\n    ...ExplorerGenericObjectFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n      priorityClassName\n    }\n    status {\n      phase\n      message\n      reason\n      containerStatuses {\n        name\n        state {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        lastTerminationState {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        ready\n        restartCount\n        imageID\n        started\n      }\n    }\n  }\n"): (typeof documents)["\n  fragment ExplorerPodsObjectFragment on CoreV1Pod {\n    ...ExplorerGenericObjectFragment\n    spec {\n      containers {\n        name\n        image\n      }\n      nodeName\n      priorityClassName\n    }\n    status {\n      phase\n      message\n      reason\n      containerStatuses {\n        name\n        state {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        lastTerminationState {\n          waiting {\n            reason\n            message\n          }\n          running {\n            startedAt\n          }\n          terminated {\n            exitCode\n            signal\n            reason\n            message\n          }\n        }\n        ready\n        restartCount\n        imageID\n        started\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerReplicaSetsObjectFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericObjectFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerReplicaSetsObjectFragment on AppsV1ReplicaSet {\n    ...ExplorerGenericObjectFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  fragment ExplorerStatefulSetsObjectFragment on AppsV1StatefulSet {\n    ...ExplorerGenericObjectFragment\n  }\n"): (typeof documents)["\n  fragment ExplorerStatefulSetsObjectFragment on AppsV1StatefulSet {\n    ...ExplorerGenericObjectFragment\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeCronJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDaemonSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeDeploymentsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeNamespacesListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomePodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomePodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomePodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomePodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomePodsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeReplicaSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query HomeStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query HomeStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...HomeGenericListFragment\n      items {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription HomeStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription HomeStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesCronJobGet($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesCronJobFragment\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesCronJobGet($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesCronJobFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesCronJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesCronJobFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesCronJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesCronJobFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesDaemonSetGet($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDaemonSetFragment\n    }    \n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesDaemonSetGet($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDaemonSetFragment\n    }    \n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesDaemonSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDaemonSetFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesDaemonSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDaemonSetFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesDeploymentGet($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDeploymentFragment\n    }    \n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesDeploymentGet($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesDeploymentFragment\n    }    \n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesDeploymentWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDeploymentFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesDeploymentWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesDeploymentFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesJobGet($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesJobFragment\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesJobGet($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesJobFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesJobWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesPodGet($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesPodFragment\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesPodGet($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesPodFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesPodWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesPodWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesReplicaSetGet($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesReplicaSetFragment\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesReplicaSetGet($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesReplicaSetFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesReplicaSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesReplicaSetFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesReplicaSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesReplicaSetFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesStatefulSetGet($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesStatefulSetFragment\n    }    \n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesStatefulSetGet($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ConsoleLoggingResourcesStatefulSetFragment\n    }    \n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesStatefulSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesStatefulSetFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesStatefulSetWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesStatefulSetFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesJobsFind($namespace: String!, $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesJobsFindFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesJobsFind($namespace: String!, $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesJobsFindFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleStreamsJobsWatch($namespace: String!, $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleStreamsJobsWatch($namespace: String!, $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesJobFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleLoggingResourcesPodsFind($namespace: String!, $labelSelector: String!, $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { labelSelector: $labelSelector, continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ConsoleLoggingResourcesPodsFind($namespace: String!, $labelSelector: String!, $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { labelSelector: $labelSelector, continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleLoggingResourcesPodsWatch($namespace: String!, $labelSelector: String!, $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { labelSelector: $labelSelector, resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleLoggingResourcesPodsWatch($namespace: String!, $labelSelector: String!, $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { labelSelector: $labelSelector, resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleLoggingResourcesPodFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ConsoleNodesListFetch($continue: String = \"\") {\n    coreV1NodesList(options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ConsoleNodesListFetch($continue: String = \"\") {\n    coreV1NodesList(options: { limit: \"50\", continue: $continue }) {\n      metadata {\n        continue\n        resourceVersion\n      }\n      items {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ConsoleNodesListWatch($resourceVersion: String = \"\") {\n    coreV1NodesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ConsoleNodesListWatch($resourceVersion: String = \"\") {\n    coreV1NodesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ConsoleNodesListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerCronJobsCountFetch($namespace: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerCronJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerCronJobsCountFetch($namespace: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerCronJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerCronJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerCronJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerDaemonSetsCountFetch($namespace: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDaemonSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerDaemonSetsCountFetch($namespace: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDaemonSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerDaemonSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerDaemonSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerDeploymentsCountFetch($namespace: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDeploymentsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerDeploymentsCountFetch($namespace: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerDeploymentsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerDeploymentsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerDeploymentsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerJobsCountFetch($namespace: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerJobsCountFetch($namespace: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerJobsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerJobsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerPodsCountFetch($namespace: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerPodsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerPodsCountFetch($namespace: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerPodsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerPodsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerPodsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerReplicaSetsCountFetch($namespace: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerReplicaSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerReplicaSetsCountFetch($namespace: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerReplicaSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerReplicaSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerReplicaSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerStatefulSetsCountFetch($namespace: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerStatefulSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerStatefulSetsCountFetch($namespace: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"1\" }) @connection(key: \"SourcePickerStatefulSetsCountFetch\") {\n      ...SourcePickerGenericCounterFragment\n      items {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerStatefulSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerStatefulSetsCountWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericCounterItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerCronJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerCronJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDaemonSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDaemonSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDeploymentsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerDeploymentsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerJobsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerNamespacesListFetch($continue: String = \"\") {\n    coreV1NamespacesList(options: { limit: \"50\", continue: $continue }) {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerNamespacesListWatch($resourceVersion: String = \"\") {\n    coreV1NamespacesWatch(options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerPodsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerPodsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerReplicaSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerReplicaSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query SourcePickerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerStatefulSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query SourcePickerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) @connection(key: \"SourcePickerStatefulSetsListFetch\") {\n      ...SourcePickerGenericListFragment\n      items {\n        ...SourcePickerGenericListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription SourcePickerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription SourcePickerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...HomeStatefulSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerCronJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1CronJobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerCronJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1CronJobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerCronJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerDaemonSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DaemonSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerDaemonSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDaemonSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerDeploymentsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1DeploymentsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerDeploymentsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerDeploymentsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerJobsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    batchV1JobsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerJobsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    batchV1JobsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerJobsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerPodsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    coreV1PodsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerPodsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    coreV1PodsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerPodsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerReplicaSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1ReplicaSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerReplicaSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerReplicaSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  query ExplorerStatefulSetsListFetch($namespace: String = \"\", $continue: String = \"\") {\n    appsV1StatefulSetsList(namespace: $namespace, options: { limit: \"50\", continue: $continue }) {\n      ...ExplorerGenericListFragment\n      items {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerStatefulSetsListWatch($namespace: String = \"\", $resourceVersion: String = \"\") {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { resourceVersion: $resourceVersion }) {\n      type\n      object {\n        ...ExplorerStatefulSetsListItemFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerCronJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerCronJobsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerCronJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1CronJobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerCronJobsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerCronJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerCronJobsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerCronJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1CronJobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerCronJobsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerDaemonSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDaemonSetsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerDaemonSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DaemonSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDaemonSetsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerDaemonSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDaemonSetsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerDaemonSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DaemonSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDaemonSetsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerDeploymentsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDeploymentsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerDeploymentsObjectFetch($namespace: String!, $name: String!) {\n    appsV1DeploymentsGet(namespace: $namespace, name: $name) {\n      ...ExplorerDeploymentsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerDeploymentsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDeploymentsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerDeploymentsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1DeploymentsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerDeploymentsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerJobsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerJobsObjectFetch($namespace: String!, $name: String!) {\n    batchV1JobsGet(namespace: $namespace, name: $name) {\n      ...ExplorerJobsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerJobsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerJobsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    batchV1JobsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerJobsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerPodsObjectFetch($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ExplorerPodsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerPodsObjectFetch($namespace: String!, $name: String!) {\n    coreV1PodsGet(namespace: $namespace, name: $name) {\n      ...ExplorerPodsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerPodsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerPodsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerPodsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    coreV1PodsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerPodsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerReplicaSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerReplicaSetsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerReplicaSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1ReplicaSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerReplicaSetsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerReplicaSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerReplicaSetsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerReplicaSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1ReplicaSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerReplicaSetsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query ExplorerStatefulSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerStatefulSetsObjectFragment\n    }\n  }\n"): (typeof documents)["\n  query ExplorerStatefulSetsObjectFetch($namespace: String!, $name: String!) {\n    appsV1StatefulSetsGet(namespace: $namespace, name: $name) {\n      ...ExplorerStatefulSetsObjectFragment\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ExplorerStatefulSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerStatefulSetsObjectFragment\n      }\n    }\n  }\n"): (typeof documents)["\n  subscription ExplorerStatefulSetsObjectWatch($namespace: String!, $fieldSelector: String!) {\n    appsV1StatefulSetsWatch(namespace: $namespace, options: { fieldSelector: $fieldSelector }) {\n      type\n      object {\n        ...ExplorerStatefulSetsObjectFragment\n      }\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  query QueryContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogQuery(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n"): (typeof documents)["\n  query QueryContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogQuery(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription TailContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogTail(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n"): (typeof documents)["\n  subscription TailContainerLog($namespace: String!, $name: String!, $container: String, $after: String, $since: String, $until: String, $limit: Int) {\n    podLogTail(namespace: $namespace, name: $name, container: $container, after: $after, since: $since, until: $until, limit: $limit) {\n      timestamp\n      message\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription LivezWatch {\n    livezWatch {\n      status\n      message\n      timestamp\n    }\n  }\n"): (typeof documents)["\n  subscription LivezWatch {\n    livezWatch {\n      status\n      message\n      timestamp\n    }\n  }\n"];
/**
 * The gql function is used to parse GraphQL queries into a document that can be used by GraphQL clients.
 */
export function gql(source: "\n  subscription ReadyzWatch {\n    readyzWatch {\n      status\n      message\n      timestamp\n    }\n  }\n"): (typeof documents)["\n  subscription ReadyzWatch {\n    readyzWatch {\n      status\n      message\n      timestamp\n    }\n  }\n"];

export function gql(source: string) {
  return (documents as any)[source] ?? {};
}

export type DocumentType<TDocumentNode extends DocumentNode<any, any>> = TDocumentNode extends DocumentNode<  infer TType,  any>  ? TType  : never;