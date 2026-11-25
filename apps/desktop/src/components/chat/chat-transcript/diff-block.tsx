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

// A change as the user reads it: a unified diff of YAML the sidecar computed
// from a dry run. Each line is its own element through VisibleText, so cluster
// text is never HTML; an added or removed line is colored, a hunk header muted.
// The fold is the caller's, since Approve waits on it.
import { Button } from '@kubetail/ui/elements/button';
import { useMemo } from 'react';

import '@/components/primitives/markdown/markdown.css';
import { VisibleText } from '@/components/primitives/visible-text';

/** The class of one diff line, by its first characters. */
function lineClass(line: string): string {
  if (line.startsWith('@@')) return 'text-muted-foreground';
  if (line.startsWith('+')) return 'diff-add';
  if (line.startsWith('-')) return 'diff-del';
  return '';
}

export function DiffBlock({
  head,
  rest,
  shown,
  onShow,
}: {
  head: string;
  rest: string;
  shown: boolean;
  onShow: () => void;
}) {
  // A fold can fall inside a line, so the lines are cut from what is drawn.
  const lines = useMemo(() => (shown ? head + rest : head).split(/(?<=\n)/), [head, rest, shown]);
  const [restChars, restLines] = useMemo(() => [[...rest].length, rest.split('\n').length - 1], [rest]);
  return (
    <>
      <pre className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
        {lines.map((line, i) => (
          // oxlint-disable-next-line react/no-array-index-key -- a line's place is its identity: the diff never reorders
          <span key={i} className={lineClass(line)}>
            <VisibleText text={line} />
          </span>
        ))}
      </pre>
      {rest !== '' && !shown && (
        <Button type="button" size="xs" variant="outline" className="mt-1" onClick={onShow}>
          Show the rest — {restChars} more characters{restLines > 0 && `, ${restLines} more lines`}
        </Button>
      )}
    </>
  );
}
