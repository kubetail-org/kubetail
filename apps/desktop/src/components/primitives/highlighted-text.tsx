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

import { useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';

import '@/components/primitives/markdown/markdown.css';
import { VisibleText } from '@/components/primitives/visible-text';
import { highlight, load, loaded } from '@/lib/highlight';

type HastNode = ReturnType<typeof highlight>['children'][number];

// A highlighted tree as React elements, each run of text through VisibleText.
function hastToReact(nodes: HastNode[]): ReactNode[] {
  return nodes.map((node, i) => {
    if (node.type === 'text') {
      // oxlint-disable-next-line react/no-array-index-key -- a tree's children are cut afresh and never reorder
      return <VisibleText key={i} text={node.value} />;
    }
    /* v8 ignore next -- highlight.js emits text and spans alone */
    if (node.type !== 'element') return null;
    // highlight.js gives every span its classes as a list.
    const className = node.properties.className as string[];
    return (
      // oxlint-disable-next-line react/no-array-index-key -- a tree's children are cut afresh and never reorder
      <span key={i} className={className.join(' ')}>
        {hastToReact(node.children)}
      </span>
    );
  });
}

// Text that is not the app's own (the cluster's, the model's) in `lang`'s
// colours: plain until the grammar has arrived, highlighted after, and spelled
// through VisibleText either way.
export function HighlightedText({ lang, text }: { lang: string; text: string }) {
  const [ready, setReady] = useState(() => loaded(lang));
  useEffect(() => {
    if (ready) return undefined;
    let live = true;
    void load(lang).then((ok) => {
      if (live) setReady(ok);
    });
    return () => {
      live = false;
    };
  }, [lang, ready]);
  return useMemo(
    () => (ready ? hastToReact(highlight(lang, text).children) : <VisibleText text={text} />),
    [lang, ready, text],
  );
}
