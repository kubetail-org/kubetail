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

import { cn } from '@kubetail/ui/lib/utils';
import type { ComponentProps, KeyboardEvent } from 'react';

// oxlint-disable-next-line no-restricted-imports -- the one place the library's boxes are drawn
import { Input as UiInput } from '@kubetail/ui/elements/input';
// oxlint-disable-next-line no-restricted-imports -- the one place the library's boxes are drawn
import { SearchBox as UiSearchBox } from '@kubetail/ui/elements/search-box';
// oxlint-disable-next-line no-restricted-imports -- the one place the library's boxes are drawn
import { Textarea as UiTextarea } from '@kubetail/ui/elements/textarea';

import { isImeKey } from '@/lib/ime';

// Left alone, the webview suggests, corrects and capitalizes as the user types
// (WKWebView inline predictions most of all). That fights code-like text: a
// grep pattern, a path, a resource name. `writingsuggestions` is WebKit's
// switch for inline predictions; React passes it through as written.
export const CODE_TEXT = {
  autoComplete: 'off',
  autoCorrect: 'off',
  autoCapitalize: 'off',
  spellCheck: false,
  writingsuggestions: 'false',
} as const;

// Prose keeps the spellcheck underline, which changes nothing unasked.
export const PROSE_TEXT = { ...CODE_TEXT, spellCheck: true } as const;

type TextProps = { prose?: boolean };

// A text box marks focus by its border alone. The library adds a ring on
// `:focus-visible`, which a browser matches on a click into a text box too.
const NO_RING = 'focus-visible:ring-0';

export function Input({ prose = false, className, ...props }: ComponentProps<'input'> & TextProps) {
  return <UiInput {...(prose ? PROSE_TEXT : CODE_TEXT)} className={cn(NO_RING, className)} {...props} />;
}

type SearchBoxProps = ComponentProps<'input'> & {
  containerClassName?: string;
  /** Escape in a box holding text calls this, and goes no further: an overlay around the
   * box closes on the next. An IME's Escape is left to it. */
  onClear?: () => void;
};

// The library's search box, typed here: its own declaration file resolves its props to
// `any`. A search is code-like text.
export function SearchBox({ className, onClear, onKeyDown, ...props }: SearchBoxProps) {
  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    onKeyDown?.(e);
    if (!onClear || e.key !== 'Escape' || isImeKey(e.nativeEvent) || e.currentTarget.value === '') return;
    e.stopPropagation();
    onClear();
  };
  return <UiSearchBox {...CODE_TEXT} className={cn(NO_RING, className)} onKeyDown={handleKeyDown} {...props} />;
}

export function Textarea({ prose = false, className, ...props }: ComponentProps<'textarea'> & TextProps) {
  return <UiTextarea {...(prose ? PROSE_TEXT : CODE_TEXT)} className={cn(NO_RING, className)} {...props} />;
}
