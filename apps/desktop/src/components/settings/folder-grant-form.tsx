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

// The form a folder is granted from: Settings' Add row, and the offer under a
// sandboxed command that failed. The path is the user's typing, never a
// command's output.
import { Button } from '@kubetail/ui/elements/button';
import { Checkbox } from '@kubetail/ui/elements/checkbox';
import { Tabs, TabsList, TabsTrigger } from '@kubetail/ui/elements/tabs';
import { useState } from 'react';

import { Input } from '@/components/primitives/text-entry';
import type { GrantDuration } from '@/gql/graphql';
import { useSandboxFolders } from '@/lib/sandbox-folders';

// cleanPath is path as the sidecar compares it: repeated and trailing slashes
// dropped, and each . and .. taken by name. The sidecar refuses a path that
// is not resolved, so this is exact for every path a grant can take.
function cleanPath(path: string): string {
  const parts: string[] = [];
  path.split('/').forEach((part) => {
    if (part === '..') parts.pop();
    else if (part !== '' && part !== '.') parts.push(part);
  });
  return path.startsWith('/') ? `/${parts.join('/')}` : parts.join('/');
}

function wideWarning(path: string): string {
  if (path === '/Volumes') return 'This lets commands read every disk mounted on this Mac.';
  return (
    'This lets commands read everything in your home folder except the credential folders Kubetail knows of, ' +
    "listed under Never readable, and Kubetail's own files. Any other secret kept in your home becomes readable."
  );
}

const DURATION_WORDS: Record<GrantDuration, string> = { Chat: 'For this chat', Always: 'Always' };

type Props = {
  chatID?: string;
  /** ['Always'], or ['Chat', 'Always']. The first starts picked, so the narrower goes first. */
  durations: GrantDuration[];
  submitLabel: string;
  /** What the field and the checkbox start from. */
  initial?: { path: string; write: boolean };
  onGranted?: () => void;
  /** Draws Cancel when set. */
  onCancel?: () => void;
};

export function FolderGrantForm({ chatID, durations, submitLabel, initial, onGranted, onCancel }: Props) {
  const { wide, granting, grantError, grant } = useSandboxFolders(chatID);
  // Destructured: the HTML-sink lint refuses any `.write`.
  const { path: initialPath = '', write: initialWrite = false } = initial ?? {};
  const [path, setPath] = useState(initialPath);
  const [write, setWrite] = useState(initialWrite);
  const [duration, setDuration] = useState(durations[0]);
  const typed = cleanPath(path);

  // Sent as typed, whitespace included: the folder drawn is the folder granted.
  const submit = async () => {
    if (await grant(path, write, duration)) {
      setPath('');
      setWrite(false);
      onGranted?.();
    }
  };

  return (
    <div className="flex flex-col gap-1">
      {/* Wraps in a narrow pane, such as the dashboard's right sidebar: the field keeps the first line. */}
      <div className="flex flex-wrap items-center gap-2">
        <Input
          aria-label="Folder to grant"
          className="min-w-48 flex-1 font-mono text-xs"
          placeholder="/path/to/folder"
          value={path}
          onChange={(e) => setPath(e.target.value)}
        />
        {/* oxlint-disable-next-line jsx-a11y/label-has-associated-control -- Checkbox renders its control inside */}
        <label className="flex shrink-0 items-center gap-1.5 text-xs">
          <Checkbox checked={write} onCheckedChange={(checked) => setWrite(checked)} />
          read and write
        </label>
        {durations.length > 1 && (
          <Tabs value={duration} onValueChange={(d) => setDuration(d as GrantDuration)}>
            <TabsList aria-label="How long">
              {durations.map((d) => (
                <TabsTrigger key={d} value={d}>
                  {DURATION_WORDS[d]}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        )}
        <Button size="sm" variant="outline" disabled={granting || path === ''} onClick={submit}>
          {submitLabel}
        </Button>
        {onCancel && (
          <Button size="sm" variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
        )}
      </div>
      {wide?.includes(typed) && <p className="text-xs text-muted-foreground">{wideWarning(typed)}</p>}
      {grantError && (
        <div className="text-xs text-destructive">
          <p>{grantError.message}</p>
          {grantError.target && (
            <Button size="sm" variant="link" className="px-0" onClick={() => setPath(grantError.target ?? '')}>
              {`Grant ${grantError.target}`}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
