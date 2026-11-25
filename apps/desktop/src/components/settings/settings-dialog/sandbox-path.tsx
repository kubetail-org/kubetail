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

// The Sandbox pane's PATH: one string, written as in a shell's startup file,
// where $PATH stands for the login shell's PATH, over the folders it expands
// to, each as a sandboxed command judges it now.
import { Button } from '@kubetail/ui/elements/button';
import { useState } from 'react';

import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import type { SandboxPath, SandboxPathFolder } from '@/lib/sandbox-path';
import { visibleSegments } from '@/lib/visible-text';

import { FolderRow } from './folder-row';
import { Group } from './group';

export function SandboxPathSection({ path, onRefreshed }: { path: SandboxPath; onRefreshed: () => void }) {
  const { value, refusal, folders, fault, saving, refreshing, saveError, refreshError, save, refresh } = path;
  // The text being edited; null while it is the saved value.
  const [draft, setDraft] = useState<string | null>(null);
  const text = draft ?? value ?? '';
  // A text field draws no hidden character, so the draft is drawn again,
  // each one spelled: what the user saves is a read grant.
  const hides = visibleSegments(text).some((segment) => segment.spelled);

  // A new PATH can change which binary each executable resolves to.
  const saveAs = async (next: string) => {
    if (await save(next)) {
      setDraft(null);
      onRefreshed();
    }
  };

  return (
    <Group
      title="PATH"
      lede="The folders sandboxed commands search, written as in your shell's startup file: $PATH is your shell's PATH, and ~ your home."
      action={
        <Button
          variant="outline"
          size="sm"
          disabled={refreshing}
          onClick={async () => {
            if (await refresh()) onRefreshed();
          }}
        >
          Import from shell
        </Button>
      }
    >
      <div className="flex flex-col gap-2 rounded-lg border px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="PATH"
            className="min-w-48 flex-1 font-mono text-xs"
            value={text}
            onChange={(e) => setDraft(e.target.value)}
          />
          <Button
            size="sm"
            variant="outline"
            disabled={saving || value === undefined || text === value}
            onClick={() => saveAs(text)}
          >
            Save
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={saving || value === undefined || value === '$PATH'}
            onClick={() => saveAs('$PATH')}
          >
            Reset
          </Button>
        </div>
        {hides && (
          <p className="font-mono text-xs break-all whitespace-pre-wrap" data-testid="draft">
            <VisibleText text={text} trailing="end" />
          </p>
        )}
        {refusal && (
          <p className="text-xs text-destructive">
            <VisibleText text={`This PATH cannot be used: ${refusal} No sandboxed command starts until it is fixed.`} />
          </p>
        )}
        {saveError && (
          <p className="text-xs text-destructive">
            <VisibleText text={saveError} />
          </p>
        )}
        {fault && (
          <p className="text-xs text-muted-foreground">
            {`Kubetail could not read your shell's PATH: ${fault}. $PATH stands for the last one it read.`}
          </p>
        )}
      </div>
      {folders && folders.length > 0 && (
        <ul aria-label="PATH folders" className="divide-y rounded-lg border">
          {keyed(folders).map(({ key, folder }) => (
            // An addable folder always has a target.
            <PathFolderRow key={key} folder={folder} onAdd={() => setDraft(withEntry(text, folder.target!))} />
          ))}
        </ul>
      )}
      {refreshError && <p className="text-xs text-destructive">{refreshError}</p>}
    </Group>
  );
}

// withEntry is text with entry appended as an element of its own.
function withEntry(text: string, entry: string): string {
  return text === '' || text.endsWith(':') ? text + entry : `${text}:${entry}`;
}

// keyed is each folder with a key of its own: a folder can be listed twice,
// so each key counts the times it was listed before.
function keyed(folders: SandboxPathFolder[]): { key: string; folder: SandboxPathFolder }[] {
  const seen = new Map<string, number>();
  return folders.map((folder) => {
    const id = `${folder.origin}:${folder.path}`;
    const n = seen.get(id) ?? 0;
    seen.set(id, n + 1);
    return { key: `${id}#${n}`, folder };
  });
}

function PathFolderRow({ folder, onAdd }: { folder: SandboxPathFolder; onAdd: () => void }) {
  const said = [folder.origin === 'Typed' ? 'typed' : 'from your shell', folder.searched ? 'searched' : 'not searched'];
  if (folder.shared) said.push('shared with its group');
  return (
    <FolderRow
      dir={folder.path}
      muted={!folder.searched}
      control={
        folder.addable && (
          <Button size="sm" variant="ghost" onClick={onAdd}>
            Add
          </Button>
        )
      }
    >
      <p className="text-muted-foreground">{said.join(' · ')}</p>
      {folder.reason && (
        <p className="text-muted-foreground">
          <VisibleText text={folder.reason} />
        </p>
      )}
    </FolderRow>
  );
}
