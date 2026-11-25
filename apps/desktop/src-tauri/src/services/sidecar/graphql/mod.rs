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

//! Host-side GraphQL clients to the sidecar. Both dial the sidecar over its
//! UDS / named pipe and speak HTTP/1:
//!   * `query` — stateless, one connection per query/mutation call.
//!   * `subscribe` — one SSE (`text/event-stream`) connection per
//!     subscription, streaming gqlgen's `next`/`complete` frames.

mod query;
mod subscribe;

pub use query::{GraphqlResponse, QueryClient};
pub use subscribe::{FrameSink, SubscriptionClient};
