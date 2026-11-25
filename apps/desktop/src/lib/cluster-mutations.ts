// Copyright 2024 The Kubetail Authors
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

// The writes to one cluster that more than one view makes: the Clusters dialog
// and Settings' Clusters tab. Each answers through `clusterCollectionWatch`, which is
// what a view draws; neither reads the mutation's own answer.
import { graphql } from '@/gql';

export const ClusterEnabledSetMutation = graphql(`
  mutation ClusterEnabledSet($id: ClusterID!, $enabled: Boolean!) {
    clusterEnabledSet(id: $id, enabled: $enabled) {
      id
      spec {
        enabled
      }
    }
  }
`);

export const ClusterSyncEnabledSetMutation = graphql(`
  mutation ClusterSyncEnabledSet($id: ClusterID!, $syncEnabled: Boolean!) {
    clusterSyncEnabledSet(id: $id, syncEnabled: $syncEnabled) {
      id
      spec {
        syncEnabled
      }
    }
  }
`);

export const ClusterDefaultSetMutation = graphql(`
  mutation ClusterDefaultSet($id: ClusterID!, $isDefault: Boolean!) {
    clusterDefaultSet(id: $id, isDefault: $isDefault) {
      id
      spec {
        isDefault
      }
    }
  }
`);

export const ClusterCacheClearMutation = graphql(`
  mutation ClusterCacheClear($id: ObjectID!) {
    clusterCacheClear(id: $id) {
      id
    }
  }
`);

export const ClusterDeleteMutation = graphql(`
  mutation ClusterDelete($id: ClusterID!) {
    clusterDelete(id: $id)
  }
`);
