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

// A log message with its colours. The sidecar keeps a line's SGR sequences and strips every
// other control (`services/logs/clean.go`). fancy-ansi turns the sequences into escaped HTML
// of flat styled spans; an inert document parses that, and its spans come back as runs, each
// drawn through `VisibleText`. No HTML reaches the page.
import { FancyAnsi } from 'fancy-ansi';
import { type CSSProperties, useMemo } from 'react';

import { VisibleText } from '@/components/primitives/visible-text';

const fancyAnsi = new FancyAnsi();

export type AnsiRun = { text: string; style?: CSSProperties };

// A span's inline style as React's style object. Every value is fancy-ansi's own, from its
// table or the sequence's digits, never from the text. `visibility` is dropped: SGR 8
// (conceal) may not hide cluster text.
function styleOf(span: HTMLElement): CSSProperties | undefined {
  const entries = Array.from(span.style)
    .filter((prop) => prop !== 'visibility')
    .map((prop): [string, string] => [
      prop.replaceAll(/-([a-z])/g, (_, c: string) => c.toUpperCase()),
      span.style.getPropertyValue(prop),
    ]);
  return entries.length ? Object.fromEntries(entries) : undefined;
}

/** A message as runs of text, each with the style its SGR sequences set. */
export function ansiRuns(message: string): AnsiRun[] {
  // Parsed bare, the document would drop the message's leading whitespace before its body.
  const doc = new DOMParser().parseFromString(`<div>${fancyAnsi.toHtml(message)}</div>`, 'text/html');
  return Array.from(doc.body.firstChild?.childNodes ?? [], (node) => ({
    text: node.textContent ?? '',
    style: node instanceof HTMLElement ? styleOf(node) : undefined,
  }));
}

export function AnsiText({ text }: { text: string }) {
  // A prepend re-renders every moved row; the parse is per message, not per render.
  const runs = useMemo(() => (text.includes('\x1b') ? ansiRuns(text) : null), [text]);
  if (!runs) return <VisibleText text={text} />;
  return runs.map((run, i) => (
    // oxlint-disable-next-line react/no-array-index-key -- a message's runs are cut afresh and never reorder
    <span key={i} style={run.style}>
      <VisibleText text={run.text} />
    </span>
  ));
}
