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

// What a window reads about chats: the recent list and one chat's transcript, each a
// collection watch folded into an id-keyed map. Stored records with no cache behind them,
// so there is no provenance to guard and no need for `useCacheCollectionWatch`.
import { useMemo, useState } from 'react';

import { graphql } from '@/gql';
import type {
  ChatMessageCollectionWatchSubscription,
  ChatMode,
  ChatCollectionWatchSubscription,
  ToolActionKind,
} from '@/gql/graphql';
import type { AppMode } from '@/lib/app-mode';
import { collectionWatchReducer } from '@/lib/graphql/collection-watch';
import type { Keyed } from '@/lib/graphql/collection-watch';
import { useFoldedSubscription, subscriptionPhase } from '@/lib/graphql/use-folded-subscription';
import type { SubscriptionPhase } from '@/lib/graphql/use-folded-subscription';

const ChatCollectionWatchDocument = graphql(`
  subscription ChatCollectionWatch {
    chatCollectionWatch {
      type
      chat {
        id
        title
        mode
        clusterID
        createdAt
        updatedAt
        awaitingApproval
        sandboxDisabled
        networkEnabled
      }
    }
  }
`);

const ChatMessageCollectionWatchDocument = graphql(`
  subscription ChatMessageCollectionWatch($chatID: ChatID!) {
    chatMessageCollectionWatch(chatID: $chatID) {
      type
      message {
        id
        chatID
        seq
        role
        createdAt
        finishedAt
        content
        thinking
        status
        awaitingApproval
        error
        failure
        model
        provider {
          id
          label
        }
        effort
        finishReason
        toolCalls {
          id
          name
          actionKind
          status
          runsOn
          action {
            description
            command {
              text
              cwd
              background
              sandboxed
              network
              env
            }
            read {
              path
            }
            write {
              path
              content
            }
            edit {
              path
              oldString
              newString
              replaceAll
            }
            search {
              query
            }
            fetch {
              url
              host
            }
            memory {
              op
              name
              body
              scope
            }
            delegate {
              prompt
              agentType
              model
            }
            kubeQuery {
              sql
              limit
            }
            logsView {
              sources {
                namespace
                kind
                name
                containers
                allContainers
                previous
              }
              filters {
                field
                values
              }
              grep
              anchor {
                kind
                at
              }
              pinToEnd
              resolvedAt
            }
            logsRead {
              sources {
                namespace
                kind
                name
                containers
                allContainers
                previous
              }
              filters {
                field
                values
              }
              grep
              since
              until
              before {
                lines
                span
              }
              after {
                lines
                span
              }
              pageFrom
              resolvedAt
              lines {
                timestamp
                namespace
                pod
                container
                previous
                message
                gap {
                  after
                  before
                }
                truncatedBytes
              }
              olderMore
              newerMore
              notices {
                code
                message
              }
              viewAround {
                sources {
                  namespace
                  kind
                  name
                  containers
                  allContainers
                  previous
                }
                filters {
                  field
                  values
                }
                grep
                anchor {
                  kind
                  at
                }
                pinToEnd
                resolvedAt
              }
            }
            logsWatch {
              sources {
                namespace
                kind
                name
                containers
                allContainers
                previous
              }
              filters {
                field
                values
              }
              grep
              since
              limit
              maxBytes
              maxMessageBytes
              resolvedAt
              until
            }
          }
          agentCallID
          approval {
            id
            status
            duration
          }
          network
          sandboxActions {
            approval {
              id
              status
              duration
            }
            action {
              target
              summary
              accessLevel
              principal
              scope
              accountName
              service
              operation
              resource
              name
              once
              grantable
              commandRule
              chatRule
            }
            request {
              __typename
              ... on ClusterWrite {
                method
                path
                subresource
                contentType
                body
                dryRun
                diff
                diffCut
                diffError
              }
              ... on CloudRequest {
                host
                method
                path
                contentType
                body
                unshown
                details {
                  label
                  value
                }
              }
            }
            reason
          }
          output
          background {
            status
            exitCode
            report
            matched {
              timestamp
              namespace
              pod
              container
              previous
              message
              gap {
                after
                before
              }
              truncatedBytes
            }
            endedBecause
            viewAround {
              sources {
                namespace
                kind
                name
                containers
                allContainers
                previous
              }
              filters {
                field
                values
              }
              grep
              anchor {
                kind
                at
              }
              pinToEnd
              resolvedAt
            }
          }
        }
        citations {
          type
          url
          title
          citedText
        }
      }
    }
  }
`);

export type Chat = NonNullable<ChatCollectionWatchSubscription['chatCollectionWatch']['chat']>;
export type ChatMessage = NonNullable<ChatMessageCollectionWatchSubscription['chatMessageCollectionWatch']['message']>;
export type ChatToolCall = ChatMessage['toolCalls'][number];
export type ChatSandboxAction = ChatToolCall['sandboxActions'][number];
type ChatSandboxRequest = NonNullable<ChatSandboxAction['request']>;
export type ChatClusterRequest = Extract<ChatSandboxRequest, { __typename: 'ClusterWrite' }>;
export type ChatCloudRequest = Extract<ChatSandboxRequest, { __typename: 'CloudRequest' }>;
export type PermissionTarget = NonNullable<ChatSandboxAction['action']>['target'];

/** A cloud platform's name in the user's words. */
export const CLOUD_PLATFORM_NAMES: Record<PermissionTarget, string> = {
  Kubernetes: 'Kubernetes',
  AWS: 'AWS',
  GitHub: 'GitHub',
  GCP: 'Google Cloud',
  Azure: 'Azure',
};

/** The cluster request an action carries, null for any other. */
export function clusterRequestOf(a: ChatSandboxAction): ChatClusterRequest | null {
  return a.request?.__typename === 'ClusterWrite' ? a.request : null;
}

/** The cloud platform request an action carries, null for any other. */
export function cloudRequestOf(a: ChatSandboxAction): ChatCloudRequest | null {
  return a.request?.__typename === 'CloudRequest' ? a.request : null;
}
export type ChatCitation = ChatMessage['citations'][number];

// Whether the turn behind an answer is still running: streaming, or stopped on
// a command the user has not decided.
export function inFlight(status: ChatMessage['status']): boolean {
  return status === 'Streaming' || status === 'WaitingApproval';
}

// The wire's spelling of the app's mode. The list filter and the send both go
// through here, so they cannot disagree.
export const chatModeOf = (mode: AppMode): ChatMode => (mode === 'dashboard' ? 'Dashboard' : 'Chat');

// A chat's title as the app names it. A chat can have none (its title reads as ""), and
// a button or a sentence naming it would then be empty.
export const chatTitleOf = (title: string): string => title || 'Untitled chat';

// The sidecar's rules for a title it is sent (`Rename`): trimmed of what Go's
// `strings.TrimSpace` trims, then at most 80 code points on one line. JavaScript's `trim` is not
// Go's: it keeps U+0085 and drops U+FEFF. A title can be a paste of any length, so both
// read only as far as they must.
export const MAX_CHAT_TITLE_LENGTH = 80;

// Go's `unicode.IsSpace`. Every one is a single UTF-16 unit.
function isGoSpace(code: number): boolean {
  return (
    (code >= 0x09 && code <= 0x0d) ||
    code === 0x20 ||
    code === 0x85 ||
    code === 0xa0 ||
    code === 0x1680 ||
    (code >= 0x2000 && code <= 0x200a) ||
    code === 0x2028 ||
    code === 0x2029 ||
    code === 0x202f ||
    code === 0x205f ||
    code === 0x3000
  );
}

export function normalizeChatTitle(title: string): string {
  let start = 0;
  let end = title.length;
  while (start < end && isGoSpace(title.charCodeAt(start))) start++;
  while (end > start && isGoSpace(title.charCodeAt(end - 1))) end--;
  return title.slice(start, end);
}

export function chatTitleTooLong(title: string): boolean {
  const codePoints = normalizeChatTitle(title)[Symbol.iterator]();
  for (let i = 0; i < MAX_CHAT_TITLE_LENGTH; i++) {
    if (codePoints.next().done) return false;
  }
  return !codePoints.next().done;
}

// Unicode's mandatory line breaks, the sidecar's `lineBreaks`.
const LINE_BREAK = /[\n\v\f\r\u0085\u2028\u2029]/;

export function chatTitleHasLineBreak(title: string): boolean {
  return LINE_BREAK.test(normalizeChatTitle(title));
}

const reduceChats = collectionWatchReducer(
  ({ chatCollectionWatch: { type, chat } }: ChatCollectionWatchSubscription) => ({
    type,
    item: chat,
  }),
);

const reduceChatMessages = collectionWatchReducer(
  ({ chatMessageCollectionWatch: { type, message } }: ChatMessageCollectionWatchSubscription) => ({
    type,
    item: message,
  }),
);

// Both watches are `unshared`: a consumer can mount into a stream already running
// (the sidebar's list when the card reopens, a chat another window is streaming),
// and a shared stream never replays the snapshot that opened it.

/** The recent list, newest first. */
export function useChats(): { chats: Chat[]; phase: SubscriptionPhase } {
  const { data, connected } = useFoldedSubscription({ query: ChatCollectionWatchDocument }, reduceChats, {
    unshared: true,
  });

  // A reconnect replays the list from nothing. Until its Bookmark, the last synced list
  // stands in for the rows the replay has yet to reach, each replaced as its own record
  // lands: only the Bookmark can say a chat is gone.
  const [lastSynced, setLastSynced] = useState<Keyed<Chat>>();
  if (data?.snapshotComplete && data.items !== lastSynced) setLastSynced(data.items);
  const items = useMemo(() => {
    if (data?.snapshotComplete || !lastSynced) return data?.items;
    return new Map([...lastSynced, ...(data?.items ?? [])]);
  }, [data, lastSynced]);
  // `updatedAt` is an ISO string, so it is parsed rather than compared as text. The
  // sidecar stores millis, so two chats can share one; the id (opaque text) breaks the
  // tie, so the order is at least stable.
  const chats = useMemo(
    () =>
      [...(items?.values() ?? [])].toSorted(
        (a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt) || b.id.localeCompare(a.id),
      ),
    [items],
  );

  return { chats, phase: subscriptionPhase(!!data?.snapshotComplete, connected) };
}

/** One chat's messages in `seq` order. A streaming answer arrives as `Modified` frames carrying the whole text so far. */
export function useChatMessages(chatID: string): { messages: ChatMessage[]; phase: SubscriptionPhase } {
  const { data, connected } = useFoldedSubscription(
    { query: ChatMessageCollectionWatchDocument, variables: { chatID } },
    reduceChatMessages,
    { unshared: true },
  );

  const items = data?.items;
  // `seq` is the order; `createdAt` is not, since two windows can send into one chat.
  const messages = useMemo(() => [...(items?.values() ?? [])].toSorted((a, b) => a.seq - b.seq), [items]);

  return { messages, phase: subscriptionPhase(!!data?.snapshotComplete, connected) };
}

type TextBlock = { type: 'text'; text: string };

function isTextBlock(block: unknown): block is TextBlock {
  if (typeof block !== 'object' || block === null) return false;
  const { type, text } = block as { type?: unknown; text?: unknown };
  return type === 'text' && typeof text === 'string';
}

function isToolBlock(block: unknown): boolean {
  if (typeof block !== 'object' || block === null) return false;
  const { type } = block as { type?: unknown };
  return type === 'tool_use' || type === 'tool_result';
}

/** A call the provider ran for the model, kept in the content for the replay alone. */
function isServerUseBlock(block: unknown): boolean {
  if (typeof block !== 'object' || block === null) return false;
  return (block as { type?: unknown }).type === 'server_use';
}

/** One source an answer cites: where it is and what it is called. Both are web text. */
export type Source = { url: string; title: string };

/**
 * The words in a message's content blocks, in order. `content` is `JSON!` on the wire
 * and arrives `unknown`; this is its one reader. Thinking, context, tool and
 * server-use blocks draw nothing here. Two text blocks with a tool round or a
 * server call between them were written at different moments, so a blank line separates
 * them, the way the sidecar's `llm.Text` joins them. Content that is not an array of blocks yields
 * '' rather than throwing.
 */
export function textOf(content: unknown): string {
  if (!Array.isArray(content)) return '';
  let text = '';
  let separate = false;
  content.forEach((block) => {
    if (isToolBlock(block) || isServerUseBlock(block)) {
      separate = text !== '';
    } else if (isTextBlock(block)) {
      text += (separate ? '\n\n' : '') + block.text;
      separate = false;
    }
  });
  return text;
}

/** A call's kind, spelled. Keyed by the generated union, so a new kind is a type error here. */
const ACTION_KIND_LABELS: Record<ToolActionKind, string> = {
  Command: 'Command',
  Read: 'Read',
  Write: 'Write',
  Edit: 'Edit',
  Search: 'Search',
  Fetch: 'Fetch page',
  Stop: 'Stop task',
  Memory: 'Memory',
  Delegate: 'Agent',
  KubeQuery: 'Query',
  LogsView: 'Logs',
  LogsRead: 'Read logs',
  LogsWatch: 'Watch logs',
};

export function actionKindLabel(kind: ToolActionKind): string {
  return ACTION_KIND_LABELS[kind];
}

/**
 * Whether a call draws Grant a folder… under it: a sandboxed command that
 * failed. Nothing tells a hidden path from any other failure, so every one
 * offers it.
 */
export function grantOffered(call: Pick<ChatToolCall, 'action' | 'status' | 'background'>): boolean {
  if (!call.action?.command?.sandboxed) return false;
  // A background call answers at once, so its end is the task's; a code the
  // sidecar could not read may be a failure.
  if (call.background) return call.background.status === 'Exited' && call.background.exitCode !== 0;
  return call.status === 'Failed';
}

/**
 * Whether a call is a search that ran: the provider's, which has no status, or
 * the sidecar's once it has succeeded. The kind is the tool's, so a search
 * whose arguments did not parse still counts.
 */
export function ranSearch(call: Pick<ChatToolCall, 'actionKind' | 'runsOn' | 'status'>): boolean {
  return call.actionKind === 'Search' && (call.runsOn === 'Provider' || call.status === 'Succeeded');
}

/**
 * An answer's searches: how many of its own ran (ranSearch), and the queries
 * they asked, in order, empty ones left out. A subagent's are drawn inside
 * its Agent call instead. A search whose action is null counts but lists
 * nothing. The queries are the model's words, and they are what left the
 * machine: read as data, drawn as text.
 */
export function searchesOf(
  calls: readonly Pick<ChatToolCall, 'actionKind' | 'runsOn' | 'status' | 'action' | 'agentCallID'>[],
): {
  count: number;
  queries: string[];
} {
  const ran = calls.filter((call) => call.agentCallID === null && ranSearch(call));
  return {
    count: ran.length,
    queries: ran.map((call) => call.action?.search?.query ?? '').filter((query) => query !== ''),
  };
}

/**
 * The sources an answer drew on, one per URL, in the order it cites them, the
 * first title winning. A citation with no URL (a document the model was given)
 * is not a source to list. All of it is web text: read as data, drawn as text.
 */
export function sourcesOf(citations: readonly ChatCitation[]): Source[] {
  const seen = new Set<string>();
  const sources: Source[] = [];
  citations.forEach(({ url, title }) => {
    if (url === '' || seen.has(url)) return;
    seen.add(url);
    sources.push({ url, title });
  });
  return sources;
}

/**
 * One request waiting on the user: a call's own, or an action its sandboxed
 * command sent to the cluster or a cloud platform, which `change` holds (null for a
 * call's own).
 */
export type WaitingRequest = {
  call: ChatToolCall;
  approval: NonNullable<ChatToolCall['approval']>;
  change: ChatSandboxAction | null;
};

/** Whether a sandbox action waits on the user: pending, while its call runs. */
export function isWaitingAction(call: ChatToolCall, action: ChatSandboxAction): boolean {
  return action.approval.status === 'Pending' && call.status === 'Running';
}

/** The DOM id of the approval request for approval `id`: what the composer's Show scrolls to. */
export const approvalAnchor = (id: string): string => `approval-${id}`;

/**
 * The requests awaiting approval, in the order they were asked — approval ids
 * are UUIDv7 — so a new one lands below the rest: an answer's own calls and
 * sandbox actions, the cluster's and each cloud platform's, and each of its agents',
 * which wait whether or not the answer is still going.
 */
export function waitingRequestsOf(calls: ChatToolCall[]): WaitingRequest[] {
  return calls
    .flatMap((call): WaitingRequest[] => {
      const own =
        call.status === 'AwaitingApproval' && call.approval ? [{ call, approval: call.approval, change: null }] : [];
      const actions = call.sandboxActions
        .filter((a) => isWaitingAction(call, a))
        .map((a) => ({ call, approval: a.approval, change: a }));
      return [...own, ...actions];
    })
    .toSorted((a, b) => (a.approval.id < b.approval.id ? -1 : 1));
}

// The statuses each kind of task ends in.
const NOTICE_ENDS = {
  command: ['exited', 'stopped', 'lost'],
  agent: ['completed', 'failed', 'stopped', 'lost'],
  watch: ['matched', 'expired', 'failed', 'stopped', 'lost'],
} as const;

const NOTICE_STATUSES = new Set<string>(Object.values(NOTICE_ENDS).flat());

/**
 * How one background task ended, as a notice told the model: what the
 * transcript draws of the sidecar's `llm.TaskNotice`. A task is a background
 * command, an agent or a log watch; a notice naming no kind reads as a
 * command's. `stoppedBy` is who stopped a stopped task, '' otherwise.
 * `description` and `command` are the model's text, one line each, cut by the
 * sidecar; `agentDescription` is the description of the Agent call whose
 * subagent started a command, '' for the answer's own. An agent's report and
 * error, and a watch's sample, are the model's copy, never read here: the user
 * reads the report in the Agent call and the lines on the watch's card.
 */
export type TaskNoticeOf<K extends keyof typeof NOTICE_ENDS> = {
  id: string;
  kind: K;
  status: (typeof NOTICE_ENDS)[K][number];
  stoppedBy: string;
  exitCode: number | null;
  description: string;
  command: string;
  agentDescription: string;
};

export type TaskNotice = TaskNoticeOf<'command'> | TaskNoticeOf<'agent'> | TaskNoticeOf<'watch'>;

const stringOr = (value: unknown): string => (typeof value === 'string' ? value : '');

/**
 * The notices a message carries, in order: each background task whose end rode
 * it to the model. The one reader of a notice block; a block of any other shape
 * is dropped. A status its kind never ends in reads as lost.
 */
export function noticesOf(content: unknown): TaskNotice[] {
  if (!Array.isArray(content)) return [];
  return content.flatMap((block): TaskNotice[] => {
    if (typeof block !== 'object' || block === null) return [];
    const { type, task } = block as { type?: unknown; task?: unknown };
    if (type !== 'task_notification' || typeof task !== 'object' || task === null) return [];
    const {
      id,
      kind,
      status,
      stopped_by: stoppedBy,
      exit_code: code,
      description,
      command,
      agent_description: agentDescription,
    } = task as Record<string, unknown>;
    if (typeof id !== 'string' || typeof status !== 'string' || !NOTICE_STATUSES.has(status)) return [];
    const ofKind = kind === 'agent' || kind === 'watch' ? kind : 'command';
    const ends: readonly string[] = NOTICE_ENDS[ofKind];
    return [
      {
        id,
        kind: ofKind,
        status: ends.includes(status) ? status : 'lost',
        stoppedBy: stringOr(stoppedBy),
        exitCode: typeof code === 'number' ? code : null,
        description: stringOr(description),
        command: stringOr(command),
        agentDescription: stringOr(agentDescription),
      } as TaskNotice,
    ];
  });
}

/** What a KubeQuery call answered: the cluster it read and the rows. */
export type QueryResult = {
  cluster: string;
  columns: string[];
  /** Each row's cells as the sidecar rendered them: strings, numbers, null, or JSON. */
  rows: unknown[][];
  /** Whether rows were left out past the limit or the answer's size. */
  more: boolean;
};

/**
 * A KubeQuery call's output as rows, or null for any answer that is not rows: an
 * error, a cache that withheld them, or a result saved to a file. The one reader of
 * a query's output; the caller draws the output as text when it answers null.
 */
export function queryResultOf(output: string): QueryResult | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(output);
  } catch {
    return null;
  }
  if (typeof parsed !== 'object' || parsed === null) return null;
  const { cluster, columns, rows, more } = parsed as Record<string, unknown>;
  if (!Array.isArray(columns) || !columns.every((c) => typeof c === 'string')) return null;
  if (!Array.isArray(rows) || !rows.every((r) => Array.isArray(r) && r.length === columns.length)) return null;
  return { cluster: stringOr(cluster), columns, rows: rows as unknown[][], more: more === true };
}
