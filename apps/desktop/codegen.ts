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

// graphql-codegen config. Reads the sidecar's authoritative schema from
// the Go module and emits typed document helpers under src/gql/ via the
// client preset (consumed by urql).
import type { CodegenConfig } from '@graphql-codegen/cli';

const config: CodegenConfig = {
  schema: 'sidecar/graph/schema.graphqls',
  // Tests are excluded: they don't define typed documents, and transport-level
  // tests use raw gql tags with schema-less placeholder operations.
  documents: ['src/**/*.{ts,tsx}', '!src/gql/**', '!src/**/*.test.{ts,tsx}'],
  ignoreNoDocuments: true,
  generates: {
    'src/gql/': {
      preset: 'client',
      config: {
        useTypeImports: true,
        // Custom scalars and their TS wire types. An unmapped one lands as `any`.
        // Time is an ISO-8601 UTC string; ObjectID is an opaque decimal-string
        // object id (an int64 server-side, a string on the wire) and ClusterID an
        // opaque row id — the webview treats both as opaque strings, and
        // ChatID/MessageID/ToolCallID/ApprovalID/MemoryID are opaque UUID strings
        // the same way. JSON is an arbitrary decoded value (a cached object's native body,
        // or a message's content blocks) — typed `unknown`, cast at the point of
        // use (ObjectsTable's per-kind column registry).
        scalars: {
          Time: 'string',
          ObjectID: 'string',
          ClusterID: 'string',
          ChatID: 'string',
          MessageID: 'string',
          ToolCallID: 'string',
          ApprovalID: 'string',
          MemoryID: 'string',
          JSON: 'unknown',
        },
      },
    },
  },
};

export default config;
