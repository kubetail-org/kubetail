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

// The breadcrumb bar's namespace picker: every namespace, or one.
import { Layers } from 'lucide-react';

import { VisibleText } from '@/components/primitives/visible-text';
import { useClusterNamespaces } from '@/lib/cluster-namespaces';
import { useNamespaceScope } from '@/lib/namespace-scope';
import { visibleTitle } from '@/lib/visible-text';

import { ScopeMenu, ScopeTrigger } from './scope-menu';
import type { ScopeMenuEntry } from './scope-menu';

// The scope never holds an empty namespace (the route reads one as absent), so it never
// collides with one.
const ALL = '';

export function NamespacePicker() {
  const { namespace, setNamespace } = useNamespaceScope();
  const { namespaces, active, phase } = useClusterNamespaces();
  const loading = active && phase === 'connecting';
  const listed = active && !loading;

  const entries = (query: string): ScopeMenuEntry[] => {
    const q = query.trim().toLowerCase();
    const matches = (name: string) => name.toLowerCase().includes(q);
    // A search names a namespace: Enter takes a match, and never broadens the scope unasked.
    const shown: ScopeMenuEntry[] = [{ type: 'option', id: ALL, label: 'All namespaces', explicit: q !== '' }];
    // The chosen namespace stays reachable while the list cannot show it.
    if (namespace !== undefined && !namespaces.includes(namespace) && matches(namespace)) {
      shown.push({ type: 'option', id: namespace, label: namespace, note: listed ? 'Not found' : undefined });
    }
    if (loading) shown.push({ type: 'label', key: 'loading', label: 'Loading namespaces…' });
    for (const name of namespaces) {
      if (matches(name)) shown.push({ type: 'option', id: name, label: name });
    }
    return shown;
  };

  const label = namespace ?? 'All namespaces';
  return (
    <ScopeMenu
      trigger={
        <ScopeTrigger icon={Layers} aria-label={`Namespace: ${visibleTitle(label)}`}>
          <VisibleText text={label} />
        </ScopeTrigger>
      }
      searchLabel="Filter namespaces"
      placeholder="Filter namespaces…"
      entries={entries}
      current={namespace ?? ALL}
      onPick={(id) => setNamespace(id === ALL ? undefined : id)}
    />
  );
}
