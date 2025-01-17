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

import { gql } from '@/lib/graphql/cluster-api/__generated__/gql';

/**
 * Log metadata queries
 */

export const LOG_METADATA_LIST_FETCH = gql(`
  query LogMetadataListFetch($namespace: String = "") {
    logMetadataList(namespace: $namespace) {
      items {
        ...LogMetadataListItemFragment
      }
    }
  }
`);

export const LOG_METADATA_LIST_WATCH = gql(`
  subscription LogMetadataListWatch($namespace: String = "") {
    logMetadataWatch(namespace: $namespace) {
      type
      object {
        ...LogMetadataListItemFragment
      }
    }
  }
`);
