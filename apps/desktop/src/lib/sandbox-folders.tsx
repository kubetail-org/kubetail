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

// The folders sandboxed commands may read, and read and write: the always
// grants, a chat's own, and what no grant opens. The one reader of the
// sandboxFoldersStateWatch state watch and the folderGrant and folderRevoke mutations.
// The watch sends the whole list again on each change from any window, so
// nothing is asked again.
import { useCallback, useState } from 'react';
import { useMutation } from 'urql';
import type { CombinedError } from 'urql';

import { graphql } from '@/gql';
import type { GrantDuration, SandboxFoldersStateWatchSubscription } from '@/gql/graphql';
import { savedUnread } from '@/lib/graphql/refusal';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const SandboxFoldersStateWatchDocument = graphql(`
  subscription SandboxFoldersStateWatch($chatID: ChatID) {
    sandboxFoldersStateWatch(chatID: $chatID) {
      always {
        id
        path
        write
        refusal
      }
      chat {
        id
        path
        write
        refusal
      }
      never
      closed
      wide
    }
  }
`);

const FolderGrantMutation = graphql(`
  mutation FolderGrant($chatID: ChatID, $path: String!, $write: Boolean!, $duration: GrantDuration!) {
    folderGrant(chatID: $chatID, path: $path, write: $write, duration: $duration) {
      wide
    }
  }
`);

const FolderRevokeMutation = graphql(`
  mutation FolderRevoke($chatID: ChatID, $id: String!, $duration: GrantDuration!) {
    folderRevoke(chatID: $chatID, id: $id, duration: $duration) {
      wide
    }
  }
`);

type Folders = SandboxFoldersStateWatchSubscription['sandboxFoldersStateWatch'];
export type SandboxFolder = Folders['always'][number];

// A state watch: the last frame is the whole state.
const lastFolders = stateWatchReducer((data: SandboxFoldersStateWatchSubscription) => data.sandboxFoldersStateWatch);

/** A refused grant: the sidecar's reason, and the folder to grant instead when it names one (a link's target, or the disk's spelling). */
export type GrantRefusal = { message: string; target: string | null };

function refusalOf(error: CombinedError): GrantRefusal {
  const first = error.graphQLErrors[0];
  const target = first?.extensions?.target;
  return { message: first?.message ?? error.message, target: typeof target === 'string' ? target : null };
}

export type SandboxFolders = {
  /** Undefined until the sidecar has answered. */
  always: SandboxFolder[] | undefined;
  chat: SandboxFolder[] | undefined;
  never: string[] | undefined;
  /** The folders a grant refuses whole, though a folder inside one can be granted. */
  closed: string[] | undefined;
  /** The folders a read grant of which reads more than a project, resolved. */
  wide: string[] | undefined;
  granting: boolean;
  /** The ids whose Remove is in flight. */
  revoking: ReadonlySet<string>;
  /** The last refused grant, until the next one starts. */
  grantError: GrantRefusal | null;
  /** The last refused Remove's message, until the next one starts. */
  revokeError: string | null;
  /** Grants path; resolves whether the sidecar took it. */
  grant: (path: string, write: boolean, duration: GrantDuration) => Promise<boolean>;
  /** Revokes the grant id: the chat's for `Chat`, else an always one. */
  revoke: (id: string, duration: GrantDuration) => Promise<void>;
};

export function useSandboxFolders(chatID?: string): SandboxFolders {
  const chat = chatID ?? null;
  // Unshared: a form mounting beside a list already open (the transcript's
  // offer under the composer's grants) would otherwise wait for the next change.
  const { data: folders } = useFoldedSubscription(
    { query: SandboxFoldersStateWatchDocument, variables: { chatID: chat } },
    lastFolders,
    { unshared: true },
  );
  const [, grantMutation] = useMutation(FolderGrantMutation);
  const [, revokeMutation] = useMutation(FolderRevokeMutation);
  const [granting, setGranting] = useState(false);
  const [revoking, setRevoking] = useState<ReadonlySet<string>>(new Set());
  const [grantError, setGrantError] = useState<GrantRefusal | null>(null);
  const [revokeError, setRevokeError] = useState<string | null>(null);

  const grant = useCallback(
    async (path: string, write: boolean, duration: GrantDuration) => {
      setGranting(true);
      setGrantError(null);
      const { error } = await grantMutation({ chatID: chat, path, write, duration });
      setGranting(false);
      if (error) setGrantError(refusalOf(error));
      return !error || savedUnread(error);
    },
    [grantMutation, chat],
  );

  const revoke = useCallback(
    async (id: string, duration: GrantDuration) => {
      // Removes of different grants overlap, so each settles its own id.
      setRevoking((prev) => new Set(prev).add(id));
      setRevokeError(null);
      const { error } = await revokeMutation({ chatID: chat, id, duration });
      setRevoking((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
      if (error) setRevokeError(refusalOf(error).message);
    },
    [revokeMutation, chat],
  );

  return {
    always: folders?.always,
    chat: folders?.chat,
    never: folders?.never,
    closed: folders?.closed,
    wide: folders?.wide,
    granting,
    revoking,
    grantError,
    revokeError,
    grant,
    revoke,
  };
}
