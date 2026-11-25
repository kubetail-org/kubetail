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

// A kind's identity — Kubernetes' GVR, but carrying combined `apiVersion` ("apps/v1",
// core "v1") because the wire, the cache, and `ServerKind` all speak apiVersion.
// `gvrKey` is the one canonical string form — kinds-catalog map key, objects-watch
// provenance half, and column-registry key — so those can't drift apart.
export type GVR = { apiVersion: string; resource: string };

export function gvrKey(g: GVR): string {
  return `${g.apiVersion}/${g.resource}`;
}

// The canonical spelling the sidecar syncs Events under — exactly one of core v1 /
// events.k8s.io/v1 is mirrored (see the sidecar's eventsKind constants).
export const EVENTS_GVR: GVR = { apiVersion: 'v1', resource: 'events' };
