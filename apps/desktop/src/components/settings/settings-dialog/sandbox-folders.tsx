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

// The Sandbox pane's Folders: what a sandboxed command can read or write, in
// collapsible groups. A folder name is text the user's shell or hand produced,
// so it is drawn through VisibleText with its whitespace kept and the trailing
// spelled: a change approves the folder drawn.
import { Collapsible } from '@base-ui/react/collapsible';
import { Button } from '@kubetail/ui/elements/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { Switch } from '@kubetail/ui/elements/switch';
import { ChevronRight } from 'lucide-react';
import { type ReactNode, useState } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';
import { FolderGrantForm } from '@/components/settings/folder-grant-form';
import { isMacOS } from '@/lib/platform';
import { type SandboxFolder, useSandboxFolders } from '@/lib/sandbox-folders';
import type { SandboxPath } from '@/lib/sandbox-path';

import { FolderRow } from './folder-row';
import { Group, InfoPopover } from './group';

// The folders macOS's privacy control guards for the app, under the home.
const PRIVATE_FOLDERS = ['Documents', 'Desktop', 'Downloads'];

// guardedByMacOS reports whether path lies inside one of the home's private
// folders, where macOS may ask the user before Kubetail reaches it.
function guardedByMacOS(path: string, home: string | undefined): boolean {
  return (
    home !== undefined &&
    PRIVATE_FOLDERS.some((name) => path.startsWith(`${home}/${name}/`) || path === `${home}/${name}`)
  );
}

// Placeholder: awaits a query of the toolchain, shell and system read paths the policy opens.
const TOOL_FOLDERS = [
  { path: '~/.local/bin', on: true },
  { path: '~/.krew', on: true },
  { path: '~/.asdf', on: true },
  { path: '~/google-cloud-sdk', on: false },
];
const SHELL_FOLDERS = ['/opt/homebrew', '/opt/homebrew/Cellar/zsh/5.9'];
const SYSTEM_FOLDERS = ['/bin', '/sbin', '/usr/bin', '/usr/sbin', '/usr/lib', '/usr/share', '/etc', '/dev'];

export function SandboxFolderGroups({ path }: { path: SandboxPath }) {
  const { always, never, closed, wide, revoking, revokeError, grant, revoke } = useSandboxFolders();
  const [adding, setAdding] = useState(false);
  const home = wide?.[0];
  // The folders a command reads through the PATH; the PATH card edits it.
  const searched = path.folders?.filter((folder) => folder.searched) ?? [];

  return (
    <Group
      title="Folders"
      lede="Everything a sandboxed command can read or write, besides the chat's own workspace. A chat can grant more for itself."
      info={<ClosedFolders never={never} closed={closed} />}
      action={
        <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
          Add folder
        </Button>
      }
    >
      {adding && (
        <FolderGrantForm
          durations={['Always']}
          submitLabel="Add"
          onGranted={() => setAdding(false)}
          onCancel={() => setAdding(false)}
        />
      )}
      <div className="divide-y overflow-hidden rounded-lg border">
        <FolderGroup label="Custom" count={always?.length ?? 0} footer={revokeError}>
          {always?.map((folder) => (
            <GrantedFolder
              key={folder.id}
              folder={folder}
              home={home}
              busy={revoking.has(folder.id)}
              onAccess={(write) => grant(folder.path, write, 'Always')}
              onRemove={() => revoke(folder.id, 'Always')}
            />
          ))}
        </FolderGroup>
        <FolderGroup label="PATH" count={searched.length} readOnly>
          {searched.map((folder) => (
            // No two searched folders share the folder they lead to.
            <FolderRow key={folder.target ?? folder.path} dir={folder.target ?? folder.path}>
              {folder.shared && <p className="text-muted-foreground">shared with its group</p>}
            </FolderRow>
          ))}
        </FolderGroup>
        <FolderGroup label="Tools" count={TOOL_FOLDERS.length} readOnly placeholder>
          {TOOL_FOLDERS.map(({ path: dir, on }) => (
            <FolderRow key={dir} dir={dir} muted={!on} control={<Switch aria-label={dir} checked={on} disabled />} />
          ))}
        </FolderGroup>
        <FolderGroup label="Shell" count={SHELL_FOLDERS.length} readOnly placeholder>
          {SHELL_FOLDERS.map((dir) => (
            <FolderRow key={dir} dir={dir} />
          ))}
        </FolderGroup>
        <FolderGroup label="System" count={SYSTEM_FOLDERS.length} readOnly placeholder defaultOpen={false}>
          {SYSTEM_FOLDERS.map((dir) => (
            <FolderRow key={dir} dir={dir} />
          ))}
        </FolderGroup>
      </div>
    </Group>
  );
}

function ClosedFolders({ never, closed }: { never: string[] | undefined; closed: string[] | undefined }) {
  return (
    <InfoPopover label="Closed folders">
      <p>These stay closed even when you grant a folder above them, like your home.</p>
      <PathList label="Never, not even a folder inside" paths={never ?? []}>
        Also other users&apos; homes and Kubetail&apos;s own data.
      </PathList>
      <PathList label="Only a folder inside can be granted" paths={closed ?? []} />
    </InfoPopover>
  );
}

function PathList({ label, paths, children }: { label: string; paths: string[]; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <p className="font-medium text-muted-foreground">{label}</p>
      {children && <p className="text-muted-foreground">{children}</p>}
      <ul aria-label={label} className="font-mono">
        {paths.map((p) => (
          <li key={p} className="break-all whitespace-pre-wrap">
            <VisibleText text={p} trailing="end" />
          </li>
        ))}
      </ul>
    </div>
  );
}

function FolderGroup({
  label,
  count,
  readOnly = false,
  placeholder = false,
  defaultOpen = true,
  footer,
  children,
}: {
  label: string;
  count: number;
  readOnly?: boolean;
  placeholder?: boolean;
  defaultOpen?: boolean;
  footer?: string | null;
  children: ReactNode;
}) {
  return (
    <Collapsible.Root defaultOpen={defaultOpen} render={<section />}>
      <div className="flex items-center gap-2 bg-muted px-4 py-1.5 text-xs text-muted-foreground">
        <Collapsible.Trigger className="group flex flex-1 items-center gap-1.5 outline-none focus-visible:underline">
          <ChevronRight className="size-3.5 transition-transform group-data-panel-open:rotate-90" aria-hidden />
          {`${label} · ${count}`}
        </Collapsible.Trigger>
        {placeholder && <span>Coming soon</span>}
        {readOnly && <span>Read only</span>}
      </div>
      <Collapsible.Panel>
        <ul className="divide-y border-t">{children}</ul>
        {footer && <p className="border-t px-4 py-2 text-xs text-destructive">{footer}</p>}
      </Collapsible.Panel>
    </Collapsible.Root>
  );
}

function GrantedFolder({
  folder,
  home,
  busy,
  onAccess,
  onRemove,
}: {
  folder: SandboxFolder;
  home: string | undefined;
  busy: boolean;
  onAccess: (write: boolean) => void;
  onRemove: () => void;
}) {
  // Destructured: the HTML-sink lint refuses any `.write`.
  const { path, write, refusal } = folder;
  return (
    <FolderRow
      dir={path}
      control={
        <>
          <Select value={write ? 'write' : 'read'} onValueChange={(v) => onAccess(v === 'write')} disabled={busy}>
            <SelectTrigger size="sm" className="w-36">
              <SelectValue>{(v) => (v === 'write' ? 'Read and write' : 'Read')}</SelectValue>
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectItem value="read">Read</SelectItem>
              <SelectItem value="write">Read and write</SelectItem>
            </SelectContent>
          </Select>
          <Button size="sm" variant="ghost" disabled={busy} onClick={onRemove}>
            Remove
          </Button>
        </>
      }
    >
      {refusal && (
        <p className="text-muted-foreground">
          <VisibleText text={refusal} />
        </p>
      )}
      {isMacOS() && guardedByMacOS(path, home) && (
        <p className="text-muted-foreground">macOS may ask you to let Kubetail reach this folder.</p>
      )}
    </FolderRow>
  );
}
