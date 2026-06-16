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

import { CodegenConfig } from '@graphql-codegen/cli';

// client-preset v6 changed several defaults: custom scalars became `unknown`
// (was `any`), GraphQL enums became union types (was native TS enums), and
// `__typename` was no longer added to operation/fragment types. Restore the prior
// behavior so the generated types match the shapes the app and Apollo cache expect:
//   - defaultScalarType: 'any'      — custom scalars back to `any`
//   - enumType: 'native'            — native TS enums instead of string unions
//   - nonOptionalTypename: true     — add (required) `__typename` to object types
//   - skipTypeNameForRoot: true     — but not on the operation root, which Apollo
//                                     never requests (its runtime addTypename adds
//                                     `__typename` to nested fields only)
const config: CodegenConfig = {
  generates: {
    './src/lib/graphql/dashboard/__generated__/': {
      schema: '../modules/dashboard/graph/schema.graphqls',
      documents: './src/lib/graphql/dashboard/*.ts',
      preset: 'client',
      plugins: [],
      presetConfig: {
        gqlTagName: 'gql',
        fragmentMasking: false,
      },
      config: {
        defaultScalarType: 'any',
        enumType: 'native',
        nonOptionalTypename: true,
        skipTypeNameForRoot: true,
      },
    },
    './src/lib/graphql/dashboard/__generated__/introspection-result.json': {
      schema: '../modules/dashboard/graph/schema.graphqls',
      documents: './src/lib/graphql/dashboard/*.ts',
      plugins: ['fragment-matcher'],
      config: {
        module: 'commonjs',
      },
    },
    './src/lib/graphql/cluster-api/__generated__/': {
      schema: '../modules/cluster-api/graph/schema.graphqls',
      documents: './src/lib/graphql/cluster-api/*.ts',
      preset: 'client',
      plugins: [],
      presetConfig: {
        gqlTagName: 'gql',
        fragmentMasking: false,
      },
      config: {
        defaultScalarType: 'any',
        enumType: 'native',
        nonOptionalTypename: true,
        skipTypeNameForRoot: true,
      },
    },
    './src/lib/graphql/cluster-api/__generated__/introspection-result.json': {
      schema: '../modules/cluster-api/graph/schema.graphqls',
      documents: './src/lib/graphql/cluster-api/*.ts',
      plugins: ['fragment-matcher'],
      config: {
        module: 'commonjs',
      },
    },
  },
  ignoreNoDocuments: true,
};

export default config;
