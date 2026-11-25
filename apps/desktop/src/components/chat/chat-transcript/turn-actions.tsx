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

import { Button } from '@kubetail/ui/elements/button';
import { Pencil, RefreshCw } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import ReactTimeAgo from 'react-timeago';

import { CopyButton } from '@/components/primitives/copy-button';
import { SMALL_ICON_STROKE } from '@/lib/control-style';

// Shown while the turn, the `group/turn` around it, is hovered or holds focus.
const ON_HOVER = 'opacity-0 group-focus-within/turn:opacity-100 group-hover/turn:opacity-100';

// Reload and Edit lead nowhere yet: the sidecar can neither answer a turn again
// nor change one.
function Unwired({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <Button type="button" variant="ghost" size="icon-xs" aria-label={label} disabled>
      <Icon aria-hidden="true" strokeWidth={SMALL_ICON_STROKE} />
    </Button>
  );
}

function TimeAgo({ at, className = '' }: { at: string; className?: string }) {
  return (
    <span className={`text-xs text-muted-foreground ${className}`}>
      <ReactTimeAgo date={at} />
    </span>
  );
}

// The row under a turn. Every turn reserves its 24px, so showing it moves nothing.
// A question's row is right-aligned under its bubble and shown on hover; an
// answer's is shown on hover, except the last answer's buttons, which are always
// shown. An answer still working keeps the row empty.
export function TurnActions({
  mine,
  text,
  createdAt,
  last = false,
  working = false,
}: {
  /** The user's question, rather than an answer. */
  mine: boolean;
  text: string;
  createdAt: string;
  last?: boolean;
  /** The answer's turn has not finished. */
  working?: boolean;
}) {
  if (mine) {
    return (
      <div className="-mr-0.5 h-6">
        <div className={`flex items-center gap-2 ${ON_HOVER}`}>
          <TimeAgo at={createdAt} />
          <div className="flex gap-0.5">
            <Unwired icon={RefreshCw} label="Reload" />
            <Unwired icon={Pencil} label="Edit" />
            <CopyButton text={text} label="Copy message" disabled={text === ''} />
          </div>
        </div>
      </div>
    );
  }
  if (working) return <div className="h-6" />;
  return (
    <div className="h-6 pl-0.5">
      <div className={`flex items-center gap-2 ${last ? '' : ON_HOVER}`}>
        <div className="flex gap-0.5">
          <CopyButton text={text} label="Copy answer" disabled={text === ''} />
          <Unwired icon={RefreshCw} label="Reload" />
        </div>
        <TimeAgo at={createdAt} className={last ? ON_HOVER : ''} />
      </div>
    </div>
  );
}
