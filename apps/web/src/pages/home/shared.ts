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

import {
  HomeCronJobsListItemFragmentFragment,
  HomeDaemonSetsListItemFragmentFragment,
  HomePodsListItemFragmentFragment,
  HomeJobsListItemFragmentFragment,
  HomeDeploymentsListItemFragmentFragment,
  HomeReplicaSetsListItemFragmentFragment,
  HomeStatefulSetsListItemFragmentFragment,
} from '@/lib/graphql/dashboard/__generated__/graphql';

export type KubeContext = string | null;

export type WorkloadItem =
  | HomeCronJobsListItemFragmentFragment
  | HomeDaemonSetsListItemFragmentFragment
  | HomePodsListItemFragmentFragment
  | HomeJobsListItemFragmentFragment
  | HomeDeploymentsListItemFragmentFragment
  | HomeReplicaSetsListItemFragmentFragment
  | HomeStatefulSetsListItemFragmentFragment;

export type FileInfo = {
  size: string;
  lastModifiedAt?: Date;
};
