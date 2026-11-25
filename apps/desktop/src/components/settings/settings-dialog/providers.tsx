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

// The Settings dialog's Providers tab: every provider, in one alphabetical
// list, each set up or not, and each set up or configured from a dialog. Each
// row the sidecar's credential state watch sends follows it.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { cn } from '@kubetail/ui/lib/utils';
import { MoreHorizontal } from 'lucide-react';
import { type ReactNode, useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { VisibleText } from '@/components/primitives/visible-text';
import { GOOGLE_ID, OPENROUTER_ID } from '@/lib/connectors';
import { type Catalog, providersOf, useModels } from '@/lib/models';
import {
  type ProviderCredentialState,
  type ProviderCredentials,
  useProviderCredentials,
} from '@/lib/provider-credentials';
import { useSelectSettingsTab } from '@/lib/settings-tab';

import { Intro } from './group';
import { type CredentialSource, isConnector, ProviderSetupForm } from './provider-setup-form';

type Provider = {
  id: string;
  label: string;
  sources: CredentialSource[];
  /** The provider's credential, from the state watch. */
  credential?: ProviderCredentialState;
  /** What a provider with no key lacks until it is set up. */
  missing?: string;
  /** Checking until the launch has read the keyring. */
  status: 'set-up' | 'not-set-up' | 'checking';
};

type Entry = Omit<Provider, 'status'>;

// Placeholder: Bedrock, Foundry and Ollama await the LLM rebuild.
const PROVIDERS: Entry[] = [
  { id: 'bedrock', label: 'Amazon Bedrock', sources: ['aws-connector', 'access-key'], missing: 'Needs credentials' },
  { id: 'foundry', label: 'Microsoft Foundry', sources: ['azure-connector', 'api-key'], missing: 'Needs credentials' },
  { id: 'ollama', label: 'Ollama', sources: ['host'], missing: 'No host set' },
];

// The connector that signs a provider's own endpoint, by id.
const CONNECTORS: Record<string, CredentialSource> = {
  chatgpt: 'chatgpt-connector',
  [GOOGLE_ID]: 'google-connector',
  [OPENROUTER_ID]: 'openrouter-connector',
};

// Every provider, by name: those from the state watch, then PROVIDERS, set up while
// the catalog serves them, then any other provider the catalog serves, with no
// sources.
function providersFrom(models: Catalog['models'], credentials: ProviderCredentialState[]): Provider[] {
  const served = providersOf(models);
  const reported = credentials.map((c): Provider => {
    const keys: CredentialSource[] = c.keyVariable === null ? [] : ['environment', 'api-key'];
    const connector = CONNECTORS[c.id];
    return {
      id: c.id,
      label: c.label,
      sources: connector ? [...keys, connector] : keys,
      credential: c,
      status: statusOf(c.source),
    };
  });
  const others = PROVIDERS.map((p): Provider => ({
    ...p,
    status: served.some((s) => s.id === p.id) ? 'set-up' : 'not-set-up',
  }));
  const known = new Set([...reported, ...others].map((p) => p.id));
  const unknown = served
    .filter((s) => !known.has(s.id))
    .map(({ id, label }): Provider => ({ id, label, sources: [], status: 'set-up' }));
  return [...reported, ...others, ...unknown].toSorted((a, b) => a.label.localeCompare(b.label));
}

function statusOf(source: ProviderCredentialState['source']): Provider['status'] {
  if (source === 'Restoring') return 'checking';
  return source === 'None' ? 'not-set-up' : 'set-up';
}

export function ProvidersSettings() {
  return (
    <Intro lede="Configure your chats' models. Enable model providers with connectors or API keys.">
      <ProviderList catalog={useModels()} keys={useProviderCredentials()} />
    </Intro>
  );
}

function ProviderList({
  catalog: { models, snapshotComplete },
  keys,
}: {
  catalog: Catalog;
  keys: ProviderCredentials;
}) {
  // The id of the provider whose dialog was last opened. It outlives `open` so
  // the dialog keeps its content while it animates closed; the provider is read
  // from each render's list, so an open dialog follows the state watch.
  const [targetID, setTargetID] = useState<string | null>(null);
  const [open, setOpen] = useState<'configure' | 'remove' | null>(null);
  // Nothing before both first frames: until then every row would read as not set up.
  if (!snapshotComplete || !keys.snapshotComplete) return null;
  const providers = providersFrom(models, keys.credentials);
  const target = providers.find((p) => p.id === targetID);
  const show = (p: Provider, dialog: 'configure' | 'remove') => {
    setTargetID(p.id);
    setOpen(dialog);
  };
  // No trigger opens either dialog, so every change is a close.
  const close = () => setOpen(null);
  return (
    <>
      <ul className="divide-y rounded-lg border">
        {providers.map((p) => (
          <ProviderRow
            key={p.id}
            provider={p}
            onConfigure={() => show(p, 'configure')}
            onRemoveKey={() => show(p, 'remove')}
          />
        ))}
      </ul>
      {target && (
        <Dialog
          open={open === 'configure'}
          onOpenChange={close}
          title={
            <>
              {target.status === 'set-up' ? 'Configure' : 'Set up'} <VisibleText text={target.label} />
            </>
          }
        >
          <ProviderSetupForm
            key={target.id}
            sources={target.sources}
            credential={target.credential}
            onSaveKey={target.credential?.keyVariable ? (key) => keys.saveKey(target.id, key) : undefined}
            onClose={close}
          />
        </Dialog>
      )}
      {target?.credential && (
        <RemoveKeyDialog
          open={open === 'remove'}
          onCancel={close}
          credential={target.credential}
          onRemove={() => {
            close();
            // A failed remove shows on the row as NotDeleted.
            void keys.deleteKey(target.id);
          }}
        />
      )}
    </>
  );
}

// A provider's row: a status dot, its name over one detail line, its action on the right.
function ProviderRow({
  provider,
  onConfigure,
  onRemoveKey,
}: {
  provider: Provider;
  onConfigure: () => void;
  onRemoveKey: () => void;
}) {
  const { label, status, sources, credential } = provider;
  const setUp = status === 'set-up';
  const removable =
    credential !== undefined &&
    credential.keyVariable !== null &&
    (credential.hasSavedKey || credential.failure === 'NotDeleted');
  return (
    <li className="flex items-center gap-3 px-4 py-2.5">
      {/* Filled when set up, a ring when not, a dashed ring while checking: shape as well as color,
          and the detail says it in words. */}
      <span
        aria-hidden
        data-slot="status"
        data-set-up={status === 'checking' ? undefined : setUp}
        className={cn(
          'size-2 shrink-0 rounded-full',
          setUp ? 'bg-emerald-500' : 'border border-muted-foreground/50',
          status === 'checking' && 'border-dashed',
        )}
      />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span data-slot="name" className="truncate text-sm text-popover-foreground">
          <VisibleText text={label} />
        </span>
        <span className="truncate text-xs text-muted-foreground">
          <Detail provider={provider} />
        </span>
      </div>
      {status !== 'checking' &&
        (setUp || removable ? (
          <RowMenu
            label={label}
            onConfigure={sources.length > 0 ? onConfigure : undefined}
            onRemoveKey={removable ? onRemoveKey : undefined}
          />
        ) : (
          <SetUpButton sources={sources} onSetUp={onConfigure} />
        ))}
    </li>
  );
}

/** A credential's line until the launch has read the keyring. */
export const CHECKING_KEYCHAIN = 'Checking your keychain…';

/** What is wrong with a credential's key, in words; undefined when nothing is. */
export function keyFailureText({ failure, source, keyVariable }: ProviderCredentialState): ReactNode {
  // A failure is a key's, so a provider that takes none has none.
  if (keyVariable === null) return undefined;
  switch (failure) {
    case 'NotDeleted':
      return 'Kubetail could not remove the saved key. It returns after a restart.';
    case 'KeyRejected':
      return source === 'SavedKey' ? (
        'The saved key was rejected. Replace or remove it.'
      ) : (
        <>
          <code>
            <VisibleText text={keyVariable} />
          </code>{' '}
          was rejected.
        </>
      );
    case null:
      return undefined;
  }
}

function Detail({ provider: { status, credential, missing } }: { provider: Provider }) {
  if (!credential) return status === 'set-up' ? 'Configured' : missing;
  if (credential.source === 'Restoring') return CHECKING_KEYCHAIN;
  if (credential.keyVariable === null) {
    return credential.source === 'Connector' ? 'Connected on the Connectors tab' : 'Not connected';
  }
  const keyVariable = <VisibleText text={credential.keyVariable} />;
  const failed = keyFailureText(credential);
  if (failed) return failed;
  switch (credential.source) {
    case 'Connector':
      return 'Connected on the Connectors tab';
    case 'SavedKey':
      return 'Using a saved key';
    case 'EnvironmentKey':
      return (
        <>
          Found <code>{keyVariable}</code>
        </>
      );
    case 'None':
      return 'No API key found';
  }
}

// A provider only a connector sets up offers Connect, to the Connectors tab,
// in place of Set up.
function SetUpButton({ sources, onSetUp }: { sources: CredentialSource[]; onSetUp: () => void }) {
  const selectTab = useSelectSettingsTab();
  const needsConnector = sources.every(isConnector);
  return (
    <Button variant="outline" size="sm" onClick={needsConnector ? () => selectTab('Connectors') : onSetUp}>
      {needsConnector ? 'Connect' : 'Set up'}
    </Button>
  );
}

// Placeholder: Disable awaits its mutation, then a confirm Dialog. Configure is
// disabled for a provider with no source to pick.
function RowMenu({
  label,
  onConfigure,
  onRemoveKey,
}: {
  label: string;
  onConfigure?: () => void;
  onRemoveKey?: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
        aria-label={`${label} actions`}
      >
        <MoreHorizontal className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side="bottom">
        <DropdownMenuItem disabled={!onConfigure} onClick={onConfigure}>
          Configure…
        </DropdownMenuItem>
        {onRemoveKey && <DropdownMenuItem onClick={onRemoveKey}>Remove key…</DropdownMenuItem>}
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" disabled>
          Disable
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// The confirm before a saved key goes, naming what signs after it.
function RemoveKeyDialog({
  open,
  onCancel,
  credential,
  onRemove,
}: {
  open: boolean;
  onCancel: () => void;
  credential: ProviderCredentialState;
  onRemove: () => void;
}) {
  const label = <VisibleText text={credential.label} />;
  let follows;
  if (credential.source === 'Connector') {
    follows = <>{label} stays connected on the Connectors tab.</>;
  } else if (credential.hasEnvironmentKey && credential.keyVariable !== null) {
    follows = (
      <>
        Kubetail will use{' '}
        <code>
          <VisibleText text={credential.keyVariable} />
        </code>{' '}
        from your environment.
      </>
    );
  } else {
    follows = <>{label}&apos;s models leave the picker.</>;
  }
  return (
    <Dialog open={open} onOpenChange={onCancel} title={<>Remove the saved key for {label}?</>} description={follows}>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="button" variant="destructive" onClick={onRemove}>
          Remove
        </Button>
      </div>
    </Dialog>
  );
}
