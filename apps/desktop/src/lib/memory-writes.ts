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

// The user's memory writes to the sidecar, and the rules a write must fit.
// Every save from here marks the memory as the user's. The webview calls a
// memory a note.
import { useMutation } from 'urql';

import { graphql } from '@/gql';

const MemorySaveMutation = graphql(`
  mutation MemorySave($input: MemorySaveInput!) {
    memorySave(input: $input) {
      id
    }
  }
`);

const MemoryDeleteMutation = graphql(`
  mutation MemoryDelete($id: MemoryID!) {
    memoryDelete(id: $id)
  }
`);

// The sidecar's shape rules (memory.checkNote). A name is a slug, since it is
// the key the model saves and forgets by; the user never sees one.
const NAME_MAX = 48;
const BODY_MAX_BYTES = 500;
export const BODY_RULE = `At most ${BODY_MAX_BYTES} bytes.`;

const utf8 = new TextEncoder();
export const bodyFits = (body: string) => utf8.encode(body).length <= BODY_MAX_BYTES;

// Lower-case ASCII words joined by hyphens. Accents come off, so `Café` keeps
// its letters; other scripts fold to nothing.
const fold = (text: string) =>
  text
    .normalize('NFD')
    .replaceAll(/\p{M}/gu, '')
    .toLowerCase()
    .replaceAll(/[^a-z0-9]+/g, '-');

// The name derived from a body, cut at a word boundary when one is in reach.
export function slugify(text: string): string {
  const slug = fold(text).replaceAll(/^-|-$/g, '');
  const cut = slug.slice(0, NAME_MAX);
  if (slug.length <= NAME_MAX || slug[NAME_MAX] === '-') return cut;
  const hyphen = cut.lastIndexOf('-');
  return hyphen === -1 ? cut : cut.slice(0, hyphen);
}

// `base`, or the first of `base-2`, `base-3`, … not in `taken`. Cutting `base`
// to fit the suffix can leave a hyphen at its end, which would double.
export function uniqueName(base: string, taken: ReadonlySet<string>): string {
  if (!taken.has(base)) return base;
  for (let n = 2; ; n += 1) {
    const suffix = `-${n}`;
    const name = base.slice(0, NAME_MAX - suffix.length).replace(/-+$/, '') + suffix;
    if (!taken.has(name)) return name;
  }
}

// A new note's name: its body's opening words, clear of `taken`.
export const nameFor = (body: string, taken: ReadonlySet<string>) => uniqueName(slugify(body) || 'note', taken);

// Why a save was refused, by the code the sidecar sent.
const REFUSALS: Record<string, string> = {
  KUBETAIL_RECORD_NOT_FOUND: 'This note or its cluster is gone.',
  KUBETAIL_MEMORY_NAME_TAKEN: 'Another note here starts the same way. Reword its opening.',
  KUBETAIL_MEMORY_FULL: 'There is no room for this note here. Shorten or delete one first.',
  KUBETAIL_MEMORY_SECRET: 'This looks like a credential. Notes never keep one.',
  KUBETAIL_VALIDATION_ERROR: `A note is at most ${BODY_MAX_BYTES} bytes.`,
};

// `clusterID` null saves the memory for every cluster.
export type MemorySaveInput = { id: string | null; clusterID: string | null; name: string; body: string };

export function useMemoryWrites(): {
  // Resolves to why the sidecar refused, or null once saved.
  save: (input: MemorySaveInput) => Promise<string | null>;
  // Resolves to whether the memory is gone. One already gone counts as
  // deleted, so a retry can succeed.
  remove: (id: string) => Promise<boolean>;
} {
  const [, memorySave] = useMutation(MemorySaveMutation);
  const [, memoryDelete] = useMutation(MemoryDeleteMutation);

  const save = async (input: MemorySaveInput) => {
    const result = await memorySave({ input });
    if (!result.error) return null;
    const code = result.error.graphQLErrors[0]?.extensions?.code;
    return REFUSALS[String(code)] ?? 'The note could not be saved.';
  };

  const remove = async (id: string) => {
    const { error } = await memoryDelete({ id });
    return !error || error.graphQLErrors[0]?.extensions?.code === 'KUBETAIL_RECORD_NOT_FOUND';
  };

  return { save, remove };
}
