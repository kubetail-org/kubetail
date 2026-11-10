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
import { DefinitionNode, DocumentNode, Kind, visit } from 'graphql';

// The console sends these operations, written once in dashboard/ops.ts, to
// either the Dashboard or the Cluster API.
const SHARED_OPERATIONS = ['LogRecordsFetch', 'LogRecordsFollow'];

// Keeps the shared operations and the fragments they spread, so codegen
// validates them alone against the schema of the output it is given to.
function keepSharedOperations(documents: { document?: DocumentNode }[]): DocumentNode {
  const definitions = documents.flatMap((file) => file.document?.definitions ?? []);
  const fragments = new Map(
    definitions.filter((def) => def.kind === Kind.FRAGMENT_DEFINITION).map((def) => [def.name.value, def]),
  );

  const kept = new Set<DefinitionNode>();
  const keep = (def: DefinitionNode) => {
    if (kept.has(def)) return;
    kept.add(def);
    visit(def, {
      FragmentSpread(node) {
        const fragment = fragments.get(node.name.value);
        if (fragment) keep(fragment);
      },
    });
  };

  for (const name of SHARED_OPERATIONS) {
    const op = definitions.find((def) => def.kind === Kind.OPERATION_DEFINITION && def.name?.value === name);
    if (!op) throw new Error(`shared operation ${name} not found`);
    keep(op);
  }

  return { kind: Kind.DOCUMENT, definitions: [...kept] };
}

// Each client preset's `config` overrides its defaults so the generated types
// match what the app and Apollo deliver: custom scalars are `any` (the cache's
// type policies convert them), enums are native TS enums, and `__typename` is
// required on nested objects but absent on the operation root, which Apollo's
// addTypename never touches. Keep the two presets' configs the same.
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
    // fragment-matcher reads the schema alone, so this output also carries
    // the check of the shared operations against the Cluster API's schema.
    './src/lib/graphql/cluster-api/__generated__/introspection-result.json': {
      schema: '../modules/cluster-api/graph/schema.graphqls',
      documents: './src/lib/graphql/dashboard/*.ts',
      documentTransforms: [
        {
          transform: ({ documents }) => [{ location: 'shared-operations', document: keepSharedOperations(documents) }],
        },
      ],
      plugins: ['fragment-matcher'],
      config: {
        module: 'commonjs',
      },
    },
  },
  ignoreNoDocuments: true,
};

export default config;
