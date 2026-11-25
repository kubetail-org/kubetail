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

// The object panel's YAML tab: the focused object's body as the cache holds it, live, with
// Copy. It watches the object's kind itself, so it stays live whatever page the window
// moves to. A deleted object keeps its last body, marked Deleted.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { Copy } from 'lucide-react';
import { useMemo, useState } from 'react';

import { HighlightedText } from '@/components/primitives/highlighted-text';
import { copyToClipboard } from '@/lib/clipboard';
import { useClusterCachedDataObjects } from '@/lib/cluster-cached-data-objects';
import type { ClusterCachedDataObject } from '@/lib/cluster-cached-data-objects';
import type { FocusedObject } from '@/lib/object-view';
import { objectYaml } from '@/lib/object-yaml';

import { ObjectPanelNote } from './note';

export function ObjectYamlView({ object }: { object: FocusedObject }) {
  // Its own connection: the kind's page may already hold the same watch, and a second
  // shared consumer would never see the snapshot.
  const { objects, active, phase } = useClusterCachedDataObjects(object, { unshared: true });
  const current = objects.find((o) => o.uid === object.uid);
  // The last body seen: held through a reconnect, and once the object is gone. The
  // sidebar keys the panel on the uid, so it never holds another object's.
  const [held, setHeld] = useState<ClusterCachedDataObject>();
  if (current && current !== held) setHeld(current);
  const shown = current ?? held;
  const deleted = phase === 'live' && !current;
  const yaml = useMemo(() => (shown ? objectYaml(shown.rawJSON) : ''), [shown]);

  if (!active) return <ObjectPanelNote>No synced cache for this cluster.</ObjectPanelNote>;
  if (!shown) {
    if (phase === 'live') return <ObjectPanelNote>This object is no longer in the cluster.</ObjectPanelNote>;
    return (
      <ObjectPanelNote>
        <Spinner size="sm" />
        Loading…
      </ObjectPanelNote>
    );
  }
  return (
    <>
      <div className="flex items-center gap-2 border-b py-1 pr-2 pl-4 text-xs text-muted-foreground">
        <span className="min-w-0 flex-1 truncate">
          {deleted ? (
            <span className="text-destructive">Deleted · its last version</span>
          ) : (
            'As cached: managed fields left out, Secret values redacted'
          )}
        </span>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="Copy YAML"
          title="Copy YAML"
          onClick={() => copyToClipboard(yaml)}
        >
          <Copy aria-hidden />
        </Button>
      </div>
      <pre className="min-h-0 flex-1 overflow-auto p-4 font-mono text-xs">
        <code>
          <HighlightedText lang="yaml" text={yaml} />
        </code>
      </pre>
    </>
  );
}
