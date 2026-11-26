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

// An answer's text as markdown, rendered to React elements. Nothing here is ever an
// HTML string: the text is the model's, and the model has read cluster data, so a
// tag in it renders as the literal characters. `react-markdown` leaves raw HTML out
// unless a rehype plugin puts it back — none is added.
import { toJsxRuntime } from 'hast-util-to-jsx-runtime';
import { Fragment, memo, useEffect, useMemo, useState } from 'react';
import type { ComponentProps, ReactNode } from 'react';
import ReactMarkdown, { type ExtraProps } from 'react-markdown';
import { jsx, jsxs } from 'react/jsx-runtime';
import remarkGfm from 'remark-gfm';

import './markdown.css';
import { CopyButton } from '@/components/primitives/copy-button';
import { VisibleText } from '@/components/primitives/visible-text';
import { highlight, load, loaded } from '@/lib/highlight';

type MarkdownProps = {
  text: string;
};

const remarkPlugins = [remarkGfm];

// A link is drawn but never followed: the webview has no window to open a page in,
// and the only URLs the host opens are ones it names itself.
function Link({ href, children }: ComponentProps<'a'>) {
  return (
    <span className="underline decoration-dotted" title={href}>
      {children}
    </span>
  );
}

// A fence's language, off the class react-markdown puts on its `<code>`.
function languageOf(className?: string): string | undefined {
  return className?.match(/(?:^|\s)language-(\S+)/)?.[1];
}

// `source` highlighted in `lang`, whose grammar must be loaded.
function highlighted(lang: string, source: string): ReactNode {
  // oxlint-disable-next-line typescript/no-unsafe-return -- tsgolint fails to resolve `toJsxRuntime`'s `JSX.Element`, which tsc reads
  return toJsxRuntime(highlight(lang, source), { Fragment, jsx, jsxs });
}

// Inline code and the code inside a fence share the tag; the fence's `<pre>` is
// what sets it apart, so the block styles ride there. A fence naming a language
// is highlighted once its grammar has arrived, plain until then; one naming none
// is never guessed at.
function Code({ className, children, ...p }: ComponentProps<'code'>) {
  const lang = languageOf(className);
  // The language whose grammar this fence's load brought in. The compiler caches
  // `loaded(lang)` on `lang`, so only this state shows a grammar arriving later.
  const [arrived, setArrived] = useState<string>();
  useEffect(() => {
    if (lang === undefined) return undefined;
    let live = true;
    void load(lang).then((ok) => {
      if (live && ok) setArrived(lang);
    });
    return () => {
      live = false;
    };
  }, [lang]);

  // Highlighted once per source: each frame of a streaming answer re-renders the
  // fences it has finished.
  const ready = lang !== undefined && (arrived === lang || loaded(lang));
  const body = useMemo(
    () => (ready && typeof children === 'string' ? highlighted(lang, children) : children),
    [lang, children, ready],
  );
  return (
    <code className={`rounded bg-muted px-1 py-0.5 font-mono text-xs ${className ?? ''}`} {...p}>
      {body}
    </code>
  );
}

type Element = NonNullable<ExtraProps['node']>;
type Text = Extract<Element['children'][number], { type: 'text' }>;

// A fence's source, read off its hast node. react-markdown always passes the node,
// and every fence is a `<pre>` over a `<code>` over one text node, an empty one too.
function fenceSource(pre: Element): string {
  const code = pre.children[0] as Element;
  return (code.children[0] as Text).value;
}

// A fence's language, off the class on its `<code>`.
function fenceLanguage(pre: Element): string | undefined {
  const classes = (pre.children[0] as Element).properties.className;
  return Array.isArray(classes) ? languageOf(classes.join(' ')) : undefined;
}

// A fence: its language over its code, and a copy button over the top right
// corner that hover or focus reveals, taking no room.
function CodeBlock({ node, children, ...p }: ComponentProps<'pre'> & ExtraProps) {
  const lang = fenceLanguage(node!);
  return (
    <div className="group/code relative my-2 flex flex-col gap-2 rounded-md border border-border bg-background p-3">
      {lang !== undefined && (
        <p className="text-xs leading-[normal] text-muted-foreground/70">
          <VisibleText text={lang} />
        </p>
      )}
      <pre className="overflow-x-auto [&>code]:bg-transparent [&>code]:p-0 [&>code]:text-[13px]/5" {...p}>
        {children}
      </pre>
      <CopyButton
        text={fenceSource(node!)}
        label="Copy code"
        className="absolute top-[3px] right-[3px] opacity-0 group-focus-within/code:opacity-100 group-hover/code:opacity-100"
      />
    </div>
  );
}

const components = {
  a: Link,
  p: (p: ComponentProps<'p'>) => <p className="my-2 first:mt-0 last:mb-0" {...p} />,
  h1: ({ children, ...p }: ComponentProps<'h1'>) => (
    <h1 className="mt-4 mb-2 text-base font-semibold first:mt-0" {...p}>
      {children}
    </h1>
  ),
  h2: ({ children, ...p }: ComponentProps<'h2'>) => (
    <h2 className="mt-4 mb-2 text-base font-semibold first:mt-0" {...p}>
      {children}
    </h2>
  ),
  h3: ({ children, ...p }: ComponentProps<'h3'>) => (
    <h3 className="mt-3 mb-1 font-semibold first:mt-0" {...p}>
      {children}
    </h3>
  ),
  h4: ({ children, ...p }: ComponentProps<'h4'>) => (
    <h4 className="mt-3 mb-1 font-semibold first:mt-0" {...p}>
      {children}
    </h4>
  ),
  h5: ({ children, ...p }: ComponentProps<'h5'>) => (
    <h5 className="mt-3 mb-1 font-semibold first:mt-0" {...p}>
      {children}
    </h5>
  ),
  h6: ({ children, ...p }: ComponentProps<'h6'>) => (
    <h6 className="mt-3 mb-1 font-semibold first:mt-0" {...p}>
      {children}
    </h6>
  ),
  ul: (p: ComponentProps<'ul'>) => <ul className="my-2 list-disc pl-5" {...p} />,
  ol: (p: ComponentProps<'ol'>) => <ol className="my-2 list-decimal pl-5" {...p} />,
  li: (p: ComponentProps<'li'>) => <li className="my-0.5" {...p} />,
  blockquote: (p: ComponentProps<'blockquote'>) => (
    <blockquote className="my-2 border-l-2 border-border pl-3 text-muted-foreground" {...p} />
  ),
  hr: (p: ComponentProps<'hr'>) => <hr className="my-3 border-border" {...p} />,
  code: Code,
  pre: CodeBlock,
  table: (p: ComponentProps<'table'>) => (
    <div className="my-2 overflow-x-auto">
      <table className="w-full border-collapse text-xs" {...p} />
    </div>
  ),
  th: (p: ComponentProps<'th'>) => <th className="border border-border px-2 py-1 text-left font-semibold" {...p} />,
  td: (p: ComponentProps<'td'>) => <td className="border border-border px-2 py-1" {...p} />,
};

function MarkdownText({ text }: MarkdownProps) {
  return (
    <ReactMarkdown remarkPlugins={remarkPlugins} components={components}>
      {text}
    </ReactMarkdown>
  );
}

// Memoized by hand, so a text is parsed once however often its row renders.
export const Markdown = memo(MarkdownText);
