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

import { invoke } from '@tauri-apps/api/core';

import { toError } from '../error-bus';

const JSON_HEADERS = { 'content-type': 'application/json' } as const;

// Return shape of the Rust `graphql_query` command. Status rides separately so
// the synthesized Response keeps it: collapsing 4xx/5xx to 200 would have
// retryExchange hammer a permanent 4xx forever.
type GraphqlResponse = { status: number; body: string };

// urql fetch adapter over the host's `graphql_query` invoke (the webview can't
// dial the sidecar socket). See docs/adr/2026-08-09-graphql-over-tauri-ipc.md
export const invokeFetch: typeof fetch = async (_input, init) => {
  // fetchExchange always POSTs a string body; anything else is forwarded empty
  // for the sidecar to reject.
  const body = typeof init?.body === 'string' ? init.body : '';
  try {
    const { status, body: text } = await invoke<GraphqlResponse>('graphql_query', { body });
    return new Response(text, { status, headers: JSON_HEADERS });
  } catch (err) {
    // Throw so urql yields a real `networkError` (retryable, distinguishable
    // from a server error); a fabricated 500 envelope would defeat both.
    throw toError(err);
  }
};
