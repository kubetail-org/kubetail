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

export type Cursor = string | 'BEGINNING';

export type LogRecord = {
  timestamp: string;
  message: string;
  cursor: Cursor;
  source: {
    metadata: {
      region: string;
      zone: string;
      os: string;
      arch: string;
      node: string;
    };
    namespace: string;
    podName: string;
    containerName: string;
  };
};

export type FetchOptions = {
  cursor?: Cursor | null;
  limit?: number;
};

export type FetchResult = {
  records: LogRecord[];
  nextCursor: Cursor | null;
};

export type SubscriptionCallback = (record: LogRecord) => void;

export type SubscriptionOptions = {
  after?: Cursor | null;
};

export type SubscriptionCancelFunction = () => void;

export type Client = {
  fetchSince: (options: FetchOptions) => Promise<FetchResult>;
  fetchUntil: (options: FetchOptions) => Promise<FetchResult>;
  fetchAfter: (options: FetchOptions) => Promise<FetchResult>;
  fetchBefore: (options: FetchOptions) => Promise<FetchResult>;
  subscribe: (callback: SubscriptionCallback, options?: SubscriptionOptions) => SubscriptionCancelFunction;
};
