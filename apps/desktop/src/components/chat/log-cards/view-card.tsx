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
// A LogsView call in the transcript: one block naming what the view reads and
// where it opened, the model's description, the receipt it read, Expand, which
// makes this call the focused view, Open in window, and the view's lines in a fixed
// window. While the sidebar or a log window shows the call, the card says where in
// the buttons' place and draws no window (`buttons.tsx`). On the dashboard,
// which has no panel for a view yet, there are no buttons at all. The buttons and
// the window wait for the chat's cluster.
import { useState } from 'react';
import { useClient } from 'urql';

import { ModelDescription } from '@/components/chat/model-description';
import { SourceLine } from '@/components/logs/log-source-line';
import { VisibleText } from '@/components/primitives/visible-text';
import type { AppMode } from '@/lib/app-mode';
import type { ChatToolCall } from '@/lib/chats';
import { INLINE_LOADING, useInlineLogLines } from '@/lib/inline-log-lines';
import { createLogsClient } from '@/lib/logs-client';
import { anchorLine } from '@/lib/logs-view';
import type { LogsViewAction } from '@/lib/logs-view';
import { descriptionLine } from '@/lib/visible-text';

import { LogsViewButtons, useViewPlace } from './buttons';
import { LogsViewInline } from './inline';

// The window over the chat's cluster, its client built once per mount.
function InlineWindow({
  clusterID,
  action,
  until,
}: {
  clusterID: string;
  action: LogsViewAction;
  until: string | null;
}) {
  const urql = useClient();
  const [client] = useState(() => createLogsClient(urql, clusterID, action));
  const state = useInlineLogLines(client, action, until);
  return <LogsViewInline action={action} state={state} />;
}

export function LogsViewCard({
  call,
  action,
  chatID,
  clusterID,
  mode,
  until,
}: {
  call: ChatToolCall;
  action: LogsViewAction;
  chatID: string;
  /** The chat's cluster. Undefined until the list watch delivers the chat. */
  clusterID: string | undefined;
  mode: AppMode;
  /** The next view's `resolvedAt`, where this one stopped being live. Null while this is the chat's newest view, which alone may run live. */
  until: string | null;
}) {
  const place = useViewPlace(chatID, call.id, mode);
  const [first] = action.sources;
  const more = action.sources.length - 1;
  const line = descriptionLine(call.action?.description ?? '');
  return (
    <div className="mt-1 rounded-md border px-3 py-2">
      <div className="flex items-start gap-2">
        <div className="min-w-0 flex-1">
          <p className="font-semibold text-foreground">
            Logs: <SourceLine source={first} />
            {more > 0 && ` and ${more} more`}
          </p>
          <p>
            {anchorLine(action.anchor)}
            {action.pinToEnd && ', pinned to the end'}
            {action.grep !== '' && (
              <>
                {', matching '}
                <span className="font-mono">
                  /<VisibleText text={action.grep} />/
                </span>
              </>
            )}
          </p>
          {line !== '' && <ModelDescription line={line} />}
          {call.output !== '' && (
            <p className="wrap-break-word opacity-70">
              <VisibleText text={call.output} />
            </p>
          )}
        </div>
        {mode === 'chat' && (
          <LogsViewButtons
            label="Expand"
            place={place}
            chatID={chatID}
            callID={call.id}
            clusterID={clusterID}
            action={action}
          />
        )}
      </div>
      {place === 'card' &&
        (clusterID === undefined ? (
          <LogsViewInline action={action} state={INLINE_LOADING} />
        ) : (
          <InlineWindow clusterID={clusterID} action={action} until={until} />
        ))}
    </div>
  );
}
