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

// The form a provider is configured from, drawn in its dialog: a choice of
// where its credentials come from, then that source's fields. A saved key is
// handed to the sidecar for the keyring and kept nowhere here; a connector
// source sends the user to the Connectors tab. The rest are placeholders.
import { Radio } from '@base-ui/react/radio';
import { RadioGroup } from '@base-ui/react/radio-group';
import { Button } from '@kubetail/ui/elements/button';
import { Field, FieldLabel } from '@kubetail/ui/elements/field';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@kubetail/ui/elements/select';
import { useId, useState } from 'react';

import { Input } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import type { ProviderCredentialState, ProviderCredentialSource, SaveKeyResult } from '@/lib/provider-credentials';
import { useSelectSettingsTab } from '@/lib/settings-tab';

/** Where a provider's credentials can come from. */
export type CredentialSource =
  | 'environment'
  | 'aws-connector'
  | 'access-key'
  | 'google-connector'
  | 'azure-connector'
  | 'chatgpt-connector'
  | 'openrouter-connector'
  | 'api-key'
  | 'host';

// Each source's radio: its label and the line under it. Placeholder: a source
// marked `soon` awaits a design, then its mutation.
const SOURCES: Record<CredentialSource, { label: string; note?: string; soon?: true }> = {
  environment: { label: 'Environment variable' },
  'aws-connector': { label: 'AWS connector', note: 'Sign in through an AWS connector.' },
  'access-key': { label: 'Access key', note: 'An AWS access key ID and secret.', soon: true },
  'google-connector': { label: 'Google connector', note: 'Sign in with your Google account.' },
  'azure-connector': { label: 'Azure connector', note: 'Sign in with your Azure account.', soon: true },
  'chatgpt-connector': { label: 'ChatGPT connector', note: 'Sign in with your ChatGPT account.' },
  'openrouter-connector': { label: 'OpenRouter connector', note: 'Sign in with your OpenRouter account.' },
  'api-key': { label: 'Saved key', note: "A key Kubetail keeps in your system's password store." },
  host: { label: 'Host', note: 'The address of the server that runs the models.', soon: true },
};

/** Whether a source is a connector's sign-in. */
export function isConnector(source: CredentialSource): boolean {
  return source.endsWith('-connector');
}

// The sources the Connectors tab sets up: the form sends the user there.
const ON_CONNECTORS_TAB = new Set<CredentialSource>(['chatgpt-connector', 'google-connector', 'openrouter-connector']);

// Each failed save's line under the key.
const SAVE_FAILURES: Record<Exclude<SaveKeyResult, 'saved'>, string> = {
  'bad-key': 'That does not look like an API key.',
  'not-saved': "Your system's password store refused the key. Nothing was saved.",
  failed: 'Kubetail could not save the key.',
};

// Placeholder: awaits connectors.
const CONNECTORS = ['Example connector'];

// Placeholder: awaits the Bedrock provider's region list.
const REGIONS = ['us-east-1', 'us-east-2', 'us-west-2', 'eu-central-1', 'eu-west-1', 'ap-northeast-1'];

/**
 * Opens on the source that signs, else the first of `sources`. Without
 * `onSaveKey` the saved key's box stays disabled.
 */
export function ProviderSetupForm({
  sources,
  credential,
  onSaveKey,
  onClose,
}: {
  sources: CredentialSource[];
  credential?: ProviderCredentialState;
  onSaveKey?: (key: string) => Promise<SaveKeyResult>;
  onClose: () => void;
}) {
  const [source, setSource] = useState(() => initialSource(sources, credential));
  const selectTab = useSelectSettingsTab();
  return (
    <div className="flex flex-col gap-4">
      <RadioGroup
        aria-label="Credentials"
        className="flex flex-col gap-3"
        value={source}
        onValueChange={(next: CredentialSource) => setSource(next)}
      >
        {sources.map((s) => (
          <SourceOption key={s} source={s} credential={credential} />
        ))}
      </RadioGroup>
      {source === 'aws-connector' && <AwsConnectorFields />}
      {source === 'api-key' ? (
        <ApiKeyForm onSaveKey={onSaveKey} onClose={onClose} />
      ) : (
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          {ON_CONNECTORS_TAB.has(source) ? (
            <Button
              type="button"
              size="sm"
              onClick={() => {
                onClose();
                selectTab('Connectors');
              }}
            >
              Open Connectors
            </Button>
          ) : (
            // Placeholder: awaits a mutation choosing the provider's credential source.
            <Button type="button" size="sm" disabled>
              Save
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

// Whether a form's source is the one behind a credential's source.
const SIGNS: Record<ProviderCredentialSource, (s: CredentialSource) => boolean> = {
  Restoring: () => false,
  None: () => false,
  EnvironmentKey: (s) => s === 'environment',
  SavedKey: (s) => s === 'api-key',
  Connector: isConnector,
};

function initialSource(sources: CredentialSource[], credential?: ProviderCredentialState): CredentialSource {
  return (credential && sources.find(SIGNS[credential.source])) ?? sources[0];
}

// One source's radio: its label, with a line saying what it means.
function SourceOption({ source, credential }: { source: CredentialSource; credential?: ProviderCredentialState }) {
  const labelID = useId();
  const noteID = useId();
  const { label, soon } = SOURCES[source];
  return (
    <div className="flex items-start gap-2.5">
      <Radio.Root
        value={source}
        aria-labelledby={labelID}
        aria-describedby={noteID}
        className="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border border-input outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 data-checked:border-primary"
      >
        <Radio.Indicator className="size-2 rounded-full bg-primary" />
      </Radio.Root>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="flex items-center gap-2 text-sm text-popover-foreground">
          <span id={labelID}>{label}</span>
          {soon && <span className="text-xs text-muted-foreground">Coming soon</span>}
        </span>
        <span id={noteID} className="text-xs text-muted-foreground">
          <SourceNote source={source} credential={credential} />
        </span>
      </div>
    </div>
  );
}

// A source's note, from what the sidecar holds. It reads the key variable from
// its launch environment or the login shell, once, at start.
function SourceNote({ source, credential }: { source: CredentialSource; credential?: ProviderCredentialState }) {
  if (source === 'environment' && credential && credential.keyVariable !== null) {
    const name = (
      <code>
        <VisibleText text={credential.keyVariable} />
      </code>
    );
    if (!credential.hasEnvironmentKey) {
      return <>{name} is not set. Kubetail reads it from your login shell when it starts.</>;
    }
    return (
      <>
        Kubetail uses {name} from your environment.
        {credential.source !== 'EnvironmentKey' && ' A saved key or a connector is used over it.'}
      </>
    );
  }
  if (source === 'api-key' && credential?.hasSavedKey) return 'A key is saved. Saving another replaces it.';
  return SOURCES[source].note;
}

// The key's box and Save. The key lives in this state alone, and goes once it
// is saved or the form closes.
function ApiKeyForm({
  onSaveKey,
  onClose,
}: {
  onSaveKey?: (key: string) => Promise<SaveKeyResult>;
  onClose: () => void;
}) {
  const id = useId();
  const [key, setKey] = useState('');
  const [saving, setSaving] = useState(false);
  const [failure, setFailure] = useState<string>();
  const save = async (saveKey: (key: string) => Promise<SaveKeyResult>) => {
    setSaving(true);
    setFailure(undefined);
    const result = await saveKey(key.trim());
    setSaving(false);
    if (result === 'saved') {
      setKey('');
      onClose();
    } else {
      setFailure(SAVE_FAILURES[result]);
    }
  };
  return (
    <>
      <Field>
        <FieldLabel htmlFor={id}>API key</FieldLabel>
        <Input
          id={id}
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={key}
          onChange={(e) => {
            setKey(e.target.value);
            setFailure(undefined);
          }}
          disabled={!onSaveKey}
        />
        {failure && (
          <p role="alert" className="text-xs text-destructive">
            {failure}
          </p>
        )}
      </Field>
      <div className="flex justify-end gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onClose}>
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          disabled={!onSaveKey || saving || key.trim() === ''}
          onClick={onSaveKey && (() => void save(onSaveKey))}
        >
          Save
        </Button>
      </div>
    </>
  );
}

function AwsConnectorFields() {
  return (
    <div className="flex gap-3">
      <PickField label="Connector" options={CONNECTORS} />
      <PickField label="Region" options={REGIONS} initial={REGIONS[0]} />
    </div>
  );
}

function PickField({ label, options, initial }: { label: string; options: string[]; initial?: string }) {
  const id = useId();
  return (
    <Field className="flex-1">
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select defaultValue={initial}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={`Pick a ${label.toLowerCase()}`} />
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          {options.map((o) => (
            <SelectItem key={o} value={o}>
              {o}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
}
