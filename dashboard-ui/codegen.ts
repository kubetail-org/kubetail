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

import { CodegenConfig } from '@graphql-codegen/cli';

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
      }
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
      }
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
