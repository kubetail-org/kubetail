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

// Configuration kinds: what a pod reads, and the limits a namespace sets on what it asks for.
import { countColumn, kind, listColumn, textColumn } from './common';

type ConfigMapBody = { data?: Record<string, unknown>; binaryData?: Record<string, unknown> };

const configMapKeys = (b: ConfigMapBody) => [...Object.keys(b.data ?? {}), ...Object.keys(b.binaryData ?? {})];

// The sidecar replaces a Secret's values and keeps its keys, so the keys are safe to draw.
type SecretBody = { type?: string; data?: Record<string, unknown> };

const secretKeys = (b: SecretBody) => Object.keys(b.data ?? {});

type ResourceQuotaBody = { status?: { hard?: Record<string, string>; used?: Record<string, string> } };

// kubectl's REQUEST and LIMIT: each resource the quota caps as `name: used/hard`, the
// `limits.` ones under Limit.
function quotaUsage(b: ResourceQuotaBody, limits: boolean): string[] {
  const hard = b.status?.hard ?? {};
  return Object.keys(hard)
    .filter((name) => name.startsWith('limits.') === limits)
    .toSorted()
    .map((name) => `${name}: ${b.status?.used?.[name] ?? '0'}/${hard[name]}`);
}

type LimitRangeBody = { spec?: { limits?: { type?: string }[] } };

export const configKinds = [
  kind<ConfigMapBody>('v1', 'configmaps', [
    countColumn('Keys', 64, (b) => configMapKeys(b).length),
    listColumn('Key names', 224, configMapKeys, { hidden: true, mono: true }),
  ]),
  kind<SecretBody>('v1', 'secrets', [
    textColumn('Type', 224, (b) => b.type),
    countColumn('Keys', 64, (b) => secretKeys(b).length),
    listColumn('Key names', 224, secretKeys, { hidden: true, mono: true }),
  ]),
  kind<ResourceQuotaBody>('v1', 'resourcequotas', [
    listColumn('Request', 256, (b) => quotaUsage(b, false)),
    listColumn('Limit', 256, (b) => quotaUsage(b, true)),
  ]),
  // kubectl prints none; the types say what the range limits.
  kind<LimitRangeBody>('v1', 'limitranges', [
    listColumn('Types', 224, (b) => [...new Set(b.spec?.limits?.map((l) => l.type))]),
  ]),
];
