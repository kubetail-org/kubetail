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

// One chat's messages. Status is drawn from the row, text from the blocks: a
// streaming, failed or cancelled answer is drawn even with nothing in it, because a
// provider that refuses before its first chunk leaves an empty row whose error is
// all there is to show. An answer's work — its thinking summary and its settled
// tool calls — is a disclosure above its text, and the cluster card a question
// carries — what the app told the model about the cluster on that turn — is one
// above the question. The calls the user looks at stay under the text, and the one
// the turn waits on is the approval request. A background command's end reaches the
// model as a notice on a user message, drawn as a muted line rather than a bubble.
import { Button } from '@kubetail/ui/elements/button';
import { Spinner } from '@kubetail/ui/elements/spinner';
import { ChevronRight } from 'lucide-react';
import { Fragment, memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { useMutation } from 'urql';
import type { CombinedError } from 'urql';

import { LogsReadCard, LogsViewCard, LogsWatchCard } from '@/components/chat/log-cards';
import { ModelDescription } from '@/components/chat/model-description';
import { Elapsed } from '@/components/primitives/elapsed';
import { Markdown } from '@/components/primitives/markdown';
import { VisibleText } from '@/components/primitives/visible-text';
import { FolderGrantForm } from '@/components/settings/folder-grant-form';
import { graphql } from '@/gql';
import type { ApprovalDecision, ApprovalDuration } from '@/gql/graphql';
import type { AppMode } from '@/lib/app-mode';
import { chatGrantsContext } from '@/lib/chat-grants';
import { useChatOutbox } from '@/lib/chat-outbox';
import {
  actionKindLabel,
  approvalAnchor,
  grantOffered,
  inFlight,
  isWaitingAction,
  clusterRequestOf,
  cloudRequestOf,
  CLOUD_PLATFORM_NAMES,
  noticesOf,
  ranSearch,
  searchesOf,
  sourcesOf,
  textOf,
  waitingRequestsOf,
} from '@/lib/chats';
import type { ChatMessage, ChatSandboxAction, ChatToolCall, Source, TaskNotice, TaskNoticeOf } from '@/lib/chats';
import { CHATGPT_ID, useOpenChatGPTUsage } from '@/lib/connectors';
import { SMALL_ICON_STROKE } from '@/lib/control-style';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import { useHeldStill } from '@/lib/held-still';
import { humanDuration } from '@/lib/human-duration';
import { modelOf, useModels } from '@/lib/models';
import { cutText, descriptionLine } from '@/lib/visible-text';

import { DiffBlock } from './diff-block';
import { KubeQuery } from './kube-query';
import { TurnActions } from './turn-actions';

// How close to the end still counts as reading the end. Fixed, not a fraction of the
// viewport: a threshold that grows with the window yanks a reader back mid-paragraph.
const PIN_SLACK_PX = 32;

/**
 * How long Approve must keep its place on screen before it arms. Requests come
 * and go and answers grow while a request waits, so the page moves around it; a
 * click aimed at one thing must not land on an Approve that slid under the
 * pointer.
 */
export const APPROVE_ARM_MS = 500;

const ApprovalDecideMutation = graphql(`
  mutation ApprovalDecide($id: ApprovalID!, $decision: ApprovalDecision!) {
    approvalDecide(id: $id, decision: $decision)
  }
`);

const BackgroundTaskStopMutation = graphql(`
  mutation BackgroundTaskStop($id: ToolCallID!) {
    backgroundTaskStop(id: $id)
  }
`);

type ChatTranscriptProps = {
  messages: ChatMessage[];
  phase: SubscriptionPhase;
  /** The chat this draws, which is also the outbox entry Ask again sends from. */
  chatID: string;
  mode: AppMode;
  /** The chat's own cluster. Undefined while the watch that names it has not answered. */
  clusterID?: string;
  /** How long Approve holds its place before it arms; production's is APPROVE_ARM_MS. */
  approveArmMs?: number;
  /** Whether the machine offers a sandbox. Undefined until the sidecar says. */
  sandboxAvailable?: boolean;
  /** The chat's switch, which Ask again sends as what the user saw. Undefined until the list watch delivers the chat. */
  sandboxDisabled?: boolean;
  /** The chat's network switch, which Ask again sends the same way. */
  networkEnabled?: boolean;
  /** One of the chat's switches is in flight, which Ask again waits for. */
  switching?: boolean;
  /** Held at the bottom of the visible part as the messages scroll under it: the composer. */
  footer?: ReactNode;
};

// Chat mode's page pads the column from the window and centres the messages and the
// composer in the main panel, each at its own width; the dashboard's sidebar is narrow
// enough to fill. The footer's backdrop is the surface behind it, so messages
// scrolling under it stay hidden.
const COLUMN: Record<AppMode, string> = {
  chat: 'px-2 pt-6',
  dashboard: '',
};
const FOOTER: Record<AppMode, string> = {
  chat: 'mx-auto w-full max-w-162 bg-background pb-2',
  dashboard: 'bg-card',
};

// Every message with something to say about itself: its text, the notices it
// carried, or — for an answer its cap cut off while it was still reasoning — the
// reason it stopped.
function isDrawn(message: ChatMessage): boolean {
  if (message.status !== 'Complete') return true;
  return textOf(message.content) !== '' || message.finishReason !== '' || noticesOf(message.content).length > 0;
}

// A disclosure's summary row: no native marker, lifted from muted by hover or focus.
const SUMMARY =
  'flex list-none items-center select-none hover:text-foreground focus-visible:text-foreground [&::-webkit-details-marker]:hidden';

// A disclosure's caret, last in its summary: it points down while its `<details>` is open.
function Caret() {
  return (
    <ChevronRight
      aria-hidden="true"
      className="size-3 shrink-0 [details[open]>summary>&]:rotate-90"
      strokeWidth={SMALL_ICON_STROKE}
    />
  );
}

// An answer's work: its thinking summary and the calls it made, under a line
// counting how long the turn has worked (*Working 8s*) until it settles, then
// naming how long it did. Untouched, it is open while the answer is still all
// work — the pause it exists to fill — and closed once text arrives. A click
// pins the reader's choice for as long as the message stays mounted.
function Work({
  summary,
  calls,
  stillThinking,
  createdAt,
  finishedAt,
}: {
  summary: string;
  calls: ReactNode;
  stillThinking: boolean;
  createdAt: string;
  finishedAt: string | null;
}) {
  const [pinned, setPinned] = useState<boolean>();
  const open = pinned ?? stillThinking;
  return (
    <details open={open} className="mb-3">
      <summary
        className={`${SUMMARY} gap-1 border-b border-border/60 pb-2 text-xs text-muted-foreground/70`}
        onClick={(e) => {
          // The attribute is React's: the click sets the pin, never the DOM.
          e.preventDefault();
          setPinned(!open);
        }}
      >
        {finishedAt ? (
          `Worked for ${humanDuration(Date.parse(finishedAt) - Date.parse(createdAt))}`
        ) : (
          <span className="animate-shimmer text-shimmer motion-reduce:animate-none">
            Working <Elapsed since={Date.parse(createdAt)} />
          </span>
        )}
        <Caret />
      </summary>
      <div className="mt-2 flex flex-col gap-2">
        {summary !== '' && <Markdown text={summary} />}
        {calls}
      </div>
    </details>
  );
}

// The host a source's URL names, or '' for one that does not parse: the URL is
// web text, and a bad one draws its title alone.
function hostOf(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return '';
  }
}

// Where an answer drawn from the web came from. Always visible, never a
// disclosure; the title and the host are text and the whole URL rides as the
// item's title, since nothing in the webview opens one.
function Sources({ sources }: { sources: Source[] }) {
  return (
    <div className="mt-1 text-xs text-muted-foreground">
      <p>Sources</p>
      <ul className="list-disc pl-4">
        {sources.map((source) => {
          const host = hostOf(source.url);
          return (
            <li key={source.url} title={source.url}>
              {source.title}
              {host !== '' && <span className="opacity-70"> · {host}</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// What left the machine: how many searches ran and their queries, closed, as
// plain text — they are the model's words.
function Searches({ count, queries }: { count: number; queries: string[] }) {
  // With no query to show, a disclosure would open onto nothing.
  if (queries.length === 0) {
    return <p className="mt-1 text-xs text-muted-foreground">Searched the web · {count}</p>;
  }
  return (
    <details className="mt-1">
      <summary className="text-xs text-muted-foreground select-none">Searched the web · {count}</summary>
      <div className="mt-1 border-l-2 border-muted pl-3 text-xs whitespace-pre-wrap text-muted-foreground">
        {queries.join('\n')}
      </div>
    </details>
  );
}

// The tag beside a call's row, off its status alone. It speaks only when something
// went wrong or is still going: a failure reads "error", and the output says which.
function toolCallTag(call: ChatToolCall): string {
  if (call.background) return backgroundTag(call.background);
  switch (call.status) {
    case 'Failed':
      return 'error';
    case 'Denied':
      return 'denied';
    case 'NotRun':
      return 'not run';
    case 'Running':
      return 'running…';
    case 'Interrupted':
      return 'cancelled — may have run';
    case 'Succeeded':
    case 'AwaitingApproval':
    case null:
      return '';
  }
}

// The tag of a call that started a background task, a command, an agent or a
// watch: where the task is, off its own row, since the call itself answered at once.
function backgroundTag(task: NonNullable<ChatToolCall['background']>): string {
  switch (task.status) {
    case 'Running':
      return 'running in background';
    case 'Exited':
      return task.exitCode === 0 ? '' : 'error';
    case 'Failed':
      return 'error';
    case 'Expired':
      return 'expired';
    case 'Stopped':
      return 'stopped';
    case 'Lost':
      return 'lost';
    case 'Completed':
    case 'Matched':
      return '';
  }
}

// The user's stop of a running background command or agent. Only an error hands
// the button back: a false answer means nothing was running, and the watch
// brings its end.
function StopTask({ id }: { id: string }) {
  const [, backgroundTaskStop] = useMutation(BackgroundTaskStopMutation);
  const [pressed, setPressed] = useState(false);
  const press = async () => {
    setPressed(true);
    const result = await backgroundTaskStop({ id });
    if (result.error) setPressed(false);
  };
  return (
    <Button type="button" size="xs" variant="outline" className="mt-1" disabled={pressed} onClick={press}>
      Stop
    </Button>
  );
}

// Grant a folder… under a sandboxed command that failed, opening the grant
// form in place: the user reads the error in the call's output and types the
// folder, so nothing may float over the call. Granted holds until the pane
// unmounts; the grant never runs the command again.
function GrantOffer({ chatID }: { chatID: string }) {
  const [state, setState] = useState<'closed' | 'open' | 'granted'>('closed');
  if (state === 'granted') return <p className="mt-1 text-muted-foreground">Granted</p>;
  return (
    <>
      <Button
        type="button"
        size="xs"
        variant="link"
        className="px-0 text-muted-foreground"
        aria-expanded={state === 'open'}
        onClick={() => setState(state === 'open' ? 'closed' : 'open')}
      >
        Grant a folder…
      </Button>
      {state === 'open' && (
        <FolderGrantForm
          chatID={chatID}
          durations={['Chat', 'Always']}
          submitLabel="Grant"
          onGranted={() => setState('granted')}
          onCancel={() => setState('closed')}
        />
      )}
    </>
  );
}

// A call's kind, what the row leads with: the app's words, or for a call its tool
// could not show the tool's name, which is the model's.
function kindOf(call: ChatToolCall): string {
  if (call.action?.memory) return MEMORY_OPS[call.action.memory.op] ?? 'Memory';
  if (call.actionKind) return actionKindLabel(call.actionKind);
  return call.name;
}

// The one reader of what a call acts on: the command it runs, the path it reads
// or writes, the query it searched, the page or the memory; '' for none. The
// sidecar reads the action off the arguments, so nothing here parses them.
function targetOf(call: ChatToolCall): string {
  const write = writeOf(call.action);
  if (call.action?.command) return call.action.command.text;
  if (call.action?.read) return call.action.read.path;
  if (write) return write.path;
  if (call.action?.edit) return call.action.edit.path;
  if (call.action?.search) return call.action.search.query;
  if (call.action?.fetch) return call.action.fetch.url;
  if (call.action?.memory) return call.action.memory.name;
  return '';
}

// A memory call's kind, by its op.
const MEMORY_OPS: Record<string, string> = { save: 'Save memory', forget: 'Forget memory' };

// What a save kept, on the page where it was kept, as text, since the model may
// have written it from cluster data.
function MemoryKept({ body }: { body: string }) {
  return (
    <pre className="mt-1 border-l-2 border-muted pl-3 font-mono break-all whitespace-pre-wrap">
      <VisibleText text={body} />
    </pre>
  );
}

// The one reader of an action's write. It destructures because the HTML-sink
// lint refuses any `.write`, which is how document.write is spelled.
function writeOf(action: ChatToolCall['action']) {
  const { write } = action ?? { write: null };
  return write;
}

// How much of a command, a write's content or a memory's body the request draws
// whole. Past either, the rest is folded and Approve waits on it being shown:
// what the user has not seen is not approved.
const COMMAND_CHARS = 2000;
const COMMAND_LINES = 24;

// How the request draws a command or a write's content.
const REQUEST_TEXT = 'mt-1 font-mono text-xs break-all whitespace-pre-wrap';

// A text cutText split, spelled: the head, the rest once shown, and while it is
// not, a button saying how much is held back. The counts are memoized, since a
// write's rest can run to megabytes and the transcript re-renders per frame.
function FoldedText({
  head,
  rest,
  shown,
  onShow,
  className,
  file = false,
}: {
  head: string;
  rest: string;
  shown: boolean;
  onShow: () => void;
  className: string;
  /** A file's content, whose whitespace at a line's end is spelled too. */
  file?: boolean;
}) {
  const [restChars, restLines] = useMemo(() => [[...rest].length, rest.split('\n').length - 1], [rest]);
  let headTrailing: 'lines' | 'end' | undefined;
  let restTrailing: 'lines' | 'end' | undefined;
  if (file) {
    // The head ends a line where the fold falls on a newline, or where it is all.
    headTrailing = rest === '' || rest.startsWith('\n') ? 'end' : 'lines';
    restTrailing = 'end';
  }
  return (
    <>
      <pre className={className}>
        <VisibleText text={head} trailing={headTrailing} />
        {shown && <VisibleText text={rest} trailing={restTrailing} />}
      </pre>
      {rest !== '' && !shown && (
        <Button type="button" size="xs" variant="outline" className="mt-1" onClick={onShow}>
          Show the rest — {restChars} more characters{restLines > 0 && `, ${restLines} more lines`}
        </Button>
      )}
    </>
  );
}

// text as cutText folds it for a request, once per text: a write's body runs to
// 1 MiB and the transcript re-renders on every frame.
function useCut(text: string) {
  return useMemo(() => cutText(text, COMMAND_CHARS, COMMAND_LINES), [text]);
}

// A settled call's long text, folded as on its request: a write's content, an
// Agent call's brief, or a query's SQL.
function FoldedBlock({ text, className, file = false }: { text: string; className: string; file?: boolean }) {
  const [shown, setShown] = useState(false);
  const { head, rest } = useCut(text);
  return (
    <FoldedText head={head} rest={rest} shown={shown} onShow={() => setShown(true)} className={className} file={file} />
  );
}

type EditAction = NonNullable<NonNullable<ChatToolCall['action']>['edit']>;

// The strings an edit replaced, drawn as on its request.
function EditStrings({ edit }: { edit: EditAction }) {
  const [shown, setShown] = useState({ old: false, new: false });
  return (
    <EditBlocks
      edit={edit}
      shown={shown}
      onShow={(which) => setShown((prev) => ({ ...prev, [which]: true }))}
      className="mt-1 border-l-2 border-muted pl-3 font-mono break-all whitespace-pre-wrap"
    />
  );
}

type EditShown = { old: boolean; new: boolean };

const folds = (text: string) => cutText(text, COMMAND_CHARS, COMMAND_LINES).rest !== '';

// Whether either of an edit's strings is still folded.
function editFolded(edit: EditAction, shown: EditShown): boolean {
  return (folds(edit.oldString) && !shown.old) || (folds(edit.newString) && !shown.new);
}

// An edit's two strings: the text that goes over the text that replaces it,
// each drawn as a file's content is, since each is a piece of one, and folded
// on its own.
function EditBlocks({
  edit,
  shown,
  onShow,
  className,
}: {
  edit: EditAction;
  shown: EditShown;
  onShow: (which: keyof EditShown) => void;
  className: string;
}) {
  const old = cutText(edit.oldString, COMMAND_CHARS, COMMAND_LINES);
  const replacement = cutText(edit.newString, COMMAND_CHARS, COMMAND_LINES);
  return (
    <>
      {edit.replaceAll && <p className="mt-1 text-xs text-muted-foreground">Every occurrence in the file.</p>}
      <p className="mt-1 text-xs text-muted-foreground">Replace</p>
      <FoldedText
        head={old.head}
        rest={old.rest}
        shown={shown.old}
        onShow={() => onShow('old')}
        className={className}
        file
      />
      <p className="mt-1 text-xs text-muted-foreground">{edgesOf(edit.oldString)}</p>
      <p className="mt-1 text-xs text-muted-foreground">With</p>
      {edit.newString === '' ? (
        <p className="mt-1 text-xs text-muted-foreground">The text will be deleted.</p>
      ) : (
        <>
          <FoldedText
            head={replacement.head}
            rest={replacement.rest}
            shown={shown.new}
            onShow={() => onShow('new')}
            className={className}
            file
          />
          <p className="mt-1 text-xs text-muted-foreground">{edgesOf(edit.newString)}</p>
        </>
      )}
      {edit.oldString.replaceAll(/\s/g, '') === edit.newString.replaceAll(/\s/g, '') && (
        <p className="mt-1 text-xs text-muted-foreground">The two differ only in whitespace.</p>
      )}
    </>
  );
}

// How a piece of a file meets what is around it: a newline at either end
// decides whether an edit joins or splits lines, and a <pre> draws neither.
// A CRLF file's strings can carry \r\n, whose \r would hide a leading \n.
function edgesOf(text: string): string {
  const starts = text.startsWith('\n') || text.startsWith('\r\n');
  const ends = text.endsWith('\n');
  if (starts && ends) return 'Starts and ends with a newline.';
  if (starts) return 'Starts with a newline.';
  if (ends) return 'Ends with a newline.';
  return 'No newline at either end.';
}

// A write that ran unasked is one in the chat's workspace or a folder granted
// read-write: what it wrote is drawn open, since a later command may run it.
function drawnOpen(call: ChatToolCall): boolean {
  return (
    call.approval === null &&
    call.status === 'Succeeded' &&
    (writeOf(call.action) !== null || call.action?.edit != null)
  );
}

// Whether a call is drawn under the answer rather than folded into its work: a
// log card the user looks at, a task the user may stop, an unasked change the
// user should see, and a failure that offers a grant.
function standsOut(call: ChatToolCall): boolean {
  const a = call.action;
  return (
    a?.logsView != null ||
    a?.logsRead != null ||
    a?.logsWatch != null ||
    call.background?.status === 'Running' ||
    drawnOpen(call) ||
    grantOffered(call)
  );
}

// The calls that are not on the user: one closed disclosure each. The row is the
// call's kind, then the model's description, or with none what the call acts on;
// the body is what it ran and what the model read, all text. The row is spelled
// like the approval request, so an override in it cannot reorder the tag, and
// carries no title, since a native tooltip would draw it raw. A call its tool
// could not show reads as its tool's name.
function ToolCalls({
  calls,
  byAgent,
  modelLabel,
  chatID,
  clusterID,
  mode,
  ends,
}: {
  calls: ChatToolCall[];
  byAgent: SubagentCalls;
  modelLabel: ModelLabel;
  chatID: string;
  clusterID: string | undefined;
  mode: AppMode;
  ends: ViewEnds;
}) {
  return (
    <div className="flex flex-col gap-1.5 text-xs text-muted-foreground">
      {calls.map((call) => {
        // A view that opened is a card, not a disclosure: the user looks at it.
        if (call.action?.logsView) {
          return (
            <LogsViewCard
              key={call.id}
              call={call}
              action={call.action.logsView}
              chatID={chatID}
              clusterID={clusterID}
              mode={mode}
              until={ends.get(call.id) ?? null}
            />
          );
        }
        // So is a read: the user sees the lines the model read.
        if (call.action?.logsRead) {
          return (
            <LogsReadCard
              key={call.id}
              call={call}
              action={call.action.logsRead}
              chatID={chatID}
              clusterID={clusterID}
              mode={mode}
            />
          );
        }
        // And a watch: the user sees what it waits for, then what it held.
        if (call.action?.logsWatch) {
          return (
            <Fragment key={call.id}>
              <LogsWatchCard
                call={call}
                action={call.action.logsWatch}
                task={call.background}
                chatID={chatID}
                clusterID={clusterID}
                mode={mode}
              />
              {call.background?.status === 'Running' && <StopTask id={call.id} />}
            </Fragment>
          );
        }
        const kind = kindOf(call);
        const target = targetOf(call);
        const line = descriptionLine(call.action?.description ?? '');
        const command = call.action?.command ?? null;
        let where = '';
        if (command?.sandboxed) where = call.network ? ' · sandboxed, with network' : ' · sandboxed';
        const written = writeOf(call.action);
        const edit = call.action?.edit ?? null;
        const delegate = call.action?.delegate ?? null;
        const tag = toolCallTag(call);
        const open = drawnOpen(call);
        const changed = (
          <>
            {written && (
              <FoldedBlock
                text={written.content}
                className="border-l-2 border-muted pl-3 font-mono break-all whitespace-pre-wrap"
                file
              />
            )}
            {edit && <EditStrings edit={edit} />}
          </>
        );
        // The row draws the description in the target's place, so the body draws the target.
        const targetInBody = line !== '' && target !== '';
        return (
          <Fragment key={call.id}>
            <details>
              <summary className={`${SUMMARY} gap-2`}>
                <span className="shrink-0 font-medium">
                  <VisibleText text={kind} />
                </span>
                {line !== '' ? (
                  <span className="max-w-110 min-w-0">
                    <ModelDescription line={line} quoted={false} />
                  </span>
                ) : (
                  target !== '' && (
                    <span className="min-w-0 font-mono break-all">
                      <VisibleText text={target} />
                    </span>
                  )
                )}
                {call.action?.memory?.scope === 'everywhere' && (
                  <span className="shrink-0 opacity-70">for every cluster</span>
                )}
                {tag !== '' && (
                  <span className={tag === 'error' ? 'shrink-0 text-destructive' : 'shrink-0 opacity-70'}>{tag}</span>
                )}
                <Caret />
              </summary>
              <div className="flex flex-col gap-1.5 pt-0.5 pb-2">
                {!open && changed}
                {call.action?.kubeQuery && <KubeQuery sql={call.action.kubeQuery.sql} output={call.output} />}
                {call.action?.memory?.op === 'save' && call.status === 'Succeeded' && (
                  <MemoryKept body={call.action.memory.body} />
                )}
                {delegate ? (
                  <AgentBody
                    delegate={delegate}
                    calls={byAgent.get(call.id) ?? NO_CALLS}
                    report={call.background?.report ?? ''}
                    modelLabel={modelLabel}
                    chatID={chatID}
                    clusterID={clusterID}
                    mode={mode}
                    ends={ends}
                  />
                ) : (
                  !call.action?.kubeQuery &&
                  (targetInBody || call.output !== '') && (
                    <div className="flex flex-col gap-1.5 rounded-md bg-muted/60 px-3 py-2.5 font-mono leading-4.5">
                      {targetInBody && (
                        <p className="break-all text-foreground">
                          {command && '$ '}
                          <VisibleText text={target} />
                        </p>
                      )}
                      {call.output !== '' && <pre className="break-all whitespace-pre-wrap">{call.output}</pre>}
                    </div>
                  )
                )}
                {command && command.cwd !== '' && (
                  <p className="text-muted-foreground/70">
                    Ran in <VisibleText text={command.cwd} />
                    {where}
                  </p>
                )}
                <SandboxActionLines call={call} />
              </div>
            </details>
            {open && changed}
            {call.background?.status === 'Running' && <StopTask id={call.id} />}
            {grantOffered(call) && <GrantOffer chatID={chatID} />}
          </Fragment>
        );
      })}
    </div>
  );
}

// A call's sandbox actions that no longer wait, one line each: the method and
// the path (a cloud platform's after its host), tagged with what the user or
// the permissions engine decided, a cloud platform's request with its name
// first, and for the engine's, the mode or rule that decided it. `approved` and
// `allowed` are the decision, not that the cluster or cloud platform received
// it; the output says what the command read back.
function SandboxActionLines({ call }: { call: ChatToolCall }) {
  const settled = call.sandboxActions.filter((a) => !isWaitingAction(call, a));
  if (settled.length === 0) return null;
  return (
    <ul className="border-l-2 border-muted pl-3">
      {settled.map((a) => {
        const cluster = clusterRequestOf(a);
        const cloud = cloudRequestOf(a);
        const target = a.action?.target ?? 'Kubernetes';
        let line = a.action?.summary ?? '';
        if (cluster) line = `${cluster.method} ${cluster.path}`;
        if (cloud) line = `${cloud.method} ${cloud.host}${cloud.path}`;
        const tag =
          target === 'Kubernetes' ? sandboxActionTag(a) : `${CLOUD_PLATFORM_NAMES[target]} · ${sandboxActionTag(a)}`;
        return (
          <li key={a.approval.id}>
            <span className="font-mono break-all">
              <VisibleText text={line} />
            </span>
            {cluster?.dryRun && ' (dry run)'}
            <span className="ml-2 opacity-70">{tag}</span>
            {a.reason && (
              <span className="ml-2 text-muted-foreground">
                <VisibleText text={a.reason} />
              </span>
            )}
          </li>
        );
      })}
    </ul>
  );
}

// Why a decision was refused: the sidecar's reason when it refused it, as a
// rule an answer would write that Kubetail cannot apply, else that it never
// arrived.
function refusedDecision(error: CombinedError): string {
  const first = error.graphQLErrors[0];
  const code = first?.extensions?.code;
  if (code === 'KUBETAIL_VALIDATION_ERROR' || code === 'KUBETAIL_RECORD_NOT_FOUND') return first.message;
  return 'The decision did not reach the sidecar. Try again.';
}

// One allow answer and the rule it adds under it, in the words Settings uses.
function AllowAnswer({
  label,
  rule,
  disabled,
  onClick,
}: {
  label: string;
  rule: string;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <div className="flex max-w-56 flex-col gap-0.5">
      <Button type="button" size="xs" variant="outline" disabled={disabled} onClick={onClick}>
        {label}
      </Button>
      <span className="text-xs wrap-break-word text-muted-foreground">
        <VisibleText text={rule} />
      </span>
    </div>
  );
}

// How long each approval holds, in the tag's words.
const DURATION_WORDS: Record<ApprovalDuration, string> = {
  Once: 'once',
  Command: 'this command',
  Chat: 'this chat',
  Always: 'always',
};

// What became of an action that no longer waits. A pending one here is one a
// crash stranded, as an abandoned one's wait ended with nobody's answer.
/**
 * A sandbox action's heading: its target and whether it reads data or changes
 * something. An action this build cannot read names neither.
 */
function sandboxActionLabel(action: ChatSandboxAction['action']): string {
  if (action === null) return 'Request awaiting approval';
  const reads = action.accessLevel === 'DataRead';
  if (action.target === 'Kubernetes')
    return reads ? 'Secret read awaiting approval' : 'Cluster change awaiting approval';
  const platform = CLOUD_PLATFORM_NAMES[action.target];
  return reads ? `${platform} data read awaiting approval` : `${platform} change awaiting approval`;
}

function sandboxActionTag(a: ChatSandboxAction): string {
  switch (a.approval.status) {
    case 'Approved':
      return a.approval.duration ? `approved · ${DURATION_WORDS[a.approval.duration]}` : 'approved';
    case 'Denied':
      return 'denied';
    case 'Allowed':
      return 'allowed';
    case 'Refused':
      return 'refused';
    case 'Pending':
    case 'Abandoned':
      return 'not answered';
  }
}

// A model id as the reader is shown it: the catalog's label, or the id itself.
type ModelLabel = (id: string) => string;

// A subagent's calls not awaiting approval, by the Agent call they ran under.
type SubagentCalls = ReadonlyMap<string, ChatToolCall[]>;

const NO_CALLS: ChatToolCall[] = [];
const NO_SUBAGENT_CALLS: SubagentCalls = new Map();

/** Where each opened view ends, by call id: the next view's `resolvedAt`, null for the chat's newest. */
export type ViewEnds = ReadonlyMap<string, string | null>;

/**
 * The views the chat's calls opened, in transcript order. Only the newest may run live; each
 * older one stopped being live when the next one resolved. A refused call opened none.
 */
export function viewEnds(messages: ChatMessage[]): ViewEnds {
  const opened = messages.flatMap((message) => message.toolCalls.filter((call) => call.action?.logsView));
  return new Map(opened.map((call, i) => [call.id, opened[i + 1]?.action?.logsView?.resolvedAt ?? null]));
}

// `viewEnds`, as the same map for as long as its entries hold, so a frame of a
// streaming answer hands every row the `ends` it already has.
function useViewEnds(messages: ChatMessage[]): ViewEnds {
  const next = viewEnds(messages);
  const [ends, setEnds] = useState(next);
  if (next.size === ends.size && [...next].every(([id, end]) => ends.get(id) === end)) return ends;
  setEnds(next);
  return next;
}

// An answer's calls split into its own and its subagents', which are drawn
// inside their Agent call. A subagent's call awaiting approval is in neither: it
// is the request.
function splitCalls(calls: ChatToolCall[]): { own: ChatToolCall[]; byAgent: SubagentCalls } {
  const own: ChatToolCall[] = [];
  const byAgent = new Map<string, ChatToolCall[]>();
  calls.forEach((call) => {
    if (call.agentCallID === null) {
      own.push(call);
    } else if (call.status !== 'AwaitingApproval') {
      byAgent.set(call.agentCallID, [...(byAgent.get(call.agentCallID) ?? []), call]);
    }
  });
  return { own, byAgent };
}

type DelegateAction = NonNullable<NonNullable<ChatToolCall['action']>['delegate']>;

// What an Agent call handed on and got back: the kind of agent and, when the
// call named one, the model it ran on; the brief, folded like a command; the
// subagent's calls, drawn as the answer's own are; and its report, off the task
// once the agent completed. The brief and the report are the models' text, drawn
// as text and markdown.
function AgentBody({
  delegate,
  calls,
  report,
  modelLabel,
  chatID,
  clusterID,
  mode,
  ends,
}: {
  delegate: DelegateAction;
  calls: ChatToolCall[];
  report: string;
  modelLabel: ModelLabel;
  chatID: string;
  clusterID: string | undefined;
  mode: AppMode;
  ends: ViewEnds;
}) {
  const kind = delegate.model === '' ? delegate.agentType : `${delegate.agentType} · ${modelLabel(delegate.model)}`;
  return (
    <>
      <p className="mt-1">
        <VisibleText text={kind} />
      </p>
      <FoldedBlock text={delegate.prompt} className="mt-1 border-l-2 border-muted pl-3 break-all whitespace-pre-wrap" />
      {calls.length > 0 && (
        <ToolCalls
          calls={calls}
          byAgent={NO_SUBAGENT_CALLS}
          modelLabel={modelLabel}
          chatID={chatID}
          clusterID={clusterID}
          mode={mode}
          ends={ends}
        />
      )}
      {report !== '' && (
        <div className="mt-1 border-l-2 border-muted pl-3 text-foreground">
          <Markdown text={report} />
        </div>
      )}
    </>
  );
}

// A value the sidecar bounds, as one line after a muted label: where a command
// runs, or the host a fetch dials. Never folded or cut, and spelled like the
// command, since the model chose it.
// A muted label, then text in mono through VisibleText, then an optional muted
// note that is the app's own, outside the text.
function LabelledLine({ label, text, after }: { label: string; text: string; after?: string }) {
  return (
    <p className="mt-1 flex min-w-0 gap-1 text-xs">
      <span className="shrink-0 text-muted-foreground">
        <VisibleText text={label} />
      </span>
      <span className="min-w-0 font-mono break-all">
        <VisibleText text={text} />
      </span>
      {after && <span className="-ml-1 shrink-0 text-muted-foreground">{after}</span>}
    </p>
  );
}

// A variable as the command gets it, NAME=value: the value's whitespace kept,
// and what ends a line or opens or closes it spelled, since a newline in
// GIT_SSH_COMMAND separates commands.
function VariableLine({ entry }: { entry: string }) {
  const at = entry.indexOf('=') + 1;
  return (
    <p className="mt-1 flex min-w-0 gap-1 text-xs">
      <span className="shrink-0 text-muted-foreground">with</span>
      <span className="min-w-0 font-mono break-all whitespace-pre-wrap">
        <VisibleText text={entry.slice(0, at)} />
        <VisibleText text={entry.slice(at)} trailing="edges" />
      </span>
    </p>
  );
}

type CommandAction = NonNullable<NonNullable<ChatToolCall['action']>['command']>;

// What a command's request asks, off the call, never the chat's switch now: the
// turn read it when it started. A sandboxed command asks only for the internet.
// On a machine with a sandbox every other command that asks runs outside it;
// unknown reads as outside, which a machine with no sandbox is too.
function commandHeading(command: CommandAction, sandboxAvailable: boolean | undefined): string {
  if (command.sandboxed) {
    return command.background
      ? 'Run this command in the background, with network access?'
      : 'Run this command with network access?';
  }
  if (sandboxAvailable !== false) {
    return command.background
      ? 'Run this command in the background, outside the sandbox?'
      : 'Run this command outside the sandbox?';
  }
  return command.background ? 'Run this command in the background?' : 'Run this command?';
}

// The call a turn is stopped on, and the only place it is approved: a command,
// a file read by its path, a file written by its path and content, a file
// edited by its path and both strings, or a page fetched by its URL. It shows
// the id the row carries and sends it back with the user's decision; the
// message changing is what takes the request down. The command and the content wrap
// anywhere and nothing caps their height, so the head reaches the eye whole
// however narrow the panel.
// `live` is a pending approval on a turn still waiting: anything else is a
// choice that no longer exists. A request with no kind to draw offers no
// Approve: what the user cannot see, they cannot approve. `agent` is the
// description of the Agent call a subagent's request ran under, null on the
// answer's own: the user is approving a call whose reasoning they cannot see,
// so the request says whose it is. `change` is an action the call's sandboxed
// command sent to the cluster or a cloud platform, which is what the request
// asks about when it is set; the call's command is then drawn under it as what
// sent it. A cluster change is the one request that offers a rule: an action a
// rule may allow offers four allow answers beside Deny, each with the rule it
// adds in the sidecar's words. A cloud platform's never may.
function ApprovalRequest({
  approval,
  action,
  change,
  live,
  agent,
  armMs,
  sandboxAvailable,
}: {
  approval: NonNullable<ChatToolCall['approval']>;
  action: ChatToolCall['action'];
  change: ChatSandboxAction | null;
  live: boolean;
  agent: string | null;
  armMs: number;
  sandboxAvailable: boolean | undefined;
}) {
  const [, approvalDecide] = useMutation(ApprovalDecideMutation);
  const buttons = useRef<HTMLDivElement>(null);
  const armed = useHeldStill(buttons, armMs);
  const [decided, setDecided] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const [shown, setShown] = useState(false);
  const [diffShown, setDiffShown] = useState(false);
  const [editShown, setEditShown] = useState({ old: false, new: false });
  const command = action?.command ?? null;
  const read = action?.read ?? null;
  const write = writeOf(action);
  const edit = action?.edit ?? null;
  const page = action?.fetch ?? null;
  // A memory call for one cluster never asks, so it is no kind this draws.
  const memory = action?.memory?.scope === 'everywhere' ? action.memory : null;
  const cluster = change ? clusterRequestOf(change) : null;
  const cloud = change ? cloudRequestOf(change) : null;
  // A cloud platform's body under an Unshown sentence is never drawn, so it
  // never folds and Approve never waits on it.
  const requestBody = cluster?.body ?? (cloud?.unshown === '' ? cloud.body : '');
  const folding = change ? requestBody : (command?.text ?? write?.content ?? memory?.body ?? '');
  const { head, rest } = useCut(folding);
  const diff = useCut(cluster?.diff ?? '');
  const line = descriptionLine(action?.description ?? '');
  // A whole diff is what a change's user reads, so the request behind it is a
  // fold Approve does not wait on; with no diff, or one cut short, the request
  // is drawn open and Approve waits on its body.
  const requestOpen = cluster === null || cluster.diff === '' || cluster.diffCut;
  const folded =
    (rest !== '' && !shown && requestOpen) ||
    (diff.rest !== '' && !diffShown) ||
    (edit !== null && editFolded(edit, editShown));

  // Only an error hands the buttons back. A false answer means no turn was
  // waiting — a cancel or a settle is on its way through the watch — so they
  // stay down. An answer that writes a rule names the rules' type, so the
  // chat's list and Settings ask again.
  const decide = async (decision: ApprovalDecision) => {
    setDecided(true);
    setFailure(null);
    const variables = { id: approval.id, decision };
    const writesRule = decision === 'Chat' || decision === 'Always';
    const result = writesRule ? await approvalDecide(variables, chatGrantsContext) : await approvalDecide(variables);
    if (result.error) {
      setDecided(false);
      setFailure(refusedDecision(result.error));
    }
  };

  const approvable = live && !decided && !folded && armed;
  let label = 'Command awaiting approval';
  // Only a request drawn below offers Approve: what the user cannot see is not
  // approved.
  let drawn = true;
  let body: ReactNode;
  // The request as sent, drawn alike for the cluster and a cloud platform: the
  // path and query as sent, one line the proxy bounds; the method, and the
  // media type, which decides what a body does; then the body as a file's
  // content is, or the cloud platform's sentence for one it does not show. The
  // command under Sent by is context, so Approve does not wait on its fold.
  const request = cluster ?? cloud;
  const unshown = cloud?.unshown ?? '';
  const sent = request && (
    <>
      <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
        <VisibleText text={request.path} />
      </p>
      <p aria-label="Method and media type" className="mt-1 font-mono text-xs break-all">
        {request.method}
        {request.contentType !== '' && (
          <>
            {' '}
            <VisibleText text={request.contentType} />
          </>
        )}
      </p>
      {unshown !== '' ? (
        <p className="mt-1 text-xs text-muted-foreground">
          <VisibleText text={unshown} />
        </p>
      ) : (
        request.body !== '' && (
          <FoldedText
            head={head}
            rest={rest}
            shown={shown}
            onShow={() => setShown(true)}
            className={REQUEST_TEXT}
            file
          />
        )
      )}
    </>
  );
  const sentBy = command && (
    <>
      <p className="mt-1 text-xs text-muted-foreground">Sent by</p>
      <FoldedBlock text={command.text} className={REQUEST_TEXT} />
    </>
  );
  if (change && (change.request === null || change.action === null)) {
    // An action with no request of its own is a kind a later step draws; one
    // with no action is a request this build cannot read. This branch comes
    // first, so neither offers Approve.
    label = sandboxActionLabel(change.action);
    drawn = false;
    body = <p className="text-xs text-muted-foreground">This request can&apos;t be shown.</p>;
  } else if (change?.action && cloud) {
    // A cloud platform's request: the summary as its heading, the host it goes
    // to, each line the classifier read, then the raw request open, as it is
    // the whole of what is approved. Every string is the command's text.
    label = sandboxActionLabel(change.action);
    body = (
      <>
        <p className="text-xs text-muted-foreground">
          <VisibleText text={change.action.summary} />
        </p>
        <LabelledLine label="Host:" text={cloud.host} />
        {cloud.details.map((d, i) => (
          // oxlint-disable-next-line react/no-array-index-key -- details are a fixed list, and two may share a label
          <LabelledLine key={i} label={d.label} text={d.value} />
        ))}
        {sent}
        {sentBy}
      </>
    );
  } else if (change?.action && cluster) {
    // The action is the heading, the sidecar's summary, so nothing here parses
    // a path; the diff a dry run computed is what changes. The request itself
    // is never a reading of it. A read of Secret data is drawn the same way: a
    // GET with no body and no diff.
    label = sandboxActionLabel(change.action);
    body = (
      <>
        <p className="text-xs text-muted-foreground">
          <VisibleText text={change.action.summary} />
          {cluster.dryRun && ' (dry run)'}
        </p>
        {cluster.diff !== '' && (
          <DiffBlock head={diff.head} rest={diff.rest} shown={diffShown} onShow={() => setDiffShown(true)} />
        )}
        {cluster.diffError !== '' && (
          <p className="mt-1 text-xs text-muted-foreground">
            No preview: <VisibleText text={cluster.diffError} />
          </p>
        )}
        {requestOpen ? (
          sent
        ) : (
          <details className="mt-1">
            <summary className="text-xs text-muted-foreground">Show the request</summary>
            {sent}
          </details>
        )}
        {sentBy}
      </>
    );
  } else if (command) {
    body = (
      <>
        <p className="text-xs text-muted-foreground">{commandHeading(command, sandboxAvailable)}</p>
        {command.background && (
          <p className="text-xs text-muted-foreground">
            It keeps running after this answer, until it exits or you stop it.
          </p>
        )}
        {line !== '' && (
          <p className="mt-1 flex min-w-0 gap-1 text-xs">
            <span className="shrink-0 text-muted-foreground">The model says:</span>
            <ModelDescription line={line} />
          </p>
        )}
        <FoldedText head={head} rest={rest} shown={shown} onShow={() => setShown(true)} className={REQUEST_TEXT} />
        {command.cwd !== '' && <LabelledLine label="in" text={command.cwd} />}
        {/* Every variable, none folded: the request is the gate. */}
        {command.env.map((entry) => (
          <VariableLine key={entry} entry={entry} />
        ))}
      </>
    );
  } else if (write) {
    // The path is one line, as a read's is; the content is every byte the file
    // will hold, so each \r, each space or tab ending a line, and whether the
    // last line ends with a newline are drawn, none of which the eye sees in a
    // <pre>.
    label = 'File write awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Write this file?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={write.path} />
        </p>
        {write.content === '' ? (
          <p className="mt-1 text-xs text-muted-foreground">The file will be empty.</p>
        ) : (
          <>
            <FoldedText
              head={head}
              rest={rest}
              shown={shown}
              onShow={() => setShown(true)}
              className={REQUEST_TEXT}
              file
            />
            <p className="mt-1 text-xs text-muted-foreground">
              {write.content.endsWith('\n') ? 'Ends with a newline.' : 'No newline at the end.'}
            </p>
          </>
        )}
      </>
    );
  } else if (edit) {
    // The path is drawn as a write's; the two strings are pieces of the file,
    // each folded on its own, and Approve waits on both.
    label = 'File edit awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Edit this file?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={edit.path} />
        </p>
        <EditBlocks
          edit={edit}
          shown={editShown}
          onShow={(which) => setEditShown((prev) => ({ ...prev, [which]: true }))}
          className={REQUEST_TEXT}
        />
      </>
    );
  } else if (read) {
    // fileguard.Abs refuses a path with a control character or past 4,096 bytes,
    // so it is one line and never folded.
    label = 'File read awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Read this file?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={read.path} />
        </p>
      </>
    );
  } else if (page) {
    // The sidecar caps the URL at 8,192 bytes, so it is never folded. The host
    // is the one it dials, on a line of its own, since a long URL can hide it.
    label = 'Page fetch awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Fetch this page?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={page.url} />
        </p>
        <LabelledLine label="Host:" text={page.host} />
      </>
    );
  } else if (memory?.op === 'save') {
    // The name has passed the store's check before any request exists, so it is
    // one short line. The body is kept as sent, so its line ends are spelled as a
    // file's are.
    label = 'Memory save awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Remember this for every cluster?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={memory.name} />
        </p>
        <FoldedText head={head} rest={rest} shown={shown} onShow={() => setShown(true)} className={REQUEST_TEXT} file />
        <p className="mt-1 text-xs text-muted-foreground">Kubetail will read it in every cluster&apos;s chats.</p>
      </>
    );
  } else if (memory?.op === 'forget') {
    // A forget carries no body, and the dialog shows what the note holds.
    label = 'Memory forget awaiting approval';
    body = (
      <>
        <p className="text-xs text-muted-foreground">Forget this memory for every cluster?</p>
        <p className="mt-1 font-mono text-xs break-all whitespace-pre-wrap">
          <VisibleText text={memory.name} />
        </p>
      </>
    );
  } else {
    drawn = false;
    body = <p className="text-xs text-muted-foreground">This request can&apos;t be shown.</p>;
  }

  return (
    <div
      id={approvalAnchor(approval.id)}
      role="group"
      aria-label={label}
      className="mt-2 rounded-md border border-border p-2"
    >
      {agent !== null && (
        <p className="mb-1 flex min-w-0 gap-1 text-xs text-muted-foreground">
          <span className="shrink-0">An agent asks:</span>
          {agent !== '' && <ModelDescription line={agent} />}
        </p>
      )}
      {body}
      {failure && <p className="mt-1 text-xs text-destructive">{failure}</p>}
      {/* Approve and the allow answers wait for their place to hold; Deny never
          does, since refusing the wrong request costs one more question, not a
          command. */}
      <div ref={buttons} className="mt-2 flex flex-wrap items-start gap-2">
        {drawn && (
          <Button type="button" size="xs" disabled={!approvable} onClick={() => decide('Once')}>
            {change ? 'Approve once' : 'Approve'}
          </Button>
        )}
        {drawn && change?.action?.grantable && (
          <>
            <AllowAnswer
              label="Allow for this command"
              rule={change.action.commandRule}
              disabled={!approvable}
              onClick={() => decide('Command')}
            />
            <AllowAnswer
              label="Allow for this chat"
              rule={change.action.chatRule}
              disabled={!approvable}
              onClick={() => decide('Chat')}
            />
            <AllowAnswer
              label="Always allow"
              rule={change.action.chatRule}
              disabled={!approvable}
              onClick={() => decide('Always')}
            />
          </>
        )}
        <Button type="button" size="xs" variant="outline" disabled={!live || decided} onClick={() => decide('Deny')}>
          Deny
        </Button>
      </div>
    </div>
  );
}

// How a notice's task ended, in the transcript's words, and for a command
// whether an agent started it: the answer that asked for it never drew the call.
function noticeHead(notice: TaskNotice): string {
  if (notice.kind === 'agent') return agentNoticeEnd(notice);
  if (notice.kind === 'watch') return watchNoticeEnd(notice);
  return commandNoticeEnd(notice) + (notice.agentDescription !== '' ? ' · started by an agent' : '');
}

// An agent's end. Its report is not drawn here: the user reads it in the Agent call.
function agentNoticeEnd(notice: TaskNoticeOf<'agent'>): string {
  switch (notice.status) {
    case 'completed':
      return 'Agent finished';
    case 'failed':
      return 'Agent failed';
    case 'stopped':
      return notice.stoppedBy === 'unanswered' ? 'Agent stopped: no answer in 30 minutes' : 'Agent stopped';
    case 'lost':
      return 'Agent lost when Kubetail stopped';
  }
}

// A watch's end. Its lines are not drawn here: the user reads them on its card.
function watchNoticeEnd(notice: TaskNoticeOf<'watch'>): string {
  switch (notice.status) {
    case 'matched':
      return 'Log watch matched';
    case 'expired':
      return 'Log watch ended: nothing matched';
    case 'failed':
      return 'Log watch failed';
    case 'stopped':
      return 'Log watch stopped';
    case 'lost':
      return 'Log watch lost when Kubetail stopped';
  }
}

function commandNoticeEnd(notice: TaskNoticeOf<'command'>): string {
  switch (notice.status) {
    case 'exited':
      return notice.exitCode === null
        ? 'Background command finished'
        : `Background command finished · exit ${notice.exitCode}`;
    case 'stopped':
      return 'Background command stopped';
    case 'lost':
      return 'Background command lost when Kubetail stopped';
  }
}

// One muted line per notice a message carried: how the task ended, then the task
// by the model's description, else a command's text. Both are the model's, so
// they are drawn as the transcript's calls are.
function Notices({ notices }: { notices: TaskNotice[] }) {
  return (
    <div className="flex w-full flex-col items-end gap-1 text-xs text-muted-foreground">
      {notices.map((notice) => (
        <p key={notice.id} className="flex max-w-[80%] min-w-0 gap-1">
          <span className="shrink-0">{noticeHead(notice)} —</span>
          {notice.description !== '' ? (
            <ModelDescription line={notice.description} />
          ) : (
            <span className="min-w-0 truncate font-mono">
              <VisibleText text={notice.command} />
            </span>
          )}
        </p>
      ))}
    </div>
  );
}

function Message({
  message,
  chatID,
  clusterID,
  mode,
  ends,
  label,
  labelOf,
  last,
  onAskAgain,
  approveArmMs,
  sandboxAvailable,
}: {
  message: ChatMessage;
  chatID: string;
  /** The chat's cluster, which a view's card sets with it. */
  clusterID: string | undefined;
  mode: AppMode;
  ends: ViewEnds;
  approveArmMs: number;
  sandboxAvailable: boolean | undefined;
  /** The model this answer ran on, when it differs from the answer before it. */
  label?: string;
  /** How a model is named, by its provider and id. */
  labelOf: (providerID: string | undefined, id: string) => string;
  /** The transcript's last row, whose answer always shows its actions. */
  last: boolean;
  /** Present on a failed last answer alone; disabled while anything else is in flight. */
  onAskAgain?: () => void;
}) {
  const text = textOf(message.content);
  const sources = sourcesOf(message.citations);
  const searches = searchesOf(message.toolCalls);
  const notices = noticesOf(message.content);
  const { own, byAgent } = splitCalls(message.toolCalls);
  // A search that ran is drawn by its query alone, under Searched the web; every
  // other call is a disclosure, the provider's included, since it left the machine.
  const done = own.filter((call) => !ranSearch(call) && call.status !== 'AwaitingApproval');
  const worked = done.filter((call) => !standsOut(call));
  const shown = done.filter(standsOut);
  const waiting = waitingRequestsOf(message.toolCalls);
  const agentOf = (call: ChatToolCall) =>
    call.agentCallID === null
      ? null
      : descriptionLine(message.toolCalls.find((c) => c.id === call.agentCallID)?.action?.description ?? '');
  const modelLabel = (id: string) => labelOf(message.provider?.id, id);
  const toolCallsOf = (calls: ChatToolCall[]) => (
    <ToolCalls
      calls={calls}
      byAgent={byAgent}
      modelLabel={modelLabel}
      chatID={chatID}
      clusterID={clusterID}
      mode={mode}
      ends={ends}
    />
  );
  const mine = message.role === 'User';
  const working = inFlight(message.status);
  // A message of notices alone is its lines and nothing else: there is no bubble.
  if (mine && text === '' && notices.length > 0) {
    return (
      <article>
        <Notices notices={notices} />
      </article>
    );
  }
  return (
    <article
      className={mine ? 'group/turn flex flex-col items-end gap-1' : 'group/turn flex flex-col items-start gap-1'}
    >
      {mine && notices.length > 0 && <Notices notices={notices} />}
      {label && <p className="px-1 text-xs text-muted-foreground">{label}</p>}
      <div
        className={
          mine
            ? 'max-w-[80%] rounded-lg bg-muted px-3 py-2 text-sm whitespace-pre-wrap'
            : 'w-full px-1 pt-2 text-sm *:last:mb-0'
        }
      >
        {!mine && (message.thinking !== '' || worked.length > 0) && (
          <Work
            summary={message.thinking}
            calls={worked.length > 0 && toolCallsOf(worked)}
            stillThinking={working && text === ''}
            createdAt={message.createdAt}
            finishedAt={message.finishedAt}
          />
        )}
        {mine ? text : <Markdown text={text} />}
        {working && (
          <span className="ml-1 inline-flex align-middle" role="status" aria-label="Answering">
            <Spinner size="sm" />
          </span>
        )}
        {shown.length > 0 && <div className="mt-1">{toolCallsOf(shown)}</div>}
        {waiting.map(({ call, approval, change }) => (
          // Keyed on the approval, never the call or the message, so a decided
          // request's pressed state never reaches the next. Live while a run of
          // the answer waits on the user, which an agent's can after it settled.
          <ApprovalRequest
            key={approval.id}
            approval={approval}
            action={call.action}
            change={change}
            live={approval.status === 'Pending' && message.awaitingApproval}
            agent={agentOf(call)}
            armMs={approveArmMs}
            sandboxAvailable={sandboxAvailable}
          />
        ))}
        {sources.length > 0 && <Sources sources={sources} />}
        {searches.count > 0 && <Searches count={searches.count} queries={searches.queries} />}
        {message.status === 'Cancelled' && <p className="mt-1 text-xs text-muted-foreground">Stopped</p>}
        {message.status === 'Complete' &&
          (text === '' || message.finishReason === 'tool_use' || message.finishReason === 'pause_turn') && (
            <p className="text-xs text-muted-foreground">Stopped: {message.finishReason}</p>
          )}
        {message.status === 'Failed' && (
          <p className="mt-1 text-xs text-destructive">{message.error || 'The answer failed.'}</p>
        )}
        {message.status === 'Failed' && message.provider?.id === CHATGPT_ID && (
          <PlanFailure limit={message.failure === 'UsageLimit'} />
        )}
      </div>
      <TurnActions mine={mine} text={text} createdAt={message.createdAt} last={last} working={working} />
      {onAskAgain && (
        <Button type="button" variant="outline" size="xs" onClick={onAskAgain}>
          Ask again
        </Button>
      )}
    </article>
  );
}

// PlanFailure is the way out under a failed answer on the ChatGPT plan: its usage
// settings, and at a limit whose limit it may be.
function PlanFailure({ limit }: { limit: boolean }) {
  const openUsage = useOpenChatGPTUsage();
  return (
    <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
      {limit && <span>The limit may be your plan&apos;s or the one set for Kubetail.</span>}
      <Button type="button" variant="outline" size="xs" onClick={openUsage}>
        Manage usage
      </Button>
    </div>
  );
}

// Memoized by hand, so the transcript's promise holds where the compiler bails
// out: a frame of a streaming answer renders that answer's row alone. Every prop
// `ChatTranscript` passes must therefore keep its identity from frame to frame.
const MessageRow = memo(Message);

// The question a failed answer followed, which Ask again sends afresh; null when
// that message was notices alone, since then there is nothing to ask.
function questionBefore(messages: ChatMessage[], index: number): string | null {
  const question = messages
    .slice(0, index)
    .toReversed()
    .find((m) => m.role === 'User');
  if (!question) return '';
  const text = textOf(question.content);
  if (text === '' && noticesOf(question.content).length > 0) return null;
  return text;
}

export function ChatTranscript({
  messages,
  phase,
  chatID,
  mode,
  clusterID,
  approveArmMs = APPROVE_ARM_MS,
  sandboxAvailable,
  sandboxDisabled,
  networkEnabled,
  switching = false,
  footer,
}: ChatTranscriptProps) {
  const { models } = useModels();
  const { send, askAgain } = useChatOutbox(mode, chatID, clusterID);
  const scroller = useRef<HTMLDivElement>(null);
  // Whether the reader is at the end, sampled as they scroll. Starts true so opening
  // a chat lands at the bottom.
  const following = useRef(true);

  // After every commit, follow the answer down — but only for a reader who was
  // already there.
  useEffect(() => {
    const el = scroller.current;
    if (el && following.current) el.scrollTop = el.scrollHeight;
  });

  const onScroll = () => {
    const el = scroller.current;
    if (el) following.current = el.scrollHeight - el.scrollTop - el.clientHeight <= PIN_SLACK_PX;
  };

  // A model's name: the catalog's display name, or the stored id where a key has
  // since been taken away.
  const labelOf = useCallback(
    (providerID: string | undefined, id: string) =>
      modelOf(models, providerID ? { providerID, id } : null)?.label ?? id,
    [models],
  );

  const drawn = messages.filter(isDrawn);
  const ends = useViewEnds(messages);
  // Labelled where the model changed, so a switch is visible where it happened and a
  // chat that stayed on one model — the first answer included — stays unlabelled.
  const rows: { message: (typeof drawn)[number]; label: string | undefined }[] = [];
  let ran: string | undefined;
  for (const message of drawn) {
    const answered = message.role === 'Assistant' && message.model !== '';
    const label =
      answered && ran !== undefined && message.model !== ran ? labelOf(message.provider?.id, message.model) : undefined;
    if (answered) ran = message.model;
    rows.push({ message, label });
  }

  // Only the last row: a fresh send lands at the end, and an answer to a question
  // mid-transcript would sit far from it. Nothing is asked twice while the entry's
  // one send is busy, which a second turn into the chat would be refused for anyway.
  const last = drawn.at(-1);
  const failed = last?.role === 'Assistant' && last.status === 'Failed' && last.provider !== null;
  const question = failed ? questionBefore(messages, messages.indexOf(last)) : null;
  const onAskAgain =
    failed &&
    question !== null &&
    send.status === 'idle' &&
    !switching &&
    sandboxDisabled !== undefined &&
    networkEnabled !== undefined
      ? () =>
          askAgain(
            question,
            { model: { providerID: last.provider!.id, id: last.model }, effort: last.effort },
            { sandboxDisabled, networkEnabled },
          )
      : undefined;

  let body;
  if (phase === 'connecting') {
    body = (
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        <Spinner size="sm" />
        Loading messages…
      </div>
    );
  } else if (drawn.length === 0) {
    body = <p className="text-sm text-muted-foreground">No messages yet.</p>;
  } else {
    body = rows.map(({ message, label }) => (
      <MessageRow
        key={message.id}
        message={message}
        chatID={chatID}
        clusterID={clusterID}
        mode={mode}
        ends={ends}
        label={label}
        labelOf={labelOf}
        last={message.id === last?.id}
        onAskAgain={message.id === last?.id ? onAskAgain : undefined}
        approveArmMs={approveArmMs}
        sandboxAvailable={sandboxAvailable}
      />
    ));
  }

  // The scroller is the whole pane, so its bar sits at the pane's edge.
  return (
    <div
      ref={scroller}
      onScroll={onScroll}
      data-testid="chat-transcript"
      className="flex min-h-0 flex-1 flex-col overflow-y-auto"
    >
      <div className={`flex w-full grow flex-col ${COLUMN[mode]}`}>
        {/* Capped, so the answer keeps a readable line length. */}
        <div className="mx-auto flex w-full max-w-163.5 grow flex-col gap-3 p-4">{body}</div>
        {footer && <div className={`sticky bottom-0 ${FOOTER[mode]}`}>{footer}</div>}
      </div>
    </div>
  );
}
