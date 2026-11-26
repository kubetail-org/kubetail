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

// The Settings dialog's Connectors tab: every service Kubetail can sign in to,
// in one alphabetical list, each connected or not.
import { Button } from '@kubetail/ui/elements/button';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@kubetail/ui/elements/dropdown-menu';
import { Ellipsis, ExternalLink } from 'lucide-react';
import { type ReactNode, useEffect, useState } from 'react';

import { Dialog } from '@/components/primitives/dialog';
import { VisibleText } from '@/components/primitives/visible-text';
import {
  type AWSActions,
  type AWSConnection,
  type ChatGPTActions,
  type ChatGPTConnection,
  type Connector,
  type ConnectorFailure,
  type GitHubActions,
  type GitHubConnection,
  type GoogleActions,
  type GoogleConnection,
  type OpenRouterActions,
  type OpenRouterConnection,
  OPENROUTER_ID,
  useConnectors,
} from '@/lib/connectors';
import { type ProviderCredentialState, useProviderCredentials } from '@/lib/provider-credentials';
import { storageKey } from '@/lib/storage-key';

import { Intro } from './group';
import { CHECKING_KEYCHAIN, keyFailureText } from './providers';

export function ConnectorsSettings() {
  const { connectors, snapshotComplete } = useConnectors();
  const keys = useProviderCredentials();
  const confirm = useFirstConnectionConfirm(connectors.find((c) => c.chatgpt)?.chatgpt?.connection);
  // OpenRouter's signed-out detail names the key that signs, so it waits for the credentials too.
  if (!snapshotComplete || !keys.snapshotComplete) return null;
  const openRouterKey = keys.credentials.find((c) => c.id === OPENROUTER_ID);
  return (
    <Intro lede="Accounts Kubetail signs in to. Model providers and tools use their credentials.">
      <ul className="divide-y rounded-lg border">
        {connectors
          .toSorted((a, b) => a.name.localeCompare(b.name))
          .map((c) => {
            if (c.chatgpt) return <ChatGPTRow key={c.id} connector={c} {...c.chatgpt} onConnect={confirm.sent} />;
            if (c.openrouter) {
              return <OpenRouterRow key={c.id} connector={c} credential={openRouterKey} {...c.openrouter} />;
            }
            if (c.github) return <GitHubRow key={c.id} connector={c} {...c.github} />;
            if (c.aws) return <AWSRow key={c.id} connector={c} {...c.aws} />;
            if (c.google) return <GoogleRow key={c.id} connector={c} {...c.google} />;
            return <ConnectorRow key={c.id} connector={c} />;
          })}
      </ul>
      <Dialog
        open={confirm.open}
        onOpenChange={(open) => !open && confirm.dismiss()}
        title="You're using your ChatGPT plan"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-muted-foreground">
            Requests Kubetail sends on ChatGPT models use your plan. You can manage usage in ChatGPT&apos;s settings.
          </p>
          <Button className="self-end" onClick={confirm.dismiss}>
            Got it
          </Button>
        </div>
      </Dialog>
    </Intro>
  );
}

// planConfirmedKey marks the plan's confirmation as seen, once for every window.
// planPendingKey marks a sign-in this window started and has not confirmed: the
// window's own, kept while the tab is hidden and its pane unmounted.
const planConfirmedKey = storageKey('chatgpt-plan-confirmed');
const planPendingKey = storageKey('chatgpt-plan-pending');

// Storage that refuses reads as unset, so a refused confirmed key shows the
// confirmation again; a refused write is dropped.
function isSet(storage: () => Storage, key: string): boolean {
  try {
    return storage().getItem(key) !== null;
  } catch {
    return false;
  }
}

function setFlag(storage: () => Storage, key: string, on: boolean) {
  try {
    if (on) storage().setItem(key, '1');
    else storage().removeItem(key);
  } catch {
    // Refused: the flag lasts as long as this pane's state.
  }
}

const local = () => localStorage;
const session = () => sessionStorage;

/**
 * The first connection's confirmation, shown only in the window that started it,
 * until the user dismisses it once. The confirmed key is read as the phase
 * reaches Connected, never at mount: another window may have set it since.
 */
function useFirstConnectionConfirm(connection: ChatGPTConnection | undefined) {
  const [sentHere, setSentHere] = useState(() => isSet(session, planPendingKey));
  // Each frame is a new object and a click sends none, so a frame saying the
  // attempt is over, or the one the pane mounts onto, ends what this window sent.
  const [seen, setSeen] = useState<ChatGPTConnection>();
  if (connection !== seen) {
    setSeen(connection);
    if (connection?.phase === 'SignedOut' || connection?.phase === 'Failed') setSentHere(false);
  }
  useEffect(() => setFlag(session, planPendingKey, sentHere), [sentHere]);
  return {
    open: sentHere && connection?.phase === 'Connected' && !isSet(local, planConfirmedKey),
    dismiss: () => {
      setFlag(local, planConfirmedKey, true);
      setSentHere(false);
    },
    sent: () => setSentHere(true),
  };
}

// A string detail may be an account's, so it is spelled out; an element is the pane's own.
function RowText({ name, detail }: { name: string; detail: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="truncate text-sm text-popover-foreground">
        <VisibleText text={name} />
      </span>
      <span className="truncate text-xs text-muted-foreground">
        {typeof detail === 'string' ? <VisibleText text={detail} /> : detail}
      </span>
    </div>
  );
}

// A placeholder: no backend yet, so every action is disabled.
function ConnectorRow({ connector }: { connector: Connector }) {
  return (
    <li className="flex items-center gap-3 px-4 py-2.5">
      <RowText name={connector.name} detail={connector.detail} />
      {connector.connected ? (
        <RowMenu connector={connector} />
      ) : (
        <Button variant="outline" size="sm" disabled>
          Coming soon
        </Button>
      )}
    </li>
  );
}

function RowMenu({ connector }: { connector: Connector }) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
        aria-label={`Actions for ${connector.name}`}
      >
        <Ellipsis className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {connector.manageLabel && (
          <DropdownMenuItem disabled>
            {connector.manageLabel}
            <ExternalLink className="ml-auto" />
          </DropdownMenuItem>
        )}
        <DropdownMenuItem disabled>Reconnect</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" disabled>
          Disconnect
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// failureText is one line saying why a connector's last attempt, session or key
// ended. name is the service's.
const failureText: Record<ConnectorFailure, (name: string) => string> = {
  Declined: (name) => `You declined on ${name}’s page.`,
  ScopeNotGranted: () => 'The sign-in did not allow use of your plan.',
  AccountMismatch: () => 'That is not the account this computer connected. Use a different account to change it.',
  TimedOut: () => 'The sign-in timed out.',
  Unavailable: (name) => `${name} could not be reached.`,
  Refused: (name) => `${name} refused the sign-in.`,
  NotSaved: () => 'This computer could not save the sign-in.',
  NotDeleted: () => 'Kubetail could not remove the key from this machine.',
  SessionEnded: (name) => `${name} ended the session. Connect again.`,
  KeyRejected: (name) => `${name} no longer accepts the key. Connect again.`,
};

function ChatGPTRow({
  connector,
  connection,
  actions,
  onConnect,
}: {
  connector: Connector;
  connection: ChatGPTConnection;
  actions: ChatGPTActions;
  onConnect: () => void;
}) {
  const connect = (fresh: boolean) => {
    onConnect();
    actions.connect(fresh);
  };
  let detail = connector.detail;
  if (connection.phase === 'Restoring') detail = CHECKING_KEYCHAIN;
  if (connection.phase === 'SigningIn') detail = 'Waiting for your browser…';
  if (connection.phase === 'Failed' && connection.failure) detail = failureText[connection.failure](connector.name);
  return (
    <li className="flex flex-col gap-2 px-4 py-2.5">
      <div className="flex items-center gap-3">
        <RowText name={connector.name} detail={detail} />
        {connection.phase === 'SigningIn' && (
          <Button variant="outline" size="sm" onClick={actions.cancel}>
            Cancel
          </Button>
        )}
        {(connection.phase === 'SignedOut' || connection.phase === 'Failed') && (
          <Button variant="outline" size="sm" onClick={() => connect(false)}>
            Continue with ChatGPT
          </Button>
        )}
        {(connection.phase === 'Connected' ||
          (connection.registered && connection.phase !== 'SigningIn' && connection.phase !== 'Restoring')) && (
          <ChatGPTMenu connected={connection.phase === 'Connected'} actions={actions} onFresh={() => connect(true)} />
        )}
      </div>
      {connection.revocationUnconfirmed && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <p className="flex-1">
            Signed out on this computer. ChatGPT did not confirm it. To be sure, disconnect Kubetail in ChatGPT&apos;s
            settings.
          </p>
          <Button variant="outline" size="xs" onClick={actions.openUsage}>
            Open ChatGPT settings
          </Button>
        </div>
      )}
    </li>
  );
}

function ChatGPTMenu({
  connected,
  actions,
  onFresh,
}: {
  connected: boolean;
  actions: ChatGPTActions;
  onFresh: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
        aria-label="Actions for ChatGPT"
      >
        <Ellipsis className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {connected ? (
          <>
            <DropdownMenuItem onClick={actions.openUsage}>
              Manage usage
              <ExternalLink className="ml-auto" />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onClick={actions.disconnect}>
              Disconnect
            </DropdownMenuItem>
          </>
        ) : (
          <DropdownMenuItem onClick={onFresh}>Use a different account</DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// Signed out, the detail names the key that signs the row in the connector's
// place, or what is wrong with it.
function openRouterDetail(
  { phase, failure }: OpenRouterConnection,
  { name, detail: offer }: Connector,
  credential: ProviderCredentialState | undefined,
): ReactNode {
  switch (phase) {
    case 'Restoring':
      return CHECKING_KEYCHAIN;
    case 'SignedOut': {
      if (credential?.source === 'Restoring') return CHECKING_KEYCHAIN;
      const failed = credential && keyFailureText(credential);
      if (failed) return failed;
      if (credential?.source === 'SavedKey') return 'Using a saved key';
      if (credential?.source === 'EnvironmentKey' && credential.keyVariable !== null) {
        return (
          <>
            Using{' '}
            <code>
              <VisibleText text={credential.keyVariable} />
            </code>
          </>
        );
      }
      return offer;
    }
    case 'SigningIn':
      return 'Waiting for the browser…';
    case 'Connected':
      return 'Key "Kubetail"';
    case 'Failed':
      return failure ? failureText[failure](name) : offer;
  }
}

function OpenRouterRow({
  connector,
  credential,
  connection,
  actions,
}: {
  connector: Connector;
  credential: ProviderCredentialState | undefined;
  connection: OpenRouterConnection;
  actions: OpenRouterActions;
}) {
  return (
    <GrantRow
      connector={connector}
      connection={connection}
      detail={openRouterDetail(connection, connector, credential)}
      actions={actions}
      grant="key"
      onOpenSettings={actions.openKeys}
      retryHelp='Retry, or delete the key labelled "Kubetail" on OpenRouter.'
      disconnectHelp='It stays active on OpenRouter until you delete the key labelled "Kubetail" in your OpenRouter settings.'
    />
  );
}

function gitHubDetail({ phase, userCode, failure }: GitHubConnection, { name, detail }: Connector): ReactNode {
  switch (phase) {
    case 'Restoring':
      return CHECKING_KEYCHAIN;
    case 'SignedOut':
    case 'Connected':
      return detail;
    case 'SigningIn':
      // The code is GitHub's: drawn spelled out, as the user types what they see.
      return userCode ? (
        <>
          Enter{' '}
          <code>
            <VisibleText text={userCode} />
          </code>{' '}
          on GitHub&rsquo;s page
        </>
      ) : (
        'Asking GitHub for a code…'
      );
    case 'Failed':
      return failure ? failureText[failure](name) : detail;
  }
}

function GitHubRow({
  connector,
  connection,
  actions,
}: {
  connector: Connector;
  connection: GitHubConnection;
  actions: GitHubActions;
}) {
  return (
    <GrantRow
      connector={connector}
      connection={connection}
      detail={gitHubDetail(connection, connector)}
      actions={actions}
      grant="token"
      onOpenSettings={actions.openSettings}
      retryHelp="Retry, or revoke Kubetail in your GitHub settings."
      disconnectHelp="It stays authorized on GitHub until you revoke Kubetail in your GitHub settings."
    />
  );
}

// GrantRow is a connector whose sign-in grants a key or token Kubetail keeps but
// cannot revoke: Disconnect forgets it here, and the provider's settings end it.
function GrantRow({
  connector: { name },
  connection: { phase, failure },
  detail,
  actions,
  grant,
  onOpenSettings,
  retryHelp,
  disconnectHelp,
}: {
  connector: Connector;
  connection: Pick<OpenRouterConnection, 'phase' | 'failure'>;
  detail: ReactNode;
  actions: { connect: () => void; cancel: () => void; disconnect: () => void };
  grant: 'key' | 'token';
  onOpenSettings: () => void;
  retryHelp: string;
  disconnectHelp: string;
}) {
  const [confirming, setConfirming] = useState(false);
  // It is still in the keyring: only another Disconnect moves on, so no Connect.
  const notDeleted = phase === 'Failed' && failure === 'NotDeleted';
  const openSettings = `Open ${name} settings`;
  return (
    <li className="flex flex-col gap-2 px-4 py-2.5">
      <div className="flex items-center gap-3">
        <RowText name={name} detail={detail} />
        {phase === 'SigningIn' && (
          <Button variant="outline" size="sm" onClick={actions.cancel}>
            Cancel
          </Button>
        )}
        {(phase === 'SignedOut' || (phase === 'Failed' && !notDeleted)) && (
          <Button variant="outline" size="sm" onClick={actions.connect}>
            Connect
          </Button>
        )}
        {phase === 'Connected' && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
              aria-label={`Actions for ${name}`}
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onClick={onOpenSettings}>
                {openSettings}
                <ExternalLink className="ml-auto" />
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onClick={() => setConfirming(true)}>
                Disconnect
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {notDeleted && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <p className="flex-1">It will be used again after a restart. {retryHelp}</p>
          <Button variant="outline" size="xs" onClick={onOpenSettings}>
            {openSettings}
          </Button>
          <Button variant="outline" size="xs" onClick={actions.disconnect}>
            Retry
          </Button>
        </div>
      )}
      <Dialog
        open={confirming}
        onOpenChange={setConfirming}
        title={`Disconnect ${name}?`}
        description={`Kubetail will forget this ${grant}. ${disconnectHelp}`}
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" className="mr-auto" onClick={onOpenSettings}>
            {openSettings}
          </Button>
          <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              setConfirming(false);
              actions.disconnect();
            }}
          >
            Disconnect
          </Button>
        </div>
      </Dialog>
    </li>
  );
}

function awsDetail({ phase, failure }: AWSConnection, { name, detail }: Connector): string {
  switch (phase) {
    case 'SigningIn':
      return 'Waiting for the browser…';
    case 'Failed':
      return failure ? failureText[failure](name) : detail;
    case 'Restoring':
      return CHECKING_KEYCHAIN;
    case 'SignedOut':
    case 'Connected':
      return detail;
  }
}

// The connector's detail is the principal's ARN once connected, an account's
// string that RowText spells out.
function AWSRow({
  connector,
  connection,
  actions,
}: {
  connector: Connector;
  connection: AWSConnection;
  actions: AWSActions;
}) {
  const [confirming, setConfirming] = useState(false);
  const { phase } = connection;
  // The session is still in the keyring: only another Disconnect moves on, so no Connect.
  const notDeleted = phase === 'Failed' && connection.failure === 'NotDeleted';
  return (
    <li className="flex flex-col gap-2 px-4 py-2.5">
      <div className="flex items-center gap-3">
        <RowText name={connector.name} detail={awsDetail(connection, connector)} />
        {phase === 'SigningIn' && (
          <Button variant="outline" size="sm" onClick={actions.cancel}>
            Cancel
          </Button>
        )}
        {(phase === 'SignedOut' || (phase === 'Failed' && !notDeleted)) && (
          <Button variant="outline" size="sm" onClick={actions.connect}>
            Connect
          </Button>
        )}
        {phase === 'Connected' && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
              aria-label="Actions for AWS"
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem variant="destructive" onClick={() => setConfirming(true)}>
                Disconnect
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {notDeleted && (
        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <p className="flex-1">It will be used again after a restart.</p>
          <Button variant="outline" size="xs" onClick={actions.disconnect}>
            Retry
          </Button>
        </div>
      )}
      <Dialog
        open={confirming}
        onOpenChange={setConfirming}
        title="Disconnect AWS?"
        description="Kubetail will forget this sign-in. AWS keeps the session valid until it expires."
      >
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => setConfirming(false)}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              setConfirming(false);
              actions.disconnect();
            }}
          >
            Disconnect
          </Button>
        </div>
      </Dialog>
    </li>
  );
}

function googleDetail({ phase, project, failure }: GoogleConnection, { name, detail }: Connector): string {
  switch (phase) {
    case 'SigningIn':
      return 'Waiting for the browser…';
    case 'Connected':
      return project ? `${detail} · ${project}` : detail;
    case 'Failed':
      if (failure === 'ScopeNotGranted') return 'The sign-in did not allow access to Google Cloud.';
      return failure ? failureText[failure](name) : detail;
    case 'Restoring':
      return CHECKING_KEYCHAIN;
    case 'SignedOut':
      return detail;
  }
}

// The connector's detail is the account's email once connected, and the project
// its models run in, both strings RowText spells out.
function GoogleRow({
  connector,
  connection,
  actions,
}: {
  connector: Connector;
  connection: GoogleConnection;
  actions: GoogleActions;
}) {
  const { phase, failure } = connection;
  return (
    <li className="flex flex-col gap-2 px-4 py-2.5">
      <div className="flex items-center gap-3">
        <RowText name={connector.name} detail={googleDetail(connection, connector)} />
        {phase === 'SigningIn' && (
          <Button variant="outline" size="sm" onClick={actions.cancel}>
            Cancel
          </Button>
        )}
        {phase === 'Failed' && failure === 'AccountMismatch' && (
          <Button variant="outline" size="sm" onClick={() => actions.connect(true)}>
            Use a different account
          </Button>
        )}
        {(phase === 'SignedOut' || phase === 'Failed') && (
          <Button variant="outline" size="sm" onClick={() => actions.connect(false)}>
            Connect
          </Button>
        )}
        {phase === 'Connected' && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<Button variant="ghost" size="icon" className="size-7 rounded-md" />}
              aria-label="Actions for Google"
            >
              <Ellipsis className="size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem variant="destructive" onClick={actions.disconnect}>
                Disconnect
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>
      {phase === 'Connected' && !connection.project && (
        <p className="text-xs text-muted-foreground">
          No Google Cloud project. Set <code>GOOGLE_CLOUD_PROJECT</code> or gcloud&rsquo;s default project, then restart
          Kubetail.
        </p>
      )}
      {connection.revocationUnconfirmed && (
        <p className="text-xs text-muted-foreground">
          Signed out on this computer. Google did not confirm it. To be sure, remove Kubetail in your Google
          Account&rsquo;s third-party connections.
        </p>
      )}
    </li>
  );
}
