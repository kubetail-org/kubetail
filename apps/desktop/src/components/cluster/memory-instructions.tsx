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

// A scope's standing instructions. They save on Cmd/Ctrl+Enter or when the
// box loses focus, never mid-edit, so a chat never reads them half-written.
// Nothing stores them yet: the text lives here and is lost on unmount. The
// parent remounts this on a scope switch, so text never carries over to
// another scope.
import { cn } from '@kubetail/ui/lib/utils';
import { Check } from 'lucide-react';
import { useEffect, useId, useRef, useState } from 'react';
import type { KeyboardEvent } from 'react';

import ResizeThumb from '@/assets/resize-thumb.svg?react';
import { Textarea } from '@/components/primitives/text-entry';

// How long "Saved" shows after a save.
export const SAVED_SHOWN_MS = 2000;

export function MemoryInstructions({
  description = 'How Kubetail should behave on this cluster.',
}: {
  description?: string;
}) {
  const id = useId();
  const [text, setText] = useState('');
  const [savedText, setSavedText] = useState('');
  const [saved, setSaved] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => () => clearTimeout(timer.current), []);

  const onChange = (value: string) => {
    setText(value);
    setSaved(false);
    clearTimeout(timer.current);
  };

  const save = () => {
    if (text === savedText) return;
    setSavedText(text);
    setSaved(true);
    timer.current = setTimeout(() => setSaved(false), SAVED_SHOWN_MS);
  };

  const onKeyDown = (e: KeyboardEvent) => {
    // An Enter that ends an IME composition belongs to the input method.
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.nativeEvent.isComposing) {
      e.preventDefault();
      save();
    }
  };

  return (
    <section className="flex flex-col gap-2.5">
      <div className="flex items-end justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <label htmlFor={id} className="text-sm font-medium">
            Instructions
          </label>
          <p className="text-xs text-muted-foreground">{description}</p>
        </div>
        <p
          role="status"
          aria-hidden={!saved}
          className={cn(
            'flex shrink-0 items-center gap-1 text-xs text-muted-foreground transition-opacity duration-300',
            !saved && 'opacity-0',
          )}
        >
          <Check aria-hidden className="size-3" />
          Saved
        </p>
      </div>
      <div className="group/box relative">
        <Textarea
          prose
          id={id}
          value={text}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={save}
          placeholder="e.g. keep explanations brief and to the point"
          // A fixed box the user can stretch: the library's grows with its text.
          className="field-sizing-fixed h-15 min-h-15 resize-y border-border bg-background px-3 py-2.5 text-sm not-placeholder-shown:border-input hover:border-input md:text-sm dark:bg-background [&::-webkit-resizer]:bg-transparent"
        />
        {/* The design's grip, in the border's color. The native one beneath it, made transparent, takes the drag. */}
        <ResizeThumb
          aria-hidden
          className="pointer-events-none absolute right-0.5 bottom-0.5 size-5 text-border group-hover/box:text-input group-has-not-placeholder-shown/box:text-input group-has-focus-visible/box:text-ring"
        />
      </div>
    </section>
  );
}
