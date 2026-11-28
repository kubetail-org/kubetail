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

// What an object's detail reads off any kind's `rawJSON`: its controller, its conditions and
// its labels.

type Body = {
  metadata?: {
    labels?: Record<string, string>;
    ownerReferences?: { kind?: string; name?: string; controller?: boolean }[];
  };
  status?: { conditions?: unknown };
};

export type ObjectCondition = { type?: string; status?: string; reason?: string; lastTransitionTime?: string };

const body = (rawJSON: unknown) => (rawJSON ?? {}) as Body;

// The direct controller owner reference, as `<kind> <name>`: a pod's ReplicaSet, not the
// Deployment `ownerOf` reads it as.
export function controllerOf(rawJSON: unknown): string | undefined {
  const owner = body(rawJSON).metadata?.ownerReferences?.find((r) => r.controller);
  if (!owner?.kind || !owner.name) return undefined;
  return `${owner.kind} ${owner.name}`;
}

const text = (value: unknown) => (typeof value === 'string' ? value : undefined);

// `status.conditions`, or nothing for a kind that keeps none there. A custom resource's status
// is whatever its controller wrote, so an entry that is not an object is dropped and a field
// that is not a string reads as missing.
export function conditionsOf(rawJSON: unknown): ObjectCondition[] | undefined {
  const conditions = body(rawJSON).status?.conditions;
  if (!Array.isArray(conditions)) return undefined;
  return conditions
    .filter((c): c is Record<string, unknown> => typeof c === 'object' && c !== null && !Array.isArray(c))
    .map((c) => ({
      type: text(c.type),
      status: text(c.status),
      reason: text(c.reason),
      lastTransitionTime: text(c.lastTransitionTime),
    }));
}

export function labelsOf(rawJSON: unknown): [string, string][] {
  return Object.entries(body(rawJSON).metadata?.labels ?? {});
}
