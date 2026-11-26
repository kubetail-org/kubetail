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

// The permission modes and rules: the one reader of the `permissionSettingsGet`
// query and the five mutations over modes and always rules, for the Settings
// section.
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery } from 'urql';
import type { CombinedError } from 'urql';

import { graphql } from '@/gql';
import type {
  PermissionAccessLevel,
  PermissionEffect,
  PermissionMode,
  PermissionPrincipal,
  PermissionSettingsGetQuery,
  PermissionTarget,
} from '@/gql/graphql';
import { refusalOf, savedUnread } from '@/lib/graphql/refusal';

const PermissionSettingsGetDocument = graphql(`
  query PermissionSettingsGet {
    permissionSettingsGet {
      targets {
        target
        defaultMode
        accounts {
          account
          name
          clusterID
          mode
          source
        }
      }
      rules {
        id
        effect
        accessLevel
        principal
        target
        scope
        service
        operation
        resource
        line
      }
      alwaysAsks
      accessControl
    }
  }
`);

const DefaultModeSetMutation = graphql(`
  mutation PermissionDefaultModeSet($target: PermissionTarget!, $mode: PermissionMode!) {
    permissionDefaultModeSet(target: $target, mode: $mode) {
      alwaysAsks
    }
  }
`);

const ModeSetMutation = graphql(`
  mutation PermissionModeSet($target: PermissionTarget!, $account: String!, $mode: PermissionMode!) {
    permissionModeSet(target: $target, account: $account, mode: $mode) {
      alwaysAsks
    }
  }
`);

const ModeClearMutation = graphql(`
  mutation PermissionModeClear($target: PermissionTarget!, $account: String!) {
    permissionModeClear(target: $target, account: $account) {
      alwaysAsks
    }
  }
`);

const RuleAddMutation = graphql(`
  mutation PermissionRuleAdd($input: PermissionRuleInput!) {
    permissionRuleAdd(input: $input) {
      alwaysAsks
    }
  }
`);

const RuleUpdateMutation = graphql(`
  mutation PermissionRuleUpdate($id: String!, $input: PermissionRuleInput!) {
    permissionRuleUpdate(id: $id, input: $input) {
      alwaysAsks
    }
  }
`);

const RuleRemoveMutation = graphql(`
  mutation PermissionRuleRemove($id: String!) {
    permissionRuleRemove(id: $id) {
      alwaysAsks
    }
  }
`);

export type PermissionSettingsValue = PermissionSettingsGetQuery['permissionSettingsGet'];

export type TargetModes = PermissionSettingsValue['targets'][number];

export type AccountMode = TargetModes['accounts'][number];

export type Rule = PermissionSettingsValue['rules'][number];

export type RuleInput = {
  effect: PermissionEffect;
  accessLevel: PermissionAccessLevel;
  /** Null is every principal. */
  principal: PermissionPrincipal | null;
  /** Null is every target, which only Deny and Ask may name. */
  target: PermissionTarget | null;
  /** A path pattern whose first segment is the account (permission-pattern.ts). */
  scope: string;
  service: string;
  operation: string;
  resource: string;
};

export type PermissionSettings = {
  /** Undefined until the sidecar has answered. */
  settings: PermissionSettingsValue | undefined;
  /** Why the settings could not be read, while they cannot. */
  readError: string | null;
  /** Asks the sidecar for the settings again. */
  retry: () => void;
  /** The last mutation's refusal, until the next mutation. */
  error: string | null;
  setDefaultMode: (target: PermissionTarget, mode: PermissionMode) => void;
  /** Resolves to whether the sidecar took the mode. */
  setMode: (target: PermissionTarget, account: string, mode: PermissionMode) => Promise<boolean>;
  clearMode: (target: PermissionTarget, account: string) => void;
  /** Resolves to whether the sidecar took the rule. */
  addRule: (input: RuleInput) => Promise<boolean>;
  /** Resolves to whether the sidecar took the edit. */
  updateRule: (id: string, input: RuleInput) => Promise<boolean>;
  removeRule: (id: string) => void;
};

export function usePermissionSettings(): PermissionSettings {
  // Asked again on every opening and whenever the window regains focus: another
  // window can change the settings, and the clusters watch can learn a cluster,
  // and neither reaches this window's cache.
  const [{ data, error: queryError }, reexecute] = useQuery({
    query: PermissionSettingsGetDocument,
    requestPolicy: 'cache-and-network',
  });
  const refresh = useCallback(() => reexecute({ requestPolicy: 'network-only' }), [reexecute]);
  useEffect(() => {
    window.addEventListener('focus', refresh);
    return () => window.removeEventListener('focus', refresh);
  }, [refresh]);
  const [error, setError] = useState<string | null>(null);
  const [, setDefaultMode] = useMutation(DefaultModeSetMutation);
  const [, setMode] = useMutation(ModeSetMutation);
  const [, clearMode] = useMutation(ModeClearMutation);
  const [, addRule] = useMutation(RuleAddMutation);
  const [, updateRule] = useMutation(RuleUpdateMutation);
  const [, removeRule] = useMutation(RuleRemoveMutation);

  const settings = data?.permissionSettingsGet;
  const readError = refusalOf(queryError);
  return useMemo(() => {
    // Every mutation answers the settings it left, so urql's cache asks the
    // query again; a refusal is drawn until the next mutation. A change that
    // took though its answer could not be read is taken, and says so.
    const run = async (mutation: Promise<{ error?: CombinedError }>) => {
      const answer = await mutation;
      const refusal = refusalOf(answer.error);
      setError(refusal);
      return refusal === null || savedUnread(answer.error);
    };
    return {
      settings,
      readError,
      retry: refresh,
      error,
      setDefaultMode: (target, mode) => run(setDefaultMode({ target, mode })),
      setMode: (target, account, mode) => run(setMode({ target, account, mode })),
      clearMode: (target, account) => run(clearMode({ target, account })),
      addRule: (input) => run(addRule({ input })),
      updateRule: (id, input) => run(updateRule({ id, input })),
      removeRule: (id) => run(removeRule({ id })),
    };
  }, [settings, readError, refresh, error, setDefaultMode, setMode, clearMode, addRule, updateRule, removeRule]);
}
