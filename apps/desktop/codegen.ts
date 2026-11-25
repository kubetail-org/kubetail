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
