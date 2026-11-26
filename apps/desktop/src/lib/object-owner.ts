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

// The workload an object belongs to, as `<kind>/<name>`, read off its controller owner.
// A Deployment's pods are owned by a ReplicaSet named `<deployment>-<pod-template-hash>`,
// so that owner reads as the Deployment.

type Body = {
  metadata?: {
    labels?: Record<string, string>;
    ownerReferences?: { kind?: string; name?: string; controller?: boolean }[];
  };
};

export function ownerOf(rawJSON: unknown): string | undefined {
  const meta = (rawJSON as Body | null)?.metadata;
  const owner = meta?.ownerReferences?.find((r) => r.controller);
  if (!owner?.kind || !owner.name) return undefined;
  const hash = meta?.labels?.['pod-template-hash'];
  if (owner.kind === 'ReplicaSet' && hash && owner.name.endsWith(`-${hash}`)) {
    return `deploy/${owner.name.slice(0, -hash.length - 1)}`;
  }
  return `${owner.kind === 'Deployment' ? 'deploy' : owner.kind.toLowerCase()}/${owner.name}`;
}
