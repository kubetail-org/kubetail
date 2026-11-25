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

import { retryExchange } from '@urql/exchange-retry';
import { Client, cacheExchange, fetchExchange } from 'urql';

import { errorReportExchange } from './error-exchange';
import { invokeFetch } from './invoke-fetch';
import { tauriSubscriptionExchange } from './subscribe-exchange';

// Retries only `networkError` failures (package default `retryIf`) — GraphQL
// errors are deterministic and must NOT be retried. Bounded so a hard-down
// sidecar fails fast; subscription reconnect owns long-term recovery.
// `randomDelay` de-correlates the several queries a route fires at once.
const networkRetryExchange = retryExchange({
  initialDelayMs: 500,
  maxDelayMs: 5_000,
  maxNumberAttempts: 3,
  randomDelay: true,
});

export function createGraphqlClient() {
  return new Client({
    // Unused (invokeFetch ignores it) but urql requires one.
    url: 'tauri://graphql',
    // Order matters: subscriptions before fetchExchange (which discards them);
    // errorReport after cache (no cache-hit reports) and before retry (final
    // post-retry error only). Retry never fights the subscription auto-reconnect
    // — subscribe-exchange never surfaces an error result to it.
    exchanges: [cacheExchange, errorReportExchange, networkRetryExchange, tauriSubscriptionExchange, fetchExchange],
    fetch: invokeFetch,
    // The Tauri bridge only handles POST bodies; urql defaults short queries to GET.
    preferGetMethod: false,
  });
}
