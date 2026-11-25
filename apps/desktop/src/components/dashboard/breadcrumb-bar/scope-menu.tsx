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

// A picker's menu: a search box over a list of options under labels. The caller decides
// what a query matches; the menu owns the box, the list and picking.
import { Popover, PopoverContent, PopoverTrigger } from '@kubetail/ui/elements/popover';
import { cn } from '@kubetail/ui/lib/utils';
import { ChevronDown } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { useCallback, useId, useRef, useState } from 'react';
import type { ComponentProps, KeyboardEvent, ReactElement, RefObject } from 'react';

import { CODE_TEXT } from '@/components/primitives/text-entry';
import { VisibleText } from '@/components/primitives/visible-text';
import { isImeKey } from '@/lib/ime';

export type ScopeMenuEntry =
  | { type: 'label'; key: string; label: string }
  | {
      type: 'option';
      id: string;
      label: string;
      // A muted word after the label, such as *Not found*.
      note?: string;
      // Highlighted only by an arrow or the pointer, never by default, so Enter takes it
      // only when asked to: a search's broadest option.
      explicit?: boolean;
    };

type ScopeTriggerProps = ComponentProps<'button'> & { icon?: LucideIcon };

// The chip that opens a menu, as wide as its content: an icon, what is chosen, and a
// down chevron. The hover needs its `dark:` form to paint over the library's dark fill.
export function ScopeTrigger({ icon: Icon, className, children, ...props }: ScopeTriggerProps) {
  return (
    <button
      type="button"
      className={cn(
        'flex h-7 min-w-0 items-center gap-1.5 rounded-lg border border-sidebar-border bg-background px-2 hover:bg-accent dark:hover:bg-accent',
        className,
      )}
      {...props}
    >
      {Icon && <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />}
      <span className="truncate text-[13px] font-medium text-foreground">{children}</span>
      <ChevronDown aria-hidden className="size-3.25 shrink-0 text-muted-foreground" />
    </button>
  );
}

type ScopeMenuProps = {
  trigger: ReactElement;
  searchLabel: string;
  placeholder: string;
  entries: (query: string) => readonly ScopeMenuEntry[];
  // The chosen option's id.
  current: string | undefined;
  onPick: (id: string) => void;
};

export function ScopeMenu({ trigger, onPick, ...panel }: ScopeMenuProps) {
  const [open, setOpen] = useState(false);
  // Keys the panel, so each open mounts a fresh one: a quick reopen can find the last still
  // mounted through its exit animation, holding its query, highlight and scroll.
  const [opens, setOpens] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);

  const onOpenChange = (next: boolean) => {
    setOpen(next);
    if (next) setOpens((n) => n + 1);
  };

  const pick = (id: string) => {
    setOpen(false);
    onPick(id);
  };

  return (
    <Popover open={open} onOpenChange={onOpenChange}>
      <PopoverTrigger render={trigger} />
      {/* Capped to the room the window leaves: the list shrinks and scrolls, and the search
      box and footer stay on screen. */}
      <PopoverContent
        align="end"
        initialFocus={inputRef}
        className="flex w-75 flex-col p-0"
        style={{ maxHeight: 'var(--available-height)' }}
      >
        {/* The portal mounts the panel only while open, so a closed menu builds no options. */}
        <ScopeMenuPanel key={opens} {...panel} inputRef={inputRef} onPick={pick} />
      </PopoverContent>
    </Popover>
  );
}

type ScopeMenuPanelProps = Omit<ScopeMenuProps, 'trigger'> & { inputRef: RefObject<HTMLInputElement | null> };

function ScopeMenuPanel({ searchLabel, placeholder, entries, current, onPick, inputRef }: ScopeMenuPanelProps) {
  const [query, setQuery] = useState('');
  const listId = useId();
  // The option Enter picks: the one set by opening, an arrow or the pointer while it is
  // still shown, else the first shown that is not `explicit`, else none.
  const [highlight, setHighlight] = useState(current);
  const shown = entries(query);
  const options = shown.flatMap((e) => (e.type === 'option' ? [e] : []));
  const optionIds = options.map((o) => o.id);
  const firstShown = options.find((o) => !o.explicit)?.id;
  const active = highlight !== undefined && optionIds.includes(highlight) ? highlight : firstShown;
  const optionId = (id: string) => `${listId}-${id}`;

  // The list mounts on each open, so this is the scroll it opens at. Stable, or React
  // would call it again on every render.
  const scrollToCurrent = useCallback((list: HTMLElement | null) => {
    list?.querySelector('[data-current]')?.scrollIntoView({ block: 'center' });
  }, []);

  const onKeyDown = (e: KeyboardEvent) => {
    // Accepting a candidate must not pick: the first option is often the broadest.
    if (isImeKey(e.nativeEvent)) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      // No option is shown: nothing to move to.
      if (optionIds.length === 0) return;
      e.preventDefault();
      // With none highlighted, either arrow lands on the first.
      const i = active === undefined ? 0 : optionIds.indexOf(active) + (e.key === 'ArrowDown' ? 1 : -1);
      const next = optionIds[Math.max(0, Math.min(i, optionIds.length - 1))];
      setHighlight(next);
      document.getElementById(optionId(next))?.scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'Enter' && active !== undefined) {
      e.preventDefault();
      onPick(active);
    }
  };

  return (
    <>
      <div className="shrink-0 p-2">
        <input
          {...CODE_TEXT}
          ref={inputRef}
          role="combobox"
          aria-label={searchLabel}
          aria-expanded
          aria-controls={listId}
          placeholder={placeholder}
          value={query}
          aria-activedescendant={active === undefined ? undefined : optionId(active)}
          onChange={(e) => {
            setQuery(e.target.value);
            setHighlight(undefined);
          }}
          onKeyDown={onKeyDown}
          className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:border-ring"
        />
      </div>
      <div
        ref={scrollToCurrent}
        id={listId}
        role="listbox"
        aria-label={searchLabel}
        className="max-h-118 min-h-0 overflow-y-auto border-t p-1.5"
      >
        {shown.length === 0 && <p className="px-2 py-1.5 text-sm text-muted-foreground">No matches</p>}
        {shown.map((e) =>
          e.type === 'label' ? (
            <div key={`label:${e.key}`} role="presentation" className="px-2 pt-2 pb-1 text-xs text-muted-foreground">
              <VisibleText text={e.label} />
            </div>
          ) : (
            // oxlint-disable-next-line jsx-a11y/click-events-have-key-events -- the combobox above takes the keys for every option
            <div
              key={`option:${e.id}`}
              id={optionId(e.id)}
              role="option"
              aria-selected={e.id === active}
              data-current={e.id === current || undefined}
              tabIndex={-1}
              onClick={() => onPick(e.id)}
              onPointerMove={() => setHighlight(e.id)}
              className={cn(
                'flex h-8 items-center gap-2 rounded-md px-2 text-sm aria-selected:bg-muted',
                e.id === current && 'bg-sidebar-accent font-medium aria-selected:bg-sidebar-accent',
              )}
            >
              <span className="truncate">
                <VisibleText text={e.label} />
              </span>
              {e.note && <span className="text-xs text-muted-foreground">{e.note}</span>}
            </div>
          ),
        )}
      </div>
      <div aria-hidden className="flex shrink-0 gap-3 border-t px-3 py-2 text-xs text-muted-foreground">
        <span>↑↓ navigate</span>
        <span>↵ open</span>
        <span>esc close</span>
      </div>
    </>
  );
}
