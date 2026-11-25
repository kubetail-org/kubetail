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

// The window's active kubeconfig context. Source of truth is the `kubeContext` URL
// search param on `_app` (per-window, deep-linkable; see
// docs/adr/2026-08-09-url-params-as-window-state.md). Frontend view-scope only —
// never rewrites the kubeconfig's current-context.
import { useNavigate, useSearch } from '@tanstack/react-router';
import { useEffect } from 'react';

import { subscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { useKubeConfig } from '@/lib/kube-config';
import type { KubeContextInfo } from '@/lib/kube-config';

type ActiveKubeContext = {
  // Resolved name: the URL param when it names a present context, else the
  // `KubeConfig.defaultContext`, else '' (clusters not reported yet, or no
  // usable default).
  context: string;
  // The resolved context's full record (cluster/user); undefined until resolved.
  active: KubeContextInfo | undefined;
  contexts: KubeContextInfo[];
  // Contexts are listed and none resolved: neither the default cluster nor the
  // kubeconfig's current context is usable.
  noneSelected: boolean;
  // Distinguishes "connecting" from "connected, empty kubeconfig" for the picker.
  phase: SubscriptionPhase;
  // Writes the URL search param, leaving the rest of the window's scope alone.
  setContext: (name: string) => void;
};

export function useActiveKubeContext(): ActiveKubeContext {
  // `strict: false`: read the merged search of whatever `_app` descendant renders this.
  const { kubeContext: param } = useSearch({ strict: false });
  const navigate = useNavigate();
  const { kubeConfig, connected } = useKubeConfig();

  // `kubeConfig === null` is exactly "the registry's snapshot is not complete" — it
  // is derived from `clusters`, which the provider holds back until the Bookmark.
  const phase = subscriptionPhase(kubeConfig !== null, connected);
  const contexts = kubeConfig?.contexts ?? [];
  // A param naming a gone context yields to the default — a stale deep link
  // never points at nothing.
  const valid = param && contexts.some((c) => c.name === param) ? param : undefined;
  const context = valid ?? kubeConfig?.defaultContext ?? '';
  const active = contexts.find((c) => c.name === context);

  // Leaves `chat` alone: the out-of-scope notice's "Switch to it" follows a chat to
  // its cluster through this. The omnibox, whose switch closes the chat, navigates
  // itself.
  const setContext = (name: string) => {
    void navigate({ to: '.', search: (prev) => ({ ...prev, kubeContext: name }) });
  };

  return { context, active, contexts, noneSelected: context === '' && contexts.length > 0, phase, setContext };
}

// Fixes the window's cluster in the `kubeContext` param once one resolves, so a later
// change to the default moves no open window. A window that resolves nothing writes
// nothing and follows the default until one resolves; a param naming a gone context
// is rewritten to what replaced it. Replaces the history entry: it is not a move the
// user made, so Back must not undo it. Called once per window, by `AppLayout`'s
// `FixWindowContext`.
export function useFixWindowContext() {
  const { kubeContext: param } = useSearch({ strict: false });
  const navigate = useNavigate();
  const { context, phase } = useActiveKubeContext();

  useEffect(() => {
    if (phase === 'connecting' || context === '' || context === param) return;
    void navigate({ to: '.', search: (prev) => ({ ...prev, kubeContext: context }), replace: true });
  }, [context, phase, param, navigate]);
}
