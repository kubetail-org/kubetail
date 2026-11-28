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

// The composer's model and effort: one button naming both, opening one menu with a tab
// per provider, the tab's models and the picked model's effort levels.
import { Radio } from '@base-ui/react/radio';
import { RadioGroup } from '@base-ui/react/radio-group';
import { Button } from '@kubetail/ui/elements/button';
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { Check } from 'lucide-react';
import { useState } from 'react';

import { SectionHeading } from '@/components/primitives/section-heading';
import { type Model, type ModelPick, providersOf } from '@/lib/models';

// A segmented control, for the provider tabs and the effort levels.
const TRACK = 'flex w-full items-center rounded-lg bg-muted p-[3px] text-muted-foreground';
const SEGMENT =
  'flex h-7 min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md px-2 text-[13px] font-medium transition-colors hover:text-foreground ' +
  'aria-checked:bg-background aria-checked:text-foreground aria-checked:shadow-sm ' +
  'aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm';

// The provider's mark: its initial, on its brand colour where there is one.
const PROVIDER_MARKS: Record<string, string> = {
  anthropic: 'bg-[#d97757] text-neutral-900',
  openai: 'bg-foreground text-background',
};

function ProviderMark({ id, label }: { id: string; label: string }) {
  return (
    <span
      aria-hidden
      className={`flex size-4 shrink-0 items-center justify-center rounded-sm text-xs leading-none font-semibold ${PROVIDER_MARKS[id] ?? 'bg-muted text-muted-foreground'}`}
    >
      {label.charAt(0)}
    </span>
  );
}

// A model's value in the radio group: the id alone is not one, two providers being
// free to name a model the same.
const keyOf = (providerID: string, id: string) => `${providerID}/${id}`;

type ModelPickerProps = {
  /** The catalog. */
  models: Model[];
  pick: ModelPick;
  /** The catalog's entry for `pick`, or null while its provider lists nothing. */
  picked: Model | null;
  onPick: (next: ModelPick) => void;
};

export function ModelPicker({ models, pick, picked, onPick }: ModelPickerProps) {
  // The tab shown, apart from the pick: browsing another provider changes nothing
  // until a model is picked there.
  const providers = providersOf(models);
  const firstTab = picked?.provider.id ?? providers[0]?.id;
  const [tab, setTab] = useState(firstTab);
  const hasEffort = picked !== null && picked.efforts.length > 0;
  let name = `Model: ${pick.model.id}, unavailable`;
  if (picked) name = hasEffort ? `Model: ${picked.label}, effort: ${pick.effort}` : `Model: ${picked.label}`;

  // Another model may not have the level this one is at, so the pick moves to the
  // new model's default rather than to a name it happens to share. The group calls
  // this only for a different model, so the picked one keeps its effort.
  const selectModel = (key: string) => {
    const next = models.find((m) => keyOf(m.provider.id, m.id) === key);
    /* v8 ignore next -- the group offers only the catalog's keys */
    if (!next) return;
    onPick({ model: { providerID: next.provider.id, id: next.id }, effort: next.defaultEffort });
  };

  return (
    <Popover onOpenChange={(open) => open && setTab(firstTab)}>
      <PopoverTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="min-w-0 shrink gap-1.5 px-1.5 text-sm font-normal text-muted-foreground"
          />
        }
        aria-label={name}
      >
        {picked ? (
          <>
            <ProviderMark id={picked.provider.id} label={picked.provider.label} />
            <span className="truncate">{picked.label}</span>
            {hasEffort && <span className="shrink-0 capitalize">{pick.effort}</span>}
          </>
        ) : (
          <>
            <span className="truncate">{pick.model.id}</span>
            <span className="shrink-0">unavailable</span>
          </>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="end"
        sideOffset={6}
        className="flex w-105 max-w-[calc(100vw-2rem)] flex-col overflow-hidden rounded-xl bg-background p-0 shadow-lg dark:bg-card"
        style={{ maxHeight: 'var(--available-height)' }}
      >
        {/* A catalog can outgrow the window: the model list alone shrinks and scrolls,
            so the tabs, the effort and the footer stay in reach. */}
        <div className="flex min-h-0 flex-col p-1">
          {/* One provider needs no tabs. */}
          {providers.length > 1 && (
            <>
              <div role="group" aria-label="Provider" className={TRACK}>
                {providers.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    aria-pressed={p.id === tab}
                    className={SEGMENT}
                    onClick={() => setTab(p.id)}
                  >
                    <ProviderMark id={p.id} label={p.label} />
                    <span className="truncate">{p.label}</span>
                  </button>
                ))}
              </div>
              <div className="my-1 h-px bg-border" />
            </>
          )}
          <SectionHeading label="Model" />
          <RadioGroup
            aria-label="Model"
            className="min-h-0 overflow-y-auto"
            value={keyOf(pick.model.providerID, pick.model.id)}
            onValueChange={(key: string) => selectModel(key)}
          >
            {models
              .filter((m) => m.provider.id === tab)
              .map((m) => (
                <Radio.Root
                  key={m.id}
                  value={keyOf(m.provider.id, m.id)}
                  className="group flex h-8 w-full items-center gap-2 rounded-md px-2 text-left text-sm text-foreground hover:bg-muted data-checked:bg-muted data-checked:font-medium"
                >
                  <span className="flex size-4 shrink-0 items-center justify-center">
                    <Check className="hidden size-4 group-data-checked:block" aria-hidden />
                  </span>
                  <span className="truncate">{m.label}</span>
                </Radio.Root>
              ))}
          </RadioGroup>
          {/* A model with no such knob has nothing to show here. */}
          {hasEffort && (
            <>
              <div className="my-1 h-px bg-border" />
              <SectionHeading label="Effort" />
              <RadioGroup
                aria-label="Effort"
                className={`${TRACK} mb-1`}
                value={pick.effort}
                onValueChange={(effort: string) => onPick({ model: pick.model, effort })}
              >
                {picked?.efforts.map((e) => (
                  <Radio.Root key={e} value={e} className={`${SEGMENT} capitalize`}>
                    {e}
                  </Radio.Root>
                ))}
              </RadioGroup>
            </>
          )}
        </div>
        {/* Nothing manages providers yet, so the link leads nowhere. */}
        <footer className="flex items-center border-t bg-muted/50 px-3 py-2">
          <Button type="button" variant="link" size="xs" className="h-auto px-0 text-foreground">
            Manage providers
          </Button>
        </footer>
      </PopoverContent>
    </Popover>
  );
}
