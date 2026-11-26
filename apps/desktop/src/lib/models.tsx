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

// What the sidecar can answer with: a state watch, since the ChatGPT plan's models come
// and go with its connection, and every window's picker follows them. The one reader
// of it is `ModelsProvider`, mounted once per window at the root.
import { createContext, useContext, useMemo } from 'react';

import { graphql } from '@/gql';
import type { ModelsStateWatchSubscription } from '@/gql/graphql';
import { stateWatchReducer } from '@/lib/graphql/state-watch';
import { useFoldedSubscription } from '@/lib/graphql/use-folded-subscription';

const ModelsStateWatchDocument = graphql(`
  subscription ModelsStateWatch {
    modelsStateWatch {
      provider {
        id
        label
      }
      id
      label
      efforts
      defaultEffort
    }
  }
`);

export type Model = ModelsStateWatchSubscription['modelsStateWatch'][number];

/** One model, as a send names it: the provider's id and its own. */
export type ModelRef = { providerID: string; id: string };

/**
 * The catalog. `loaded` is the watch's first frame: before it nothing has been
 * answered; after it an empty catalog is a sidecar with no provider, which disables
 * Send with a placeholder.
 */
export type Catalog = { models: Model[]; snapshotComplete: boolean };

const NONE: Model[] = [];

const ModelsContext = createContext<Catalog | null>(null);

/**
 * Holds the window's one `modelsStateWatch`. A state watch's first frame is its snapshot, so
 * a reader mounting into a running stream would never see one; holding it here,
 * above every reader, hands a late reader the catalog the window already has.
 */
export function ModelsProvider({ children }: { children: React.ReactNode }) {
  const { data } = useFoldedSubscription<ModelsStateWatchSubscription, Model[]>(
    { query: ModelsStateWatchDocument },
    stateWatchReducer((next) => next.modelsStateWatch),
  );
  const value = useMemo(() => ({ models: data ?? NONE, snapshotComplete: data !== undefined }), [data]);
  return <ModelsContext.Provider value={value}>{children}</ModelsContext.Provider>;
}

export function useModels(): Catalog {
  const ctx = useContext(ModelsContext);
  if (!ctx) throw new Error('useModels must be used inside <ModelsProvider>');
  return ctx;
}

/** What a send runs on: a model the catalog holds, and one of its effort levels. */
export type ModelPick = { model: ModelRef; effort: string };

/**
 * What a chat's composer starts on: the chat's last answer, or the catalog's first
 * model when there is none or the catalog no longer offers it. A last answer whose
 * provider lists nothing now (the ChatGPT plan, disconnected) is kept, unavailable,
 * so the chat never moves to another provider unasked. Undefined for an empty
 * catalog, which is nothing to pick from.
 */
export function seedPick(models: Model[], lastAnswer: (ModelRef & { effort: string }) | null): ModelPick | undefined {
  if (models.length === 0) return undefined;
  if (lastAnswer && !models.some((m) => m.provider.id === lastAnswer.providerID)) {
    return { model: { providerID: lastAnswer.providerID, id: lastAnswer.id }, effort: lastAnswer.effort };
  }
  // The effort belongs to the model that ran it: another model spelling the same
  // level is not the same level, and carrying it over would quietly change what a
  // turn costs.
  const answered = modelOf(models, lastAnswer);
  const model = answered ?? models[0];
  const effort =
    answered && model.efforts.includes(lastAnswer?.effort ?? '') ? lastAnswer!.effort : model.defaultEffort;
  return { model: { providerID: model.provider.id, id: model.id }, effort };
}

/** The catalog's entry for a ref, or null when it no longer offers it. */
export function modelOf(models: Model[], ref: ModelRef | null): Model | null {
  if (!ref) return null;
  return models.find((m) => m.provider.id === ref.providerID && m.id === ref.id) ?? null;
}

/** The catalog's providers, in the order their first model comes. */
export function providersOf(models: Model[]): Model['provider'][] {
  const seen = new Map<string, Model['provider']>();
  models.forEach((m) => {
    if (!seen.has(m.provider.id)) seen.set(m.provider.id, m.provider);
  });
  return [...seen.values()];
}
