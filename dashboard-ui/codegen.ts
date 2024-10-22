// Copyright 2024 Andres Morey
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
  schema: '../modules/server/graph/schema.graphqls',
  documents: ['./src/**/*.{ts,tsx}'],
  generates: {
    './src/lib/graphql/__generated__/': {
      preset: 'client',
      plugins: [],
      presetConfig: {
        gqlTagName: 'gql',
        fragmentMasking: false,
      }
    },
    './src/lib/graphql/__generated__/introspection-result.json': {
      plugins: ['fragment-matcher'],
      config: {
        module: 'commonjs'
      },
    },
  },
  ignoreNoDocuments: true,
};

export default config;
