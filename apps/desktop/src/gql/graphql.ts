/* eslint-disable */
/** Internal type. DO NOT USE DIRECTLY. */
type Exact<T extends { [key: string]: unknown }> = { [K in keyof T]: T[K] };
/** Internal type. DO NOT USE DIRECTLY. */
export type Incremental<T> = T | { [P in keyof T]?: P extends ' $fragmentName' | '__typename' ? T[P] : never };
import type { TypedDocumentNode as DocumentNode } from '@graphql-typed-document-node/core';
/** The kind of failure a `Failed` answer met. */
export type AnswerFailure =
  /** The provider refused to read the chat: it is longer than the model can read. */
  | 'ContextFull'
  /** Anything else. */
  | 'Other'
  /** The provider refused the request at a plan's usage limit. */
  | 'UsageLimit';

/** The user's answer to a request. `Once` and `Deny` answer this request alone; `Command` also allows the same change for the rest of the command; `Chat` and `Always` also write an Allow rule for the action's class and scope. */
export type ApprovalDecision =
  | 'Always'
  | 'Chat'
  | 'Command'
  | 'Deny'
  | 'Once';

/** How long an approval holds: this request, the rest of the command, the chat, or always. */
export type ApprovalDuration =
  | 'Always'
  | 'Chat'
  | 'Command'
  | 'Once';

/**
 * Where the user's decision is. `Pending` until it commits, and after, when the call's turn ended
 * before anyone answered: the record of a question nobody answered. An `Approved` call whose status
 * is `NotRun` was approved and never started. `Abandoned` is a cluster write whose wait ended with no
 * decision while its call ran on; a call's own approval never takes it. `Allowed` and `Refused`
 * are cluster writes the permissions engine decided with nobody asked.
 */
export type ApprovalStatus =
  | 'Abandoned'
  | 'Allowed'
  | 'Approved'
  | 'Denied'
  | 'Pending'
  | 'Refused';

/**
 * Where a background task is. A command ends `Exited`; an agent `Completed` (it answered) or
 * `Failed` (it could not); a watch `Matched` (it held lines, however it ended), `Expired` (its time
 * ran out with none) or `Failed` (an error, or its pods went away, with none). `Stopped` was ended by the model,
 * the user, the app's quit, or an agent's request that went unanswered; `Lost` was running when the
 * sidecar stopped unexpectedly, and a command may still be running.
 */
export type BackgroundTaskStatus =
  | 'Completed'
  | 'Exited'
  | 'Expired'
  | 'Failed'
  | 'Lost'
  | 'Matched'
  | 'Running'
  | 'Stopped';

/**
 * Who produced a message, matching the Messages API wire vocabulary. Not a display hint:
 * a `User` message carrying only tool results is machinery, not something a person typed.
 * What to draw comes from the content blocks.
 */
export type ChatMessageRole =
  | 'Assistant'
  | 'User';

/** How far along an assistant's turn is. A user message is always `Complete`. */
export type ChatMessageStatus =
  /** Stopped by `chatCancel` or by shutdown. The partial text is kept. */
  | 'Cancelled'
  | 'Complete'
  /** Ended badly; `error` says how. Never retried automatically. */
  | 'Failed'
  /** Still being written. Reconciled to `Failed` if the sidecar restarts. */
  | 'Streaming'
  /** Stopped on a command the user has not decided: in flight, drawn with the approval request. */
  | 'WaitingApproval';

/**
 * Which of the app's two modes a chat belongs to. Set when the chat is created and never
 * changed. Each mode lists only its own chats.
 */
export type ChatMode =
  | 'Chat'
  | 'Dashboard';

/**
 * Classifies one frame on a collection watch, mirroring a Kubernetes watch event. On
 * subscribe a watch replays the current set as `Added` frames (the snapshot), closes it
 * with one `Bookmark`, then streams live changes. A `Deleted` carries the object's
 * last-known state; the client keys on its `id`.
 */
export type CollectionWatchFrameType =
  | 'Added'
  | 'Bookmark'
  | 'Deleted'
  | 'Modified';

/** A condition's three-valued verdict, Kubernetes-style. */
export type ConditionStatus =
  /** The condition does not hold. */
  | 'False'
  /** The condition holds. */
  | 'True'
  /** The condition cannot currently be assessed. */
  | 'Unknown';

/** Why a connector's last attempt, session or key ended. A connector answers only the ones it can reach. */
export type ConnectorFailure =
  /** The account is not the one this installation registered; Use a different account changes it. */
  | 'AccountMismatch'
  /** The user declined on the provider's page. */
  | 'Declined'
  /** The provider no longer honours the key; connect again. */
  | 'KeyRejected'
  /** The keyring would not delete the key, which may be one the provider rejected. It is used again after a restart until a Disconnect deletes it. */
  | 'NotDeleted'
  /** This machine could not save the sign-in: its keyring or its data directory refused. */
  | 'NotSaved'
  /** The sign-in could not finish: the provider refused it, the browser could not open, or the callback was malformed. */
  | 'Refused'
  /** The sign-in did not grant the scope the connector needs: for ChatGPT, use of the plan. */
  | 'ScopeNotGranted'
  /** The provider ended the session; sign in again. */
  | 'SessionEnded'
  /** Nobody finished the sign-in in time. */
  | 'TimedOut'
  /** The provider could not be reached. */
  | 'Unavailable';

/** Where a connector is. */
export type ConnectorPhase =
  | 'Connected'
  /** The last attempt, session or key ended; `failure` says why. */
  | 'Failed'
  /** The launch has not read the keyring yet. Neither signed out nor connected. */
  | 'Restoring'
  | 'SignedOut'
  /** The browser is open on the provider's sign-in. */
  | 'SigningIn';

/** The commands a variable reaches. */
export type EnvVarTarget =
  | 'Both'
  /** Commands outside the sandbox, each of which the user approves, shown this variable. */
  | 'Outside'
  /** Commands in the sandbox. */
  | 'Sandbox';

/**
 * Classifies one frame on an event feed. Two values where `CollectionWatchFrameType` has
 * four: an event log is a positioned log, not a mirrored set. The server delivers the
 * snapshot plus what grows above it and never reports a prune, so a run is only ever
 * upserted and there is no `Deleted`.
 */
export type EventFeedFrameType =
  /** Closes the on-subscribe snapshot; sent once, carrying no run. */
  | 'Bookmark'
  /** A run to upsert by `Event.id`. */
  | 'Run';

/** An event's severity, mirroring the control plane's event type: Normal (✓) or Warning (✗). */
export type EventType =
  | 'Normal'
  | 'Warning';

/** How long a folder grant lasts. */
export type GrantDuration =
  /** For every chat. */
  | 'Always'
  /** For one chat: a grant of the chat's, gone with it. */
  | 'Chat';

/** How an occurrence ended. */
export type IncidentOccurrenceEndReason =
  /** Its signal was judged absent for its resolve-after window. */
  | 'Resolved'
  /** Nothing saw it for 24 hours. */
  | 'Stale'
  /** The user switched the read off, or the target reached another server. */
  | 'Stopped';

/**
 * Classifies one frame on an incident's occurrence feed. An occurrence is upserted by
 * `id`; a trimmed or deleted one is never announced, so there is no `Deleted`.
 */
export type IncidentOccurrenceEventFeedFrameType =
  /** Closes the on-subscribe snapshot; sent once, carrying no occurrence. */
  | 'Bookmark'
  /** An occurrence to upsert by `IncidentOccurrence.id`. */
  | 'Occurrence';

/**
 * One page: a first page, which gives `scope` and the window, or a continuation, which gives
 * `cursor` and nothing of them. The three caps are read on every page.
 */
export type LogsFetchInput = {
  after?: LogsPaddingInput | null | undefined;
  /** The window's padding past each bound. */
  before?: LogsPaddingInput | null | undefined;
  /** A page's `prev`, `next` or `follow` as it came. It holds the scope and the window, so sending either beside it is refused. */
  cursor?: string | null | undefined;
  /** Cuts each message; 0 is the server's cap. */
  maxMessageBytes?: number | null | undefined;
  /** Caps the page's messages together; 0 is the server's default. */
  maxPageBytes?: number | null | undefined;
  /** Required on a first page, omitted on a continuation: there is no default. */
  pageFrom?: LogsPageFrom | null | undefined;
  /** 1 to 500; null or 0 is the server's default. */
  pageSize?: number | null | undefined;
  scope?: LogsScopeInput | null | undefined;
  /** A line the caller has of each stream, by its `cursor`: with `First`, or beside a `next` or `follow`, that stream reads from strictly past it; with `Last`, from strictly before it. Every other stream reads from the window's edge, or from where the page left it beside a `cursor`. A first page from `Last` with `seen` hands each entry on in its `prev`, moved back as its stream is read, so a stream whose clock runs behind never reads past it. Refused with padding on a first page, beside a `prev`, and beside a cursor whose `after` is a count. */
  seen?: Array<string> | null | undefined;
  /** The range's bounds, each including every line at that moment. Null is open. */
  since?: string | null | undefined;
  until?: string | null | undefined;
};

/** How far the window extends past one end of the range, to show the lines around it: a number of lines, or a span as an ISO 8601 duration such as `PT5M`, at most seven days. Give one. */
export type LogsPaddingInput = {
  lines?: number | null | undefined;
  span?: string | null | undefined;
};

/** Where a window's first page comes from. */
export type LogsPageFrom =
  | 'First'
  | 'Last';

/** What a read covers: the `LogsViewAction` fields the backend takes, as an input. */
export type LogsScopeInput = {
  filters: Array<LogsViewFilterInput>;
  grep: string;
  sources: Array<LogsViewSourceInput>;
};

export type LogsTailFrameType =
  | 'Ended'
  | 'Line'
  | 'Notice';

/** One tail: what to tail, and where it starts. With neither `since` nor `cursor` it starts from now. */
export type LogsTailInput = {
  /** A page's `next` or `follow`: each stream reads past where that page left it, and on past the page's window. */
  cursor?: string | null | undefined;
  /** Cuts each message; 0 is the server's cap. */
  maxMessageBytes?: number | null | undefined;
  /** What is tailed. Omitted beside `cursor`, which holds it. */
  scope?: LogsScopeInput | null | undefined;
  /** The last line the caller has of each stream, by its `cursor`: that stream reads past it, and a stream not named reads from the start. Refused alone. */
  seen?: Array<string> | null | undefined;
  /** A start: that moment's lines included. Refused beside `cursor`. */
  since?: string | null | undefined;
};

/** The oldest line the backend keeps, the newest, or a moment between. */
export type LogsViewAnchorKind =
  | 'At'
  | 'Head'
  | 'Tail';

/** What a line can be filtered by: the node it ran on, and the node's labels. */
export type LogsViewFilterField =
  | 'Arch'
  | 'Node'
  | 'Os'
  | 'Region'
  | 'Zone';

export type LogsViewFilterInput = {
  field: LogsViewFilterField;
  values: Array<string>;
};

/** One selected line, as the viewer holds it. Cluster text. */
export type LogsViewSelectedLineInput = {
  container: string;
  message: string;
  namespace: string;
  pod: string;
  previous: boolean;
  timestamp: string;
};

/** What the user's open log view shows as they send. The sidecar reads the view itself from the call. */
export type LogsViewSnapshotInput = {
  /** Whether the newest line is on screen and the viewer is following. */
  atEnd: boolean;
  /** The `LogsView`, `LogsRead` or `LogsWatch` call whose view is open. It must be a call of the chat the send is to; a `LogsRead` one must have a `viewAround`, and a `LogsWatch` one a finished task with one. */
  callID: string;
  /** The first and last visible line's timestamps. Both null when no line is on screen. */
  from?: string | null | undefined;
  /** The grep the viewer runs, which the user can change from the call's. Null keeps the call's. */
  grep?: string | null | undefined;
  /** How many lines are on screen. */
  lines: number;
  /** Whether the user paused the viewer: no line arrives until they resume. */
  paused: boolean;
  /** Whether the viewer keeps the end in view. */
  pinnedToEnd: boolean;
  /** The selected lines, in order, at most 50. */
  selected: Array<LogsViewSelectedLineInput>;
  /** How many lines are selected, `selected` included. */
  selectedCount: number;
  to?: string | null | undefined;
};

export type LogsViewSourceInput = {
  allContainers: boolean;
  containers: Array<string>;
  kind: LogsViewSourceKind;
  name: string;
  namespace: string;
  previous: boolean;
};

/** The kinds the log backend reads: a pod, or a workload whose pods it follows. */
export type LogsViewSourceKind =
  | 'CronJob'
  | 'DaemonSet'
  | 'Deployment'
  | 'Job'
  | 'Pod'
  | 'ReplicaSet'
  | 'StatefulSet';

/** Who last wrote a memory. */
export type MemoryAuthor =
  | 'Model'
  | 'User';

export type MemorySaveInput = {
  body: string;
  /** The cluster it is for. Null is every cluster. */
  clusterID?: string | null | undefined;
  /** The memory to rewrite. Null creates one. */
  id?: string | null | undefined;
  name: string;
};

/** The system a target belongs to. The webview draws each value's name. */
export type MonitorIntegration =
  | 'Kubernetes';

/** Why a signal could not be judged at a target's last reading. */
export type MonitorReadError =
  /** The user switched the read off. */
  | 'Disabled'
  /** The reading broke the monitor's bounds. */
  | 'Invalid'
  /** The target is still loading. */
  | 'NotReady'
  /** The target refused the read. */
  | 'PermissionDenied'
  /** The target asked the reader to wait. */
  | 'RateLimited'
  /** The reading outlived its cadence. */
  | 'Timeout'
  /** The answer was cut at its bound. */
  | 'Truncated'
  /** The target refused the credentials. */
  | 'Unauthenticated'
  /** The target did not answer. */
  | 'Unavailable'
  /** The target cannot answer this signal at all. */
  | 'Unsupported';

/** What a target's last reading could judge. */
export type MonitorReadState =
  /** No signal could be judged; `unknownSignals` names each. */
  | 'Failed'
  /** No reading has committed yet. */
  | 'NeverRead'
  /** Every signal was judged. */
  | 'Ok'
  /** Some signals could not be judged; `unknownSignals` names them. */
  | 'Partial';

/** How loud a signal's incidents are. */
export type MonitorSeverity =
  | 'Critical'
  | 'Info'
  | 'Warning';

/** How much an action can do, its one access level. A rule may name `DataRead`, `Write`, `AccessControl` or `Destructive`; a `Write` rule that denies or asks covers `AccessControl` and `Destructive` too. `AccessControl` is a change to who may do what, or to what sees every request. */
export type PermissionAccessLevel =
  | 'AccessControl'
  | 'DataRead'
  | 'Destructive'
  | 'Reach'
  | 'Read'
  | 'Write';

/** What a rule does to an action it matches. */
export type PermissionEffect =
  | 'Allow'
  | 'Ask'
  | 'Deny';

/** An account's word on what runs unasked: `ReadOnly` refuses changes, `Ask` asks for every change, `Auto` runs writes and data reads unasked and asks for the rest. A data read asks under `ReadOnly` and `Ask`. */
export type PermissionMode =
  | 'Ask'
  | 'Auto'
  | 'ReadOnly';

/** Where an account's mode comes from: `Entry`, the account's own; `Default`, its target's default. */
export type PermissionModeSource =
  | 'Default'
  | 'Entry';

/** Who sent an action: a chat, a chat's subagent, the monitor, or the Sandbox console. */
export type PermissionPrincipal =
  | 'Chat'
  | 'Console'
  | 'Monitor'
  | 'Subagent';

/** A rule to add. A null `principal` or `target` is every one; a rule that allows names a target and an account by a literal first scope segment, and a Kubernetes one names a cluster Settings lists. `scope`, `service`, `operation` and `resource` are patterns, `*` by default. */
export type PermissionRuleInput = {
  accessLevel: PermissionAccessLevel;
  effect: PermissionEffect;
  operation?: string;
  principal?: PermissionPrincipal | null | undefined;
  resource?: string;
  scope?: string;
  service?: string;
  target?: PermissionTarget | null | undefined;
};

/** What an action reaches. */
export type PermissionTarget =
  | 'AWS'
  | 'Azure'
  | 'GCP'
  | 'GitHub'
  | 'Kubernetes';

/** Which credential signs a provider's requests. The highest one present does: Connector, then SavedKey, then EnvironmentKey. */
export type ProviderCredentialSource =
  /** The provider's connector, while it is connected. */
  | 'Connector'
  /** The key variable, from the launch or the login shell. */
  | 'EnvironmentKey'
  | 'None'
  /** The launch has not read the keyring yet, or the provider's connector has not. */
  | 'Restoring'
  /** A key saved from the Providers pane, in the OS keyring. */
  | 'SavedKey';

/** What is wrong with a provider's key. */
export type ProviderKeyFailure =
  /** The provider answered 401 to the key that signs. The key is kept and still used. Never reported while NotDeleted is. */
  | 'KeyRejected'
  /** The keyring would not delete the saved key. It is out of use now and returns after a restart; providerKeyDelete retries. */
  | 'NotDeleted';

/** Where a folder of the PATH setting came from. */
export type SandboxPathOrigin =
  /** The login shell's PATH, spliced in for $PATH. Searched only when every sandboxed command reads it already and no group but an administrators' one can add programs to it. */
  | 'Shell'
  /** A folder the user typed: their read grant, held to a folder grant's checks. */
  | 'Typed';

/** What a call does, from the tool that made it. Never read from the call's arguments. */
export type ToolActionKind =
  | 'Command'
  | 'Delegate'
  | 'Edit'
  | 'Fetch'
  | 'KubeQuery'
  | 'LogsRead'
  | 'LogsView'
  | 'LogsWatch'
  | 'Memory'
  | 'Read'
  | 'Search'
  | 'Stop'
  | 'Write';

/** Why a sandboxed call reached the internet: the chat's switch, the turn's toggle, or its own request the user approved. */
export type ToolCallNetwork =
  | 'Approved'
  | 'Chat'
  | 'Turn';

/** Who ran a tool call: the sidecar, on the user's machine, or the model's provider, on its own servers. */
export type ToolCallRunsOn =
  | 'Provider'
  | 'Sidecar';

/**
 * Where a tool call is. `NotRun` never started; `Interrupted` started and was cut off with no
 * answer from its tool (a cancel, the loop's deadline, a crash), so it may have run; `Failed` is a
 * tool's own error.
 */
export type ToolCallStatus =
  | 'AwaitingApproval'
  | 'Denied'
  | 'Failed'
  | 'Interrupted'
  | 'NotRun'
  | 'Running'
  | 'Succeeded';

/** What a tool does, as Settings groups the tools. */
export type ToolGroup =
  | 'Agents'
  | 'Cluster'
  | 'Commands'
  | 'Files'
  | 'Logs'
  | 'Memory'
  | 'Web';

export type ChatCancelMutationVariables = Exact<{
  chatID: string;
}>;


export type ChatCancelMutation = { chatCancel: boolean };

export type ChatDeleteMutationVariables = Exact<{
  id: string;
}>;


export type ChatDeleteMutation = { chatDelete: boolean };

export type ChatRenameMutationVariables = Exact<{
  id: string;
  title: string;
}>;


export type ChatRenameMutation = { chatRename: { id: string, title: string, updatedAt: string } };

export type ApprovalDecideMutationVariables = Exact<{
  id: string;
  decision: ApprovalDecision;
}>;


export type ApprovalDecideMutation = { approvalDecide: boolean };

export type BackgroundTaskStopMutationVariables = Exact<{
  id: string;
}>;


export type BackgroundTaskStopMutation = { backgroundTaskStop: boolean };

export type ClusterCachedKindSyncEnabledSetMutationVariables = Exact<{
  id: string;
  syncEnabled: boolean;
}>;


export type ClusterCachedKindSyncEnabledSetMutation = { clusterCachedKindSyncEnabledSet: { id: string, spec: { syncEnabled: boolean } } };

export type ClusterEventFeedConnectionSubscriptionVariables = Exact<{
  id: string;
}>;


export type ClusterEventFeedConnectionSubscription = { clusterEventFeed: { type: EventFeedFrameType, event: { id: string, type: EventType, reason: string, message: string, count: number, firstAt: string, lastAt: string } | null } };

export type ObjectEventFeedSyncSubscriptionVariables = Exact<{
  id: string;
}>;


export type ObjectEventFeedSyncSubscription = { objectEventFeed: { type: EventFeedFrameType, event: { id: string, type: EventType, reason: string, message: string, count: number, firstAt: string, lastAt: string } | null } };

export type ObjectEventFeedDiscoverySubscriptionVariables = Exact<{
  id: string;
}>;


export type ObjectEventFeedDiscoverySubscription = { objectEventFeed: { type: EventFeedFrameType, event: { id: string, type: EventType, reason: string, message: string, count: number, firstAt: string, lastAt: string } | null } };

export type ClusterCacheSyncStateWatchSubscriptionVariables = Exact<{
  id: string;
  cacheID: string;
}>;


export type ClusterCacheSyncStateWatchSubscription = { clusterCacheSyncStateWatch: { exists: boolean, discovery: { reason: string, message: string }, kinds: Array<{ apiVersion: string, resource: string, reason: string, message: string, objectCount: number }> } };

export type ClusterCachedKindCollectionWatchSubscriptionVariables = Exact<{
  cacheID: string;
}>;


export type ClusterCachedKindCollectionWatchSubscription = { clusterCachedKindCollectionWatch: { type: CollectionWatchFrameType, kind: { id: string, spec: { apiVersion: string, resource: string } } | null } };

export type ClusterConnectionRetryMutationVariables = Exact<{
  id: string;
}>;


export type ClusterConnectionRetryMutation = { clusterConnectionRetry: boolean };

export type AuthStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type AuthStateWatchSubscription = { authStateWatch: { restoring: boolean, authenticated: boolean, identity: { sub: string, email: string, name: string } | null } };

export type AuthLoginStartMutationVariables = Exact<{ [key: string]: never; }>;


export type AuthLoginStartMutation = { authLoginStart: boolean };

export type AuthLogoutMutationVariables = Exact<{ [key: string]: never; }>;


export type AuthLogoutMutation = { authLogout: boolean };

export type ChatPermissionRulesGetQueryVariables = Exact<{
  chatID: string;
}>;


export type ChatPermissionRulesGetQuery = { chatPermissionRulesGet: Array<{ id: string, line: string }> };

export type ChatPermissionRuleRemoveMutationVariables = Exact<{
  chatID: string;
  id: string;
}>;


export type ChatPermissionRuleRemoveMutation = { chatPermissionRuleRemove: Array<{ id: string }> };

export type ChatSendMutationVariables = Exact<{
  chatID?: string | null | undefined;
  mode: ChatMode;
  clusterID: string;
  sandboxDisabled: boolean;
  networkEnabled: boolean;
  networkThisTurn: boolean;
  sandboxDisabledThisTurn: boolean;
  providerID: string;
  modelID: string;
  effort: string;
  requestID: string;
  content: string;
  snapshot?: LogsViewSnapshotInput | null | undefined;
}>;


export type ChatSendMutation = { chatSend: { id: string, chatID: string, seq: number, status: ChatMessageStatus } };

export type ChatCollectionWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ChatCollectionWatchSubscription = { chatCollectionWatch: { type: CollectionWatchFrameType, chat: { id: string, title: string, mode: ChatMode, clusterID: string, createdAt: string, updatedAt: string, awaitingApproval: boolean, sandboxDisabled: boolean, networkEnabled: boolean } | null } };

export type ChatMessageCollectionWatchSubscriptionVariables = Exact<{
  chatID: string;
}>;


export type ChatMessageCollectionWatchSubscription = { chatMessageCollectionWatch: { type: CollectionWatchFrameType, message: { id: string, chatID: string, seq: number, role: ChatMessageRole, createdAt: string, finishedAt: string | null, content: unknown, thinking: string, status: ChatMessageStatus, awaitingApproval: boolean, error: string, failure: AnswerFailure | null, model: string, effort: string, finishReason: string, provider: { id: string, label: string } | null, toolCalls: Array<{ id: string, name: string, actionKind: ToolActionKind | null, status: ToolCallStatus | null, runsOn: ToolCallRunsOn, agentCallID: string | null, network: ToolCallNetwork | null, output: string, action: { description: string, command: { text: string, cwd: string, background: boolean, sandboxed: boolean, network: boolean, env: Array<string> } | null, read: { path: string } | null, write: { path: string, content: string } | null, edit: { path: string, oldString: string, newString: string, replaceAll: boolean } | null, search: { query: string } | null, fetch: { url: string, host: string } | null, memory: { op: string, name: string, body: string, scope: string } | null, delegate: { prompt: string, agentType: string, model: string } | null, kubeQuery: { sql: string, limit: number } | null, logsView: { grep: string, pinToEnd: boolean, resolvedAt: string, sources: Array<{ namespace: string, kind: LogsViewSourceKind, name: string, containers: Array<string>, allContainers: boolean, previous: boolean }>, filters: Array<{ field: LogsViewFilterField, values: Array<string> }>, anchor: { kind: LogsViewAnchorKind, at: string | null } } | null, logsRead: { grep: string, since: string | null, until: string | null, pageFrom: LogsPageFrom | null, resolvedAt: string, olderMore: boolean, newerMore: boolean, sources: Array<{ namespace: string, kind: LogsViewSourceKind, name: string, containers: Array<string>, allContainers: boolean, previous: boolean }>, filters: Array<{ field: LogsViewFilterField, values: Array<string> }>, before: { lines: number, span: string }, after: { lines: number, span: string }, lines: Array<{ timestamp: string, namespace: string, pod: string, container: string, previous: boolean, message: string, truncatedBytes: number, gap: { after: string, before: string } | null }>, notices: Array<{ code: string, message: string }>, viewAround: { grep: string, pinToEnd: boolean, resolvedAt: string, sources: Array<{ namespace: string, kind: LogsViewSourceKind, name: string, containers: Array<string>, allContainers: boolean, previous: boolean }>, filters: Array<{ field: LogsViewFilterField, values: Array<string> }>, anchor: { kind: LogsViewAnchorKind, at: string | null } } | null } | null, logsWatch: { grep: string, since: string | null, limit: number, maxBytes: number, maxMessageBytes: number, resolvedAt: string, until: string, sources: Array<{ namespace: string, kind: LogsViewSourceKind, name: string, containers: Array<string>, allContainers: boolean, previous: boolean }>, filters: Array<{ field: LogsViewFilterField, values: Array<string> }> } | null } | null, approval: { id: string, status: ApprovalStatus, duration: ApprovalDuration | null } | null, sandboxActions: Array<{ reason: string | null, approval: { id: string, status: ApprovalStatus, duration: ApprovalDuration | null }, action: { target: PermissionTarget, summary: string, accessLevel: PermissionAccessLevel, principal: PermissionPrincipal | null, scope: string, accountName: string, service: string, operation: string, resource: string, name: string, once: boolean, grantable: boolean, commandRule: string, chatRule: string } | null, request:
            | { __typename: 'CloudRequest', host: string, method: string, path: string, contentType: string, body: string, unshown: string, details: Array<{ label: string, value: string }> }
            | { __typename: 'ClusterWrite', method: string, path: string, subresource: string, contentType: string, body: string, dryRun: boolean, diff: string, diffCut: boolean, diffError: string }
           | null }>, background: { status: BackgroundTaskStatus, exitCode: number | null, report: string, endedBecause: string, matched: Array<{ timestamp: string, namespace: string, pod: string, container: string, previous: boolean, message: string, truncatedBytes: number, gap: { after: string, before: string } | null }>, viewAround: { grep: string, pinToEnd: boolean, resolvedAt: string, sources: Array<{ namespace: string, kind: LogsViewSourceKind, name: string, containers: Array<string>, allContainers: boolean, previous: boolean }>, filters: Array<{ field: LogsViewFilterField, values: Array<string> }>, anchor: { kind: LogsViewAnchorKind, at: string | null } } | null } | null }>, citations: Array<{ type: string, url: string, title: string, citedText: string }> } | null } };

export type ClusterCachedDataEventCollectionWatchSubscriptionVariables = Exact<{
  id: string;
  cacheID: string;
}>;


export type ClusterCachedDataEventCollectionWatchSubscription = { clusterCachedDataEventCollectionWatch: { type: CollectionWatchFrameType, cacheID: string, event: { uid: string, type: string, reason: string, message: string, count: number, firstSeen: string | null, lastSeen: string | null, involvedKind: string, involvedNamespace: string, involvedName: string } | null } };

export type ClusterCachedDataKindCollectionWatchSubscriptionVariables = Exact<{
  id: string;
  cacheID: string;
  namespace: string;
}>;


export type ClusterCachedDataKindCollectionWatchSubscription = { clusterCachedDataKindCollectionWatch: { type: CollectionWatchFrameType, cacheID: string, namespace: string, kind: { apiVersion: string, kind: string, resource: string, scope: string, isCRD: boolean, count: number, printerColumns: Array<{ name: string, type: string, jsonPath: string, priority: number }> } | null } };

export type ClusterCachedDataObjectCollectionWatchSubscriptionVariables = Exact<{
  id: string;
  cacheID: string;
  apiVersion: string;
  resource: string;
}>;


export type ClusterCachedDataObjectCollectionWatchSubscription = { clusterCachedDataObjectCollectionWatch: { type: CollectionWatchFrameType, cacheID: string, apiVersion: string, resource: string, object: { uid: string, apiVersion: string, kind: string, namespace: string, name: string, creationTimestamp: string | null, rawJSON: unknown } | null } };

export type ClusterCacheStatsStateWatchSubscriptionVariables = Exact<{
  id: string;
  cacheID: string;
}>;


export type ClusterCacheStatsStateWatchSubscription = { clusterCacheStatsStateWatch: { exists: boolean, bytes: number, dbBytes: number, walBytes: number, shmBytes: number, objectCount: number, kindCount: number } };

export type ClusterScheduleStateWatchSubscriptionVariables = Exact<{
  id: string;
}>;


export type ClusterScheduleStateWatchSubscription = { clusterScheduleStateWatch: { nextRequeueAt: string | null, probing: boolean, failures: number } };

export type ClusterDataRateSampleFeedSubscriptionVariables = Exact<{
  id: string;
}>;


export type ClusterDataRateSampleFeedSubscription = { clusterDataRateSampleFeed: { intervalSeconds: number, windowSeconds: number, samples: Array<{ at: string, receivedBytesPerSecond: number, sentBytesPerSecond: number }> } };

export type ClusterEnabledSetMutationVariables = Exact<{
  id: string;
  enabled: boolean;
}>;


export type ClusterEnabledSetMutation = { clusterEnabledSet: { id: string, spec: { enabled: boolean } } };

export type ClusterSyncEnabledSetMutationVariables = Exact<{
  id: string;
  syncEnabled: boolean;
}>;


export type ClusterSyncEnabledSetMutation = { clusterSyncEnabledSet: { id: string, spec: { syncEnabled: boolean } } };

export type ClusterDefaultSetMutationVariables = Exact<{
  id: string;
  isDefault: boolean;
}>;


export type ClusterDefaultSetMutation = { clusterDefaultSet: { id: string, spec: { isDefault: boolean } } };

export type ClusterCacheClearMutationVariables = Exact<{
  id: string;
}>;


export type ClusterCacheClearMutation = { clusterCacheClear: { id: string } };

export type ClusterDeleteMutationVariables = Exact<{
  id: string;
}>;


export type ClusterDeleteMutation = { clusterDelete: boolean };

export type ClusterCollectionWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ClusterCollectionWatchSubscription = { clusterCollectionWatch: { type: CollectionWatchFrameType, cluster: { id: string, deletionRequestedAt: string | null, spec: { name: string | null, syncEnabled: boolean, enabled: boolean, isDefault: boolean, source: { kubeconfig: { context: string } | null } }, status: { source: { kubeconfig: { isPresent: boolean, isDefault: boolean, cluster: { name: string, entry: { server: string, insecureSkipTLSVerify: boolean } | null }, user: { name: string } } | null }, server: { uid: string | null } }, conditions: Array<{ type: string, status: ConditionStatus, reason: string, message: string, liveness: boolean, unconfirmed: boolean, transitionedAt: string }> } | null } };

export type ClusterCacheCollectionWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ClusterCacheCollectionWatchSubscription = { clusterCacheCollectionWatch: { type: CollectionWatchFrameType, cache: { id: string, clusterID: string, spec: { serverUid: string } } | null } };

export type ClusterCacheHealthKeyedStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ClusterCacheHealthKeyedStateWatchSubscription = { clusterCacheHealthKeyedStateWatch: { cacheID: string, status: ConditionStatus, reason: string, failure: string, totalKinds: number, unhealthyKinds: number, pausedKinds: number, lastUpdateAt: string | null, lastLiveAt: string | null, unhealthyKindRefs: Array<{ apiVersion: string, resource: string, reason: string }> } };

export type ChatGptConnectionStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ChatGptConnectionStateWatchSubscription = { chatGPTConnectionStateWatch: { phase: ConnectorPhase, email: string | null, failure: ConnectorFailure | null, registered: boolean, revocationUnconfirmed: boolean } };

export type ChatGptConnectMutationVariables = Exact<{
  fresh: boolean;
}>;


export type ChatGptConnectMutation = { chatGPTConnect: boolean };

export type ChatGptConnectCancelMutationVariables = Exact<{ [key: string]: never; }>;


export type ChatGptConnectCancelMutation = { chatGPTConnectCancel: boolean };

export type ChatGptDisconnectMutationVariables = Exact<{ [key: string]: never; }>;


export type ChatGptDisconnectMutation = { chatGPTDisconnect: boolean };

export type ChatGptUsageOpenMutationVariables = Exact<{ [key: string]: never; }>;


export type ChatGptUsageOpenMutation = { chatGPTUsageOpen: boolean };

export type OpenRouterConnectionStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type OpenRouterConnectionStateWatchSubscription = { openRouterConnectionStateWatch: { phase: ConnectorPhase, failure: ConnectorFailure | null } };

export type OpenRouterConnectMutationVariables = Exact<{ [key: string]: never; }>;


export type OpenRouterConnectMutation = { openRouterConnect: boolean };

export type OpenRouterConnectCancelMutationVariables = Exact<{ [key: string]: never; }>;


export type OpenRouterConnectCancelMutation = { openRouterConnectCancel: boolean };

export type OpenRouterDisconnectMutationVariables = Exact<{ [key: string]: never; }>;


export type OpenRouterDisconnectMutation = { openRouterDisconnect: boolean };

export type OpenRouterKeysOpenMutationVariables = Exact<{ [key: string]: never; }>;


export type OpenRouterKeysOpenMutation = { openRouterKeysOpen: boolean };

export type AwsConnectionStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type AwsConnectionStateWatchSubscription = { awsConnectionStateWatch: { phase: ConnectorPhase, arn: string | null, failure: ConnectorFailure | null } };

export type AwsConnectMutationVariables = Exact<{ [key: string]: never; }>;


export type AwsConnectMutation = { awsConnect: boolean };

export type AwsConnectCancelMutationVariables = Exact<{ [key: string]: never; }>;


export type AwsConnectCancelMutation = { awsConnectCancel: boolean };

export type AwsDisconnectMutationVariables = Exact<{ [key: string]: never; }>;


export type AwsDisconnectMutation = { awsDisconnect: boolean };

export type GitHubConnectionStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type GitHubConnectionStateWatchSubscription = { gitHubConnectionStateWatch: { phase: ConnectorPhase, login: string | null, userCode: string | null, failure: ConnectorFailure | null } };

export type GitHubConnectMutationVariables = Exact<{ [key: string]: never; }>;


export type GitHubConnectMutation = { gitHubConnect: boolean };

export type GitHubConnectCancelMutationVariables = Exact<{ [key: string]: never; }>;


export type GitHubConnectCancelMutation = { gitHubConnectCancel: boolean };

export type GitHubDisconnectMutationVariables = Exact<{ [key: string]: never; }>;


export type GitHubDisconnectMutation = { gitHubDisconnect: boolean };

export type GitHubSettingsOpenMutationVariables = Exact<{ [key: string]: never; }>;


export type GitHubSettingsOpenMutation = { gitHubSettingsOpen: boolean };

export type GoogleConnectionStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type GoogleConnectionStateWatchSubscription = { googleConnectionStateWatch: { phase: ConnectorPhase, email: string | null, project: string | null, failure: ConnectorFailure | null, revocationUnconfirmed: boolean } };

export type GoogleConnectMutationVariables = Exact<{
  fresh: boolean;
}>;


export type GoogleConnectMutation = { googleConnect: boolean };

export type GoogleConnectCancelMutationVariables = Exact<{ [key: string]: never; }>;


export type GoogleConnectCancelMutation = { googleConnectCancel: boolean };

export type GoogleDisconnectMutationVariables = Exact<{ [key: string]: never; }>;


export type GoogleDisconnectMutation = { googleDisconnect: boolean };

export type EnvVarsGetQueryVariables = Exact<{
  clusterID?: string | null | undefined;
}>;


export type EnvVarsGetQuery = { envVarsGet: Array<{ name: string, value: string, target: EnvVarTarget, refusal: string | null }> };

export type EnvVarSetMutationVariables = Exact<{
  clusterID?: string | null | undefined;
  name: string;
  value: string;
  target: EnvVarTarget;
}>;


export type EnvVarSetMutation = { envVarSet: Array<{ name: string }> };

export type EnvVarRemoveMutationVariables = Exact<{
  clusterID?: string | null | undefined;
  name: string;
}>;


export type EnvVarRemoveMutation = { envVarRemove: Array<{ name: string }> };

export type MonitorTargetCollectionWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type MonitorTargetCollectionWatchSubscription = { monitorTargetCollectionWatch: { type: CollectionWatchFrameType, target: { id: string, integration: MonitorIntegration, label: string, clusterID: string | null, currentUID: string | null, enabled: boolean, readState: MonitorReadState, unknownSignals: Array<{ name: string, reason: MonitorReadError }> } | null } };

export type IncidentCollectionWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type IncidentCollectionWatchSubscription = { incidentCollectionWatch: { type: CollectionWatchFrameType, incident: { id: string, targetID: string, targetUID: string, subjectLabel: string, signal: string, signalLabel: string, severity: MonitorSeverity, firstSeenAt: string, open: boolean, occurrenceCount: number, lastStartedAt: string, lastSeenAt: string | null, lastEndReason: IncidentOccurrenceEndReason | null, baseline: boolean, subjectAttributes: Array<{ label: string, value: string }> } | null } };

export type MonitorSignalListQueryVariables = Exact<{ [key: string]: never; }>;


export type MonitorSignalListQuery = { monitorSignalList: Array<{ name: string, integration: MonitorIntegration, label: string, severity: MonitorSeverity }> };

export type IncidentOccurrenceEventFeedSubscriptionVariables = Exact<{
  incidentID: string;
}>;


export type IncidentOccurrenceEventFeedSubscription = { incidentOccurrenceEventFeed: { type: IncidentOccurrenceEventFeedFrameType, incidentID: string, occurrence: { id: string, incidentID: string, number: number, startedAt: string, endedAt: string | null, endReason: IncidentOccurrenceEndReason | null, detail: Array<{ label: string, value: string }> } | null } };

export type MonitorTargetEnabledSetMutationVariables = Exact<{
  id: string;
  enabled: boolean;
}>;


export type MonitorTargetEnabledSetMutation = { monitorTargetEnabledSet: { id: string, enabled: boolean } };

export type LogsFetchQueryVariables = Exact<{
  id: string;
  request: LogsFetchInput;
}>;


export type LogsFetchQuery = { logsFetch: { records: Array<{ timestamp: string, message: string, truncatedBytes: number, cursor: string, stream: { namespace: string, pod: string, container: string, previous: boolean }, place: { node: string, region: string, zone: string, os: string, arch: string }, gap: { after: string, before: string } | null }>, page: { prev: string | null, next: string | null, follow: string | null }, notices: Array<{ code: string, message: string }> } };

export type LogsTailSubscriptionVariables = Exact<{
  id: string;
  request: LogsTailInput;
}>;


export type LogsTailSubscription = { logsTail: { type: LogsTailFrameType, record: { timestamp: string, message: string, truncatedBytes: number, cursor: string, stream: { namespace: string, pod: string, container: string, previous: boolean }, place: { node: string, region: string, zone: string, os: string, arch: string }, gap: { after: string, before: string } | null } | null, notice: { code: string, message: string } | null, ended: { code: string, message: string } | null } };

export type MemoryCollectionWatchSubscriptionVariables = Exact<{
  clusterID: string;
}>;


export type MemoryCollectionWatchSubscription = { memoryCollectionWatch: { type: CollectionWatchFrameType, memory: { id: string, clusterID: string | null, name: string, body: string, writtenBy: MemoryAuthor, updatedAt: string } | null } };

export type MemorySaveMutationVariables = Exact<{
  input: MemorySaveInput;
}>;


export type MemorySaveMutation = { memorySave: { id: string } };

export type MemoryDeleteMutationVariables = Exact<{
  id: string;
}>;


export type MemoryDeleteMutation = { memoryDelete: boolean };

export type ModelsStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ModelsStateWatchSubscription = { modelsStateWatch: Array<{ id: string, label: string, efforts: Array<string>, defaultEffort: string, provider: { id: string, label: string } }> };

export type ChatNetworkEnabledSetMutationVariables = Exact<{
  id: string;
  enabled: boolean;
}>;


export type ChatNetworkEnabledSetMutation = { chatNetworkEnabledSet: { id: string, networkEnabled: boolean } };

export type PermissionSettingsGetQueryVariables = Exact<{ [key: string]: never; }>;


export type PermissionSettingsGetQuery = { permissionSettingsGet: { alwaysAsks: Array<string>, accessControl: Array<string>, targets: Array<{ target: PermissionTarget, defaultMode: PermissionMode, accounts: Array<{ account: string, name: string, clusterID: string | null, mode: PermissionMode, source: PermissionModeSource }> }>, rules: Array<{ id: string, effect: PermissionEffect, accessLevel: PermissionAccessLevel, principal: PermissionPrincipal | null, target: PermissionTarget | null, scope: string, service: string, operation: string, resource: string, line: string }> } };

export type PermissionDefaultModeSetMutationVariables = Exact<{
  target: PermissionTarget;
  mode: PermissionMode;
}>;


export type PermissionDefaultModeSetMutation = { permissionDefaultModeSet: { alwaysAsks: Array<string> } };

export type PermissionModeSetMutationVariables = Exact<{
  target: PermissionTarget;
  account: string;
  mode: PermissionMode;
}>;


export type PermissionModeSetMutation = { permissionModeSet: { alwaysAsks: Array<string> } };

export type PermissionModeClearMutationVariables = Exact<{
  target: PermissionTarget;
  account: string;
}>;


export type PermissionModeClearMutation = { permissionModeClear: { alwaysAsks: Array<string> } };

export type PermissionRuleAddMutationVariables = Exact<{
  input: PermissionRuleInput;
}>;


export type PermissionRuleAddMutation = { permissionRuleAdd: { alwaysAsks: Array<string> } };

export type PermissionRuleUpdateMutationVariables = Exact<{
  id: string;
  input: PermissionRuleInput;
}>;


export type PermissionRuleUpdateMutation = { permissionRuleUpdate: { alwaysAsks: Array<string> } };

export type PermissionRuleRemoveMutationVariables = Exact<{
  id: string;
}>;


export type PermissionRuleRemoveMutation = { permissionRuleRemove: { alwaysAsks: Array<string> } };

export type ProviderCredentialsStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ProviderCredentialsStateWatchSubscription = { providerCredentialsStateWatch: Array<{ id: string, label: string, keyVariable: string | null, source: ProviderCredentialSource, hasEnvironmentKey: boolean, hasSavedKey: boolean, failure: ProviderKeyFailure | null }> };

export type ProviderKeySaveMutationVariables = Exact<{
  providerID: string;
  key: string;
}>;


export type ProviderKeySaveMutation = { providerKeySave: boolean };

export type ProviderKeyDeleteMutationVariables = Exact<{
  providerID: string;
}>;


export type ProviderKeyDeleteMutation = { providerKeyDelete: boolean };

export type SandboxCloudAccessGetQueryVariables = Exact<{ [key: string]: never; }>;


export type SandboxCloudAccessGetQuery = { sandboxCloudAccessGet: Array<{ target: PermissionTarget, on: boolean }> };

export type SandboxConsoleStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type SandboxConsoleStateWatchSubscription = { sandboxConsoleStateWatch: { run: { number: number, command: string, clusterID: string | null, network: boolean, running: boolean, output: string, discarded: number, exitCode: number | null, notice: string | null, durationMs: number } | null } };

export type SandboxConsoleRunMutationVariables = Exact<{
  command: string;
  clusterID?: string | null | undefined;
  network: boolean;
}>;


export type SandboxConsoleRunMutation = { sandboxConsoleRun: number };

export type SandboxConsoleStopMutationVariables = Exact<{
  number?: number | null | undefined;
}>;


export type SandboxConsoleStopMutation = { sandboxConsoleStop: boolean };

export type SandboxDefaultStateWatchSubscriptionVariables = Exact<{
  clusterID?: string | null | undefined;
}>;


export type SandboxDefaultStateWatchSubscription = { sandboxDefaultStateWatch: { sandboxDisabled: boolean, clusterOverride: boolean } };

export type SandboxDefaultSetMutationVariables = Exact<{
  clusterID?: string | null | undefined;
  sandboxDisabled: boolean;
}>;


export type SandboxDefaultSetMutation = { sandboxDefaultSet: { sandboxDisabled: boolean, clusterOverride: boolean } };

export type SandboxDefaultResetMutationVariables = Exact<{
  clusterID: string;
  sandboxDisabled: boolean;
}>;


export type SandboxDefaultResetMutation = { sandboxDefaultReset: { sandboxDisabled: boolean, clusterOverride: boolean } };

export type SandboxExecutablesStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type SandboxExecutablesStateWatchSubscription = { sandboxExecutablesStateWatch: { probing: boolean, probes: number, executables: Array<{ name: string, invocation: string, registered: boolean, probed: boolean, resolved: string, shim: boolean, target: string, ok: boolean, version: string, error: string, refusal: string | null }> } };

export type SandboxExecutablesProbeMutationVariables = Exact<{ [key: string]: never; }>;


export type SandboxExecutablesProbeMutation = { sandboxExecutablesProbe: Array<{ name: string }> };

export type SandboxExecutableRegisterMutationVariables = Exact<{
  name: string;
  invocation?: string | null | undefined;
}>;


export type SandboxExecutableRegisterMutation = { sandboxExecutableRegister: Array<{ name: string }> };

export type SandboxExecutableRemoveMutationVariables = Exact<{
  name: string;
}>;


export type SandboxExecutableRemoveMutation = { sandboxExecutableRemove: Array<{ name: string }> };

export type SandboxFoldersStateWatchSubscriptionVariables = Exact<{
  chatID?: string | null | undefined;
}>;


export type SandboxFoldersStateWatchSubscription = { sandboxFoldersStateWatch: { never: Array<string>, closed: Array<string>, wide: Array<string>, always: Array<{ id: string, path: string, write: boolean, refusal: string | null }>, chat: Array<{ id: string, path: string, write: boolean, refusal: string | null }> } };

export type FolderGrantMutationVariables = Exact<{
  chatID?: string | null | undefined;
  path: string;
  write: boolean;
  duration: GrantDuration;
}>;


export type FolderGrantMutation = { folderGrant: { wide: Array<string> } };

export type FolderRevokeMutationVariables = Exact<{
  chatID?: string | null | undefined;
  id: string;
  duration: GrantDuration;
}>;


export type FolderRevokeMutation = { folderRevoke: { wide: Array<string> } };

export type SandboxPathGetQueryVariables = Exact<{ [key: string]: never; }>;


export type SandboxPathGetQuery = { sandboxPathFaultGet: string | null, sandboxPathGet: { value: string, refusal: string | null, folders: Array<{ path: string, origin: SandboxPathOrigin, target: string | null, searched: boolean, reason: string | null, addable: boolean, shared: boolean }> } };

export type SandboxPathSetMutationVariables = Exact<{
  value: string;
}>;


export type SandboxPathSetMutation = { sandboxPathSet: { value: string } };

export type SandboxPathRefreshMutationVariables = Exact<{ [key: string]: never; }>;


export type SandboxPathRefreshMutation = { sandboxPathRefresh: { value: string } };

export type ChatSandboxDisabledSetMutationVariables = Exact<{
  id: string;
  sandboxDisabled: boolean;
}>;


export type ChatSandboxDisabledSetMutation = { chatSandboxDisabledSet: { id: string, sandboxDisabled: boolean } };

export type SandboxStateGetQueryVariables = Exact<{ [key: string]: never; }>;


export type SandboxStateGetQuery = { sandboxStateGet: { available: boolean, reason: string, networkAvailable: boolean, networkReason: string } };

export type ToolSettingsStateWatchSubscriptionVariables = Exact<{ [key: string]: never; }>;


export type ToolSettingsStateWatchSubscription = { toolSettingsStateWatch: Array<{ name: string, label: string, group: ToolGroup, description: string, enabled: boolean, alwaysOn: boolean }> };

export type ToolSettingSetMutationVariables = Exact<{
  name: string;
  enabled: boolean;
}>;


export type ToolSettingSetMutation = { toolSettingSet: Array<{ name: string, enabled: boolean }> };


export const ChatCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatCancel"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatCancel"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}}]}]}}]} as unknown as DocumentNode<ChatCancelMutation, ChatCancelMutationVariables>;
export const ChatDeleteDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatDelete"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatDelete"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<ChatDeleteMutation, ChatDeleteMutationVariables>;
export const ChatRenameDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatRename"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"title"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatRename"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"title"},"value":{"kind":"Variable","name":{"kind":"Name","value":"title"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}}]}}]}}]} as unknown as DocumentNode<ChatRenameMutation, ChatRenameMutationVariables>;
export const ApprovalDecideDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ApprovalDecide"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ApprovalID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"decision"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ApprovalDecision"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"approvalDecide"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"decision"},"value":{"kind":"Variable","name":{"kind":"Name","value":"decision"}}}]}]}}]} as unknown as DocumentNode<ApprovalDecideMutation, ApprovalDecideMutationVariables>;
export const BackgroundTaskStopDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"BackgroundTaskStop"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ToolCallID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"backgroundTaskStop"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<BackgroundTaskStopMutation, BackgroundTaskStopMutationVariables>;
export const ClusterCachedKindSyncEnabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterCachedKindSyncEnabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"syncEnabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCachedKindSyncEnabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"syncEnabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"syncEnabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"syncEnabled"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCachedKindSyncEnabledSetMutation, ClusterCachedKindSyncEnabledSetMutationVariables>;
export const ClusterEventFeedConnectionDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterEventFeedConnection"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterEventFeed"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"category"},"value":{"kind":"StringValue","value":"connection","block":false}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"event"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"count"}},{"kind":"Field","name":{"kind":"Name","value":"firstAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastAt"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterEventFeedConnectionSubscription, ClusterEventFeedConnectionSubscriptionVariables>;
export const ObjectEventFeedSyncDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ObjectEventFeedSync"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"objectEventFeed"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"category"},"value":{"kind":"StringValue","value":"sync","block":false}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"event"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"count"}},{"kind":"Field","name":{"kind":"Name","value":"firstAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastAt"}}]}}]}}]}}]} as unknown as DocumentNode<ObjectEventFeedSyncSubscription, ObjectEventFeedSyncSubscriptionVariables>;
export const ObjectEventFeedDiscoveryDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ObjectEventFeedDiscovery"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"objectEventFeed"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"category"},"value":{"kind":"StringValue","value":"discovery","block":false}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"event"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"count"}},{"kind":"Field","name":{"kind":"Name","value":"firstAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastAt"}}]}}]}}]}}]} as unknown as DocumentNode<ObjectEventFeedDiscoverySubscription, ObjectEventFeedDiscoverySubscriptionVariables>;
export const ClusterCacheSyncStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCacheSyncStateWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCacheSyncStateWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"exists"}},{"kind":"Field","name":{"kind":"Name","value":"discovery"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}}]}},{"kind":"Field","name":{"kind":"Name","value":"kinds"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"objectCount"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCacheSyncStateWatchSubscription, ClusterCacheSyncStateWatchSubscriptionVariables>;
export const ClusterCachedKindCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCachedKindCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCachedKindCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"kind"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}}]}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCachedKindCollectionWatchSubscription, ClusterCachedKindCollectionWatchSubscriptionVariables>;
export const ClusterConnectionRetryDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterConnectionRetry"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterConnectionRetry"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<ClusterConnectionRetryMutation, ClusterConnectionRetryMutationVariables>;
export const AuthStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"AuthStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"authStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"restoring"}},{"kind":"Field","name":{"kind":"Name","value":"authenticated"}},{"kind":"Field","name":{"kind":"Name","value":"identity"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sub"}},{"kind":"Field","name":{"kind":"Name","value":"email"}},{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]}}]} as unknown as DocumentNode<AuthStateWatchSubscription, AuthStateWatchSubscriptionVariables>;
export const AuthLoginStartDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"AuthLoginStart"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"authLoginStart"}}]}}]} as unknown as DocumentNode<AuthLoginStartMutation, AuthLoginStartMutationVariables>;
export const AuthLogoutDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"AuthLogout"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"authLogout"}}]}}]} as unknown as DocumentNode<AuthLogoutMutation, AuthLogoutMutationVariables>;
export const ChatPermissionRulesGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"ChatPermissionRulesGet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatPermissionRulesGet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"line"}}]}}]}}]} as unknown as DocumentNode<ChatPermissionRulesGetQuery, ChatPermissionRulesGetQueryVariables>;
export const ChatPermissionRuleRemoveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatPermissionRuleRemove"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatPermissionRuleRemove"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}},{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}}]}}]}}]} as unknown as DocumentNode<ChatPermissionRuleRemoveMutation, ChatPermissionRuleRemoveMutationVariables>;
export const ChatSendDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatSend"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"mode"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatMode"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"networkEnabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"networkThisTurn"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabledThisTurn"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"modelID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"effort"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"requestID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"content"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"snapshot"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"LogsViewSnapshotInput"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatSend"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}},{"kind":"Argument","name":{"kind":"Name","value":"mode"},"value":{"kind":"Variable","name":{"kind":"Name","value":"mode"}}},{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"sandboxDisabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}}},{"kind":"Argument","name":{"kind":"Name","value":"networkEnabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"networkEnabled"}}},{"kind":"Argument","name":{"kind":"Name","value":"networkThisTurn"},"value":{"kind":"Variable","name":{"kind":"Name","value":"networkThisTurn"}}},{"kind":"Argument","name":{"kind":"Name","value":"sandboxDisabledThisTurn"},"value":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabledThisTurn"}}},{"kind":"Argument","name":{"kind":"Name","value":"providerID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}}},{"kind":"Argument","name":{"kind":"Name","value":"modelID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"modelID"}}},{"kind":"Argument","name":{"kind":"Name","value":"effort"},"value":{"kind":"Variable","name":{"kind":"Name","value":"effort"}}},{"kind":"Argument","name":{"kind":"Name","value":"requestID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"requestID"}}},{"kind":"Argument","name":{"kind":"Name","value":"content"},"value":{"kind":"Variable","name":{"kind":"Name","value":"content"}}},{"kind":"Argument","name":{"kind":"Name","value":"snapshot"},"value":{"kind":"Variable","name":{"kind":"Name","value":"snapshot"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"chatID"}},{"kind":"Field","name":{"kind":"Name","value":"seq"}},{"kind":"Field","name":{"kind":"Name","value":"status"}}]}}]}}]} as unknown as DocumentNode<ChatSendMutation, ChatSendMutationVariables>;
export const ChatCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ChatCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"chat"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"mode"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"createdAt"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}},{"kind":"Field","name":{"kind":"Name","value":"awaitingApproval"}},{"kind":"Field","name":{"kind":"Name","value":"sandboxDisabled"}},{"kind":"Field","name":{"kind":"Name","value":"networkEnabled"}}]}}]}}]}}]} as unknown as DocumentNode<ChatCollectionWatchSubscription, ChatCollectionWatchSubscriptionVariables>;
export const ChatMessageCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ChatMessageCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatMessageCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"message"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"chatID"}},{"kind":"Field","name":{"kind":"Name","value":"seq"}},{"kind":"Field","name":{"kind":"Name","value":"role"}},{"kind":"Field","name":{"kind":"Name","value":"createdAt"}},{"kind":"Field","name":{"kind":"Name","value":"finishedAt"}},{"kind":"Field","name":{"kind":"Name","value":"content"}},{"kind":"Field","name":{"kind":"Name","value":"thinking"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"awaitingApproval"}},{"kind":"Field","name":{"kind":"Name","value":"error"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}},{"kind":"Field","name":{"kind":"Name","value":"model"}},{"kind":"Field","name":{"kind":"Name","value":"provider"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"label"}}]}},{"kind":"Field","name":{"kind":"Name","value":"effort"}},{"kind":"Field","name":{"kind":"Name","value":"finishReason"}},{"kind":"Field","name":{"kind":"Name","value":"toolCalls"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"actionKind"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"runsOn"}},{"kind":"Field","name":{"kind":"Name","value":"action"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"description"}},{"kind":"Field","name":{"kind":"Name","value":"command"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"text"}},{"kind":"Field","name":{"kind":"Name","value":"cwd"}},{"kind":"Field","name":{"kind":"Name","value":"background"}},{"kind":"Field","name":{"kind":"Name","value":"sandboxed"}},{"kind":"Field","name":{"kind":"Name","value":"network"}},{"kind":"Field","name":{"kind":"Name","value":"env"}}]}},{"kind":"Field","name":{"kind":"Name","value":"read"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"path"}}]}},{"kind":"Field","name":{"kind":"Name","value":"write"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"content"}}]}},{"kind":"Field","name":{"kind":"Name","value":"edit"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"oldString"}},{"kind":"Field","name":{"kind":"Name","value":"newString"}},{"kind":"Field","name":{"kind":"Name","value":"replaceAll"}}]}},{"kind":"Field","name":{"kind":"Name","value":"search"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"query"}}]}},{"kind":"Field","name":{"kind":"Name","value":"fetch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"url"}},{"kind":"Field","name":{"kind":"Name","value":"host"}}]}},{"kind":"Field","name":{"kind":"Name","value":"memory"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"op"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"scope"}}]}},{"kind":"Field","name":{"kind":"Name","value":"delegate"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"prompt"}},{"kind":"Field","name":{"kind":"Name","value":"agentType"}},{"kind":"Field","name":{"kind":"Name","value":"model"}}]}},{"kind":"Field","name":{"kind":"Name","value":"kubeQuery"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sql"}},{"kind":"Field","name":{"kind":"Name","value":"limit"}}]}},{"kind":"Field","name":{"kind":"Name","value":"logsView"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sources"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"containers"}},{"kind":"Field","name":{"kind":"Name","value":"allContainers"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"filters"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"field"}},{"kind":"Field","name":{"kind":"Name","value":"values"}}]}},{"kind":"Field","name":{"kind":"Name","value":"grep"}},{"kind":"Field","name":{"kind":"Name","value":"anchor"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"at"}}]}},{"kind":"Field","name":{"kind":"Name","value":"pinToEnd"}},{"kind":"Field","name":{"kind":"Name","value":"resolvedAt"}}]}},{"kind":"Field","name":{"kind":"Name","value":"logsRead"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sources"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"containers"}},{"kind":"Field","name":{"kind":"Name","value":"allContainers"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"filters"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"field"}},{"kind":"Field","name":{"kind":"Name","value":"values"}}]}},{"kind":"Field","name":{"kind":"Name","value":"grep"}},{"kind":"Field","name":{"kind":"Name","value":"since"}},{"kind":"Field","name":{"kind":"Name","value":"until"}},{"kind":"Field","name":{"kind":"Name","value":"before"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"lines"}},{"kind":"Field","name":{"kind":"Name","value":"span"}}]}},{"kind":"Field","name":{"kind":"Name","value":"after"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"lines"}},{"kind":"Field","name":{"kind":"Name","value":"span"}}]}},{"kind":"Field","name":{"kind":"Name","value":"pageFrom"}},{"kind":"Field","name":{"kind":"Name","value":"resolvedAt"}},{"kind":"Field","name":{"kind":"Name","value":"lines"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"timestamp"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"pod"}},{"kind":"Field","name":{"kind":"Name","value":"container"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"gap"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"after"}},{"kind":"Field","name":{"kind":"Name","value":"before"}}]}},{"kind":"Field","name":{"kind":"Name","value":"truncatedBytes"}}]}},{"kind":"Field","name":{"kind":"Name","value":"olderMore"}},{"kind":"Field","name":{"kind":"Name","value":"newerMore"}},{"kind":"Field","name":{"kind":"Name","value":"notices"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"code"}},{"kind":"Field","name":{"kind":"Name","value":"message"}}]}},{"kind":"Field","name":{"kind":"Name","value":"viewAround"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sources"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"containers"}},{"kind":"Field","name":{"kind":"Name","value":"allContainers"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"filters"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"field"}},{"kind":"Field","name":{"kind":"Name","value":"values"}}]}},{"kind":"Field","name":{"kind":"Name","value":"grep"}},{"kind":"Field","name":{"kind":"Name","value":"anchor"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"at"}}]}},{"kind":"Field","name":{"kind":"Name","value":"pinToEnd"}},{"kind":"Field","name":{"kind":"Name","value":"resolvedAt"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"logsWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sources"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"containers"}},{"kind":"Field","name":{"kind":"Name","value":"allContainers"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"filters"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"field"}},{"kind":"Field","name":{"kind":"Name","value":"values"}}]}},{"kind":"Field","name":{"kind":"Name","value":"grep"}},{"kind":"Field","name":{"kind":"Name","value":"since"}},{"kind":"Field","name":{"kind":"Name","value":"limit"}},{"kind":"Field","name":{"kind":"Name","value":"maxBytes"}},{"kind":"Field","name":{"kind":"Name","value":"maxMessageBytes"}},{"kind":"Field","name":{"kind":"Name","value":"resolvedAt"}},{"kind":"Field","name":{"kind":"Name","value":"until"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"agentCallID"}},{"kind":"Field","name":{"kind":"Name","value":"approval"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"duration"}}]}},{"kind":"Field","name":{"kind":"Name","value":"network"}},{"kind":"Field","name":{"kind":"Name","value":"sandboxActions"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"approval"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"duration"}}]}},{"kind":"Field","name":{"kind":"Name","value":"action"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"summary"}},{"kind":"Field","name":{"kind":"Name","value":"accessLevel"}},{"kind":"Field","name":{"kind":"Name","value":"principal"}},{"kind":"Field","name":{"kind":"Name","value":"scope"}},{"kind":"Field","name":{"kind":"Name","value":"accountName"}},{"kind":"Field","name":{"kind":"Name","value":"service"}},{"kind":"Field","name":{"kind":"Name","value":"operation"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"once"}},{"kind":"Field","name":{"kind":"Name","value":"grantable"}},{"kind":"Field","name":{"kind":"Name","value":"commandRule"}},{"kind":"Field","name":{"kind":"Name","value":"chatRule"}}]}},{"kind":"Field","name":{"kind":"Name","value":"request"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"__typename"}},{"kind":"InlineFragment","typeCondition":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterWrite"}},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"method"}},{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"subresource"}},{"kind":"Field","name":{"kind":"Name","value":"contentType"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"dryRun"}},{"kind":"Field","name":{"kind":"Name","value":"diff"}},{"kind":"Field","name":{"kind":"Name","value":"diffCut"}},{"kind":"Field","name":{"kind":"Name","value":"diffError"}}]}},{"kind":"InlineFragment","typeCondition":{"kind":"NamedType","name":{"kind":"Name","value":"CloudRequest"}},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"host"}},{"kind":"Field","name":{"kind":"Name","value":"method"}},{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"contentType"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"unshown"}},{"kind":"Field","name":{"kind":"Name","value":"details"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"value"}}]}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"reason"}}]}},{"kind":"Field","name":{"kind":"Name","value":"output"}},{"kind":"Field","name":{"kind":"Name","value":"background"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"exitCode"}},{"kind":"Field","name":{"kind":"Name","value":"report"}},{"kind":"Field","name":{"kind":"Name","value":"matched"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"timestamp"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"pod"}},{"kind":"Field","name":{"kind":"Name","value":"container"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"gap"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"after"}},{"kind":"Field","name":{"kind":"Name","value":"before"}}]}},{"kind":"Field","name":{"kind":"Name","value":"truncatedBytes"}}]}},{"kind":"Field","name":{"kind":"Name","value":"endedBecause"}},{"kind":"Field","name":{"kind":"Name","value":"viewAround"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sources"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"containers"}},{"kind":"Field","name":{"kind":"Name","value":"allContainers"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"filters"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"field"}},{"kind":"Field","name":{"kind":"Name","value":"values"}}]}},{"kind":"Field","name":{"kind":"Name","value":"grep"}},{"kind":"Field","name":{"kind":"Name","value":"anchor"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"at"}}]}},{"kind":"Field","name":{"kind":"Name","value":"pinToEnd"}},{"kind":"Field","name":{"kind":"Name","value":"resolvedAt"}}]}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"citations"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"url"}},{"kind":"Field","name":{"kind":"Name","value":"title"}},{"kind":"Field","name":{"kind":"Name","value":"citedText"}}]}}]}}]}}]}}]} as unknown as DocumentNode<ChatMessageCollectionWatchSubscription, ChatMessageCollectionWatchSubscriptionVariables>;
export const ClusterCachedDataEventCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCachedDataEventCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCachedDataEventCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"cacheID"}},{"kind":"Field","name":{"kind":"Name","value":"event"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"uid"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"count"}},{"kind":"Field","name":{"kind":"Name","value":"firstSeen"}},{"kind":"Field","name":{"kind":"Name","value":"lastSeen"}},{"kind":"Field","name":{"kind":"Name","value":"involvedKind"}},{"kind":"Field","name":{"kind":"Name","value":"involvedNamespace"}},{"kind":"Field","name":{"kind":"Name","value":"involvedName"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCachedDataEventCollectionWatchSubscription, ClusterCachedDataEventCollectionWatchSubscriptionVariables>;
export const ClusterCachedDataKindCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCachedDataKindCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCachedDataKindCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}},{"kind":"Argument","name":{"kind":"Name","value":"namespace"},"value":{"kind":"Variable","name":{"kind":"Name","value":"namespace"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"cacheID"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"kind"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"scope"}},{"kind":"Field","name":{"kind":"Name","value":"isCRD"}},{"kind":"Field","name":{"kind":"Name","value":"count"}},{"kind":"Field","name":{"kind":"Name","value":"printerColumns"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"jsonPath"}},{"kind":"Field","name":{"kind":"Name","value":"priority"}}]}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCachedDataKindCollectionWatchSubscription, ClusterCachedDataKindCollectionWatchSubscriptionVariables>;
export const ClusterCachedDataObjectCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCachedDataObjectCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"apiVersion"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"resource"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCachedDataObjectCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}},{"kind":"Argument","name":{"kind":"Name","value":"apiVersion"},"value":{"kind":"Variable","name":{"kind":"Name","value":"apiVersion"}}},{"kind":"Argument","name":{"kind":"Name","value":"resource"},"value":{"kind":"Variable","name":{"kind":"Name","value":"resource"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"cacheID"}},{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"object"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"uid"}},{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"kind"}},{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"creationTimestamp"}},{"kind":"Field","name":{"kind":"Name","value":"rawJSON"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCachedDataObjectCollectionWatchSubscription, ClusterCachedDataObjectCollectionWatchSubscriptionVariables>;
export const ClusterCacheStatsStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCacheStatsStateWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCacheStatsStateWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"cacheID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"cacheID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"exists"}},{"kind":"Field","name":{"kind":"Name","value":"bytes"}},{"kind":"Field","name":{"kind":"Name","value":"dbBytes"}},{"kind":"Field","name":{"kind":"Name","value":"walBytes"}},{"kind":"Field","name":{"kind":"Name","value":"shmBytes"}},{"kind":"Field","name":{"kind":"Name","value":"objectCount"}},{"kind":"Field","name":{"kind":"Name","value":"kindCount"}}]}}]}}]} as unknown as DocumentNode<ClusterCacheStatsStateWatchSubscription, ClusterCacheStatsStateWatchSubscriptionVariables>;
export const ClusterScheduleStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterScheduleStateWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterScheduleStateWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"nextRequeueAt"}},{"kind":"Field","name":{"kind":"Name","value":"probing"}},{"kind":"Field","name":{"kind":"Name","value":"failures"}}]}}]}}]} as unknown as DocumentNode<ClusterScheduleStateWatchSubscription, ClusterScheduleStateWatchSubscriptionVariables>;
export const ClusterDataRateSampleFeedDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterDataRateSampleFeed"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterDataRateSampleFeed"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"intervalSeconds"}},{"kind":"Field","name":{"kind":"Name","value":"windowSeconds"}},{"kind":"Field","name":{"kind":"Name","value":"samples"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"at"}},{"kind":"Field","name":{"kind":"Name","value":"receivedBytesPerSecond"}},{"kind":"Field","name":{"kind":"Name","value":"sentBytesPerSecond"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterDataRateSampleFeedSubscription, ClusterDataRateSampleFeedSubscriptionVariables>;
export const ClusterEnabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterEnabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterEnabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"enabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"enabled"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterEnabledSetMutation, ClusterEnabledSetMutationVariables>;
export const ClusterSyncEnabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterSyncEnabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"syncEnabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterSyncEnabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"syncEnabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"syncEnabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"syncEnabled"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterSyncEnabledSetMutation, ClusterSyncEnabledSetMutationVariables>;
export const ClusterDefaultSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterDefaultSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"isDefault"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterDefaultSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"isDefault"},"value":{"kind":"Variable","name":{"kind":"Name","value":"isDefault"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"isDefault"}}]}}]}}]}}]} as unknown as DocumentNode<ClusterDefaultSetMutation, ClusterDefaultSetMutationVariables>;
export const ClusterCacheClearDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterCacheClear"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ObjectID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCacheClear"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}}]}}]}}]} as unknown as DocumentNode<ClusterCacheClearMutation, ClusterCacheClearMutationVariables>;
export const ClusterDeleteDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ClusterDelete"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterDelete"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<ClusterDeleteMutation, ClusterDeleteMutationVariables>;
export const ClusterCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"cluster"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"deletionRequestedAt"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"syncEnabled"}},{"kind":"Field","name":{"kind":"Name","value":"enabled"}},{"kind":"Field","name":{"kind":"Name","value":"isDefault"}},{"kind":"Field","name":{"kind":"Name","value":"source"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"kubeconfig"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"context"}}]}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"status"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"source"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"kubeconfig"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"cluster"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"entry"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"server"}},{"kind":"Field","name":{"kind":"Name","value":"insecureSkipTLSVerify"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"user"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}},{"kind":"Field","name":{"kind":"Name","value":"isPresent"}},{"kind":"Field","name":{"kind":"Name","value":"isDefault"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"server"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"uid"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"conditions"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"liveness"}},{"kind":"Field","name":{"kind":"Name","value":"unconfirmed"}},{"kind":"Field","name":{"kind":"Name","value":"transitionedAt"}}]}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCollectionWatchSubscription, ClusterCollectionWatchSubscriptionVariables>;
export const ClusterCacheCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCacheCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCacheCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"cache"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"spec"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"serverUid"}}]}}]}}]}}]}}]} as unknown as DocumentNode<ClusterCacheCollectionWatchSubscription, ClusterCacheCollectionWatchSubscriptionVariables>;
export const ClusterCacheHealthKeyedStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ClusterCacheHealthKeyedStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"clusterCacheHealthKeyedStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"cacheID"}},{"kind":"Field","name":{"kind":"Name","value":"status"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"unhealthyKindRefs"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"apiVersion"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}}]}},{"kind":"Field","name":{"kind":"Name","value":"failure"}},{"kind":"Field","name":{"kind":"Name","value":"totalKinds"}},{"kind":"Field","name":{"kind":"Name","value":"unhealthyKinds"}},{"kind":"Field","name":{"kind":"Name","value":"pausedKinds"}},{"kind":"Field","name":{"kind":"Name","value":"lastUpdateAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastLiveAt"}}]}}]}}]} as unknown as DocumentNode<ClusterCacheHealthKeyedStateWatchSubscription, ClusterCacheHealthKeyedStateWatchSubscriptionVariables>;
export const ChatGptConnectionStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ChatGPTConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatGPTConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"phase"}},{"kind":"Field","name":{"kind":"Name","value":"email"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}},{"kind":"Field","name":{"kind":"Name","value":"registered"}},{"kind":"Field","name":{"kind":"Name","value":"revocationUnconfirmed"}}]}}]}}]} as unknown as DocumentNode<ChatGptConnectionStateWatchSubscription, ChatGptConnectionStateWatchSubscriptionVariables>;
export const ChatGptConnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatGPTConnect"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"fresh"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatGPTConnect"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"fresh"},"value":{"kind":"Variable","name":{"kind":"Name","value":"fresh"}}}]}]}}]} as unknown as DocumentNode<ChatGptConnectMutation, ChatGptConnectMutationVariables>;
export const ChatGptConnectCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatGPTConnectCancel"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatGPTConnectCancel"}}]}}]} as unknown as DocumentNode<ChatGptConnectCancelMutation, ChatGptConnectCancelMutationVariables>;
export const ChatGptDisconnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatGPTDisconnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatGPTDisconnect"}}]}}]} as unknown as DocumentNode<ChatGptDisconnectMutation, ChatGptDisconnectMutationVariables>;
export const ChatGptUsageOpenDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatGPTUsageOpen"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatGPTUsageOpen"}}]}}]} as unknown as DocumentNode<ChatGptUsageOpenMutation, ChatGptUsageOpenMutationVariables>;
export const OpenRouterConnectionStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"OpenRouterConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"openRouterConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"phase"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}}]}}]}}]} as unknown as DocumentNode<OpenRouterConnectionStateWatchSubscription, OpenRouterConnectionStateWatchSubscriptionVariables>;
export const OpenRouterConnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"OpenRouterConnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"openRouterConnect"}}]}}]} as unknown as DocumentNode<OpenRouterConnectMutation, OpenRouterConnectMutationVariables>;
export const OpenRouterConnectCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"OpenRouterConnectCancel"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"openRouterConnectCancel"}}]}}]} as unknown as DocumentNode<OpenRouterConnectCancelMutation, OpenRouterConnectCancelMutationVariables>;
export const OpenRouterDisconnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"OpenRouterDisconnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"openRouterDisconnect"}}]}}]} as unknown as DocumentNode<OpenRouterDisconnectMutation, OpenRouterDisconnectMutationVariables>;
export const OpenRouterKeysOpenDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"OpenRouterKeysOpen"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"openRouterKeysOpen"}}]}}]} as unknown as DocumentNode<OpenRouterKeysOpenMutation, OpenRouterKeysOpenMutationVariables>;
export const AwsConnectionStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"AWSConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"awsConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"phase"}},{"kind":"Field","name":{"kind":"Name","value":"arn"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}}]}}]}}]} as unknown as DocumentNode<AwsConnectionStateWatchSubscription, AwsConnectionStateWatchSubscriptionVariables>;
export const AwsConnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"AWSConnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"awsConnect"}}]}}]} as unknown as DocumentNode<AwsConnectMutation, AwsConnectMutationVariables>;
export const AwsConnectCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"AWSConnectCancel"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"awsConnectCancel"}}]}}]} as unknown as DocumentNode<AwsConnectCancelMutation, AwsConnectCancelMutationVariables>;
export const AwsDisconnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"AWSDisconnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"awsDisconnect"}}]}}]} as unknown as DocumentNode<AwsDisconnectMutation, AwsDisconnectMutationVariables>;
export const GitHubConnectionStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"GitHubConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"gitHubConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"phase"}},{"kind":"Field","name":{"kind":"Name","value":"login"}},{"kind":"Field","name":{"kind":"Name","value":"userCode"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}}]}}]}}]} as unknown as DocumentNode<GitHubConnectionStateWatchSubscription, GitHubConnectionStateWatchSubscriptionVariables>;
export const GitHubConnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GitHubConnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"gitHubConnect"}}]}}]} as unknown as DocumentNode<GitHubConnectMutation, GitHubConnectMutationVariables>;
export const GitHubConnectCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GitHubConnectCancel"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"gitHubConnectCancel"}}]}}]} as unknown as DocumentNode<GitHubConnectCancelMutation, GitHubConnectCancelMutationVariables>;
export const GitHubDisconnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GitHubDisconnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"gitHubDisconnect"}}]}}]} as unknown as DocumentNode<GitHubDisconnectMutation, GitHubDisconnectMutationVariables>;
export const GitHubSettingsOpenDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GitHubSettingsOpen"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"gitHubSettingsOpen"}}]}}]} as unknown as DocumentNode<GitHubSettingsOpenMutation, GitHubSettingsOpenMutationVariables>;
export const GoogleConnectionStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"GoogleConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"googleConnectionStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"phase"}},{"kind":"Field","name":{"kind":"Name","value":"email"}},{"kind":"Field","name":{"kind":"Name","value":"project"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}},{"kind":"Field","name":{"kind":"Name","value":"revocationUnconfirmed"}}]}}]}}]} as unknown as DocumentNode<GoogleConnectionStateWatchSubscription, GoogleConnectionStateWatchSubscriptionVariables>;
export const GoogleConnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GoogleConnect"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"fresh"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"googleConnect"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"fresh"},"value":{"kind":"Variable","name":{"kind":"Name","value":"fresh"}}}]}]}}]} as unknown as DocumentNode<GoogleConnectMutation, GoogleConnectMutationVariables>;
export const GoogleConnectCancelDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GoogleConnectCancel"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"googleConnectCancel"}}]}}]} as unknown as DocumentNode<GoogleConnectCancelMutation, GoogleConnectCancelMutationVariables>;
export const GoogleDisconnectDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"GoogleDisconnect"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"googleDisconnect"}}]}}]} as unknown as DocumentNode<GoogleDisconnectMutation, GoogleDisconnectMutationVariables>;
export const EnvVarsGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"EnvVarsGet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"envVarsGet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"value"}},{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"refusal"}}]}}]}}]} as unknown as DocumentNode<EnvVarsGetQuery, EnvVarsGetQueryVariables>;
export const EnvVarSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"EnvVarSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"name"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"value"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"target"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"EnvVarTarget"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"envVarSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"name"},"value":{"kind":"Variable","name":{"kind":"Name","value":"name"}}},{"kind":"Argument","name":{"kind":"Name","value":"value"},"value":{"kind":"Variable","name":{"kind":"Name","value":"value"}}},{"kind":"Argument","name":{"kind":"Name","value":"target"},"value":{"kind":"Variable","name":{"kind":"Name","value":"target"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]} as unknown as DocumentNode<EnvVarSetMutation, EnvVarSetMutationVariables>;
export const EnvVarRemoveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"EnvVarRemove"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"name"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"envVarRemove"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"name"},"value":{"kind":"Variable","name":{"kind":"Name","value":"name"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]} as unknown as DocumentNode<EnvVarRemoveMutation, EnvVarRemoveMutationVariables>;
export const MonitorTargetCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"MonitorTargetCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"monitorTargetCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"target"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"integration"}},{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"currentUID"}},{"kind":"Field","name":{"kind":"Name","value":"enabled"}},{"kind":"Field","name":{"kind":"Name","value":"readState"}},{"kind":"Field","name":{"kind":"Name","value":"unknownSignals"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}}]}}]}}]}}]}}]} as unknown as DocumentNode<MonitorTargetCollectionWatchSubscription, MonitorTargetCollectionWatchSubscriptionVariables>;
export const IncidentCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"IncidentCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"incidentCollectionWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"incident"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"targetID"}},{"kind":"Field","name":{"kind":"Name","value":"targetUID"}},{"kind":"Field","name":{"kind":"Name","value":"subjectLabel"}},{"kind":"Field","name":{"kind":"Name","value":"subjectAttributes"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"value"}}]}},{"kind":"Field","name":{"kind":"Name","value":"signal"}},{"kind":"Field","name":{"kind":"Name","value":"signalLabel"}},{"kind":"Field","name":{"kind":"Name","value":"severity"}},{"kind":"Field","name":{"kind":"Name","value":"firstSeenAt"}},{"kind":"Field","name":{"kind":"Name","value":"open"}},{"kind":"Field","name":{"kind":"Name","value":"occurrenceCount"}},{"kind":"Field","name":{"kind":"Name","value":"lastStartedAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastSeenAt"}},{"kind":"Field","name":{"kind":"Name","value":"lastEndReason"}},{"kind":"Field","name":{"kind":"Name","value":"baseline"}}]}}]}}]}}]} as unknown as DocumentNode<IncidentCollectionWatchSubscription, IncidentCollectionWatchSubscriptionVariables>;
export const MonitorSignalListDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"MonitorSignalList"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"monitorSignalList"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"integration"}},{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"severity"}}]}}]}}]} as unknown as DocumentNode<MonitorSignalListQuery, MonitorSignalListQueryVariables>;
export const IncidentOccurrenceEventFeedDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"IncidentOccurrenceEventFeed"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"incidentID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"IncidentID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"incidentOccurrenceEventFeed"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"incidentID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"incidentID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"incidentID"}},{"kind":"Field","name":{"kind":"Name","value":"occurrence"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"incidentID"}},{"kind":"Field","name":{"kind":"Name","value":"number"}},{"kind":"Field","name":{"kind":"Name","value":"startedAt"}},{"kind":"Field","name":{"kind":"Name","value":"endedAt"}},{"kind":"Field","name":{"kind":"Name","value":"endReason"}},{"kind":"Field","name":{"kind":"Name","value":"detail"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"value"}}]}}]}}]}}]}}]} as unknown as DocumentNode<IncidentOccurrenceEventFeedSubscription, IncidentOccurrenceEventFeedSubscriptionVariables>;
export const MonitorTargetEnabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"MonitorTargetEnabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"MonitorTargetID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"monitorTargetEnabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"enabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"enabled"}}]}}]}}]} as unknown as DocumentNode<MonitorTargetEnabledSetMutation, MonitorTargetEnabledSetMutationVariables>;
export const LogsFetchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"LogsFetch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"request"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"LogsFetchInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"logsFetch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"request"},"value":{"kind":"Variable","name":{"kind":"Name","value":"request"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"records"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"timestamp"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"truncatedBytes"}},{"kind":"Field","name":{"kind":"Name","value":"cursor"}},{"kind":"Field","name":{"kind":"Name","value":"stream"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"pod"}},{"kind":"Field","name":{"kind":"Name","value":"container"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"place"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"node"}},{"kind":"Field","name":{"kind":"Name","value":"region"}},{"kind":"Field","name":{"kind":"Name","value":"zone"}},{"kind":"Field","name":{"kind":"Name","value":"os"}},{"kind":"Field","name":{"kind":"Name","value":"arch"}}]}},{"kind":"Field","name":{"kind":"Name","value":"gap"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"after"}},{"kind":"Field","name":{"kind":"Name","value":"before"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"page"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"prev"}},{"kind":"Field","name":{"kind":"Name","value":"next"}},{"kind":"Field","name":{"kind":"Name","value":"follow"}}]}},{"kind":"Field","name":{"kind":"Name","value":"notices"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"code"}},{"kind":"Field","name":{"kind":"Name","value":"message"}}]}}]}}]}}]} as unknown as DocumentNode<LogsFetchQuery, LogsFetchQueryVariables>;
export const LogsTailDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"LogsTail"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"request"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"LogsTailInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"logsTail"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"request"},"value":{"kind":"Variable","name":{"kind":"Name","value":"request"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"record"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"timestamp"}},{"kind":"Field","name":{"kind":"Name","value":"message"}},{"kind":"Field","name":{"kind":"Name","value":"truncatedBytes"}},{"kind":"Field","name":{"kind":"Name","value":"cursor"}},{"kind":"Field","name":{"kind":"Name","value":"stream"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"namespace"}},{"kind":"Field","name":{"kind":"Name","value":"pod"}},{"kind":"Field","name":{"kind":"Name","value":"container"}},{"kind":"Field","name":{"kind":"Name","value":"previous"}}]}},{"kind":"Field","name":{"kind":"Name","value":"place"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"node"}},{"kind":"Field","name":{"kind":"Name","value":"region"}},{"kind":"Field","name":{"kind":"Name","value":"zone"}},{"kind":"Field","name":{"kind":"Name","value":"os"}},{"kind":"Field","name":{"kind":"Name","value":"arch"}}]}},{"kind":"Field","name":{"kind":"Name","value":"gap"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"after"}},{"kind":"Field","name":{"kind":"Name","value":"before"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"notice"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"code"}},{"kind":"Field","name":{"kind":"Name","value":"message"}}]}},{"kind":"Field","name":{"kind":"Name","value":"ended"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"code"}},{"kind":"Field","name":{"kind":"Name","value":"message"}}]}}]}}]}}]} as unknown as DocumentNode<LogsTailSubscription, LogsTailSubscriptionVariables>;
export const MemoryCollectionWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"MemoryCollectionWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"memoryCollectionWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"type"}},{"kind":"Field","name":{"kind":"Name","value":"memory"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"body"}},{"kind":"Field","name":{"kind":"Name","value":"writtenBy"}},{"kind":"Field","name":{"kind":"Name","value":"updatedAt"}}]}}]}}]}}]} as unknown as DocumentNode<MemoryCollectionWatchSubscription, MemoryCollectionWatchSubscriptionVariables>;
export const MemorySaveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"MemorySave"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"input"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"MemorySaveInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"memorySave"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"input"},"value":{"kind":"Variable","name":{"kind":"Name","value":"input"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}}]}}]}}]} as unknown as DocumentNode<MemorySaveMutation, MemorySaveMutationVariables>;
export const MemoryDeleteDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"MemoryDelete"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"MemoryID"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"memoryDelete"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}]}]}}]} as unknown as DocumentNode<MemoryDeleteMutation, MemoryDeleteMutationVariables>;
export const ModelsStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ModelsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"modelsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"provider"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"label"}}]}},{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"efforts"}},{"kind":"Field","name":{"kind":"Name","value":"defaultEffort"}}]}}]}}]} as unknown as DocumentNode<ModelsStateWatchSubscription, ModelsStateWatchSubscriptionVariables>;
export const ChatNetworkEnabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatNetworkEnabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatNetworkEnabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"enabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"networkEnabled"}}]}}]}}]} as unknown as DocumentNode<ChatNetworkEnabledSetMutation, ChatNetworkEnabledSetMutationVariables>;
export const PermissionSettingsGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"PermissionSettingsGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionSettingsGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"targets"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"defaultMode"}},{"kind":"Field","name":{"kind":"Name","value":"accounts"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"account"}},{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"mode"}},{"kind":"Field","name":{"kind":"Name","value":"source"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"rules"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"effect"}},{"kind":"Field","name":{"kind":"Name","value":"accessLevel"}},{"kind":"Field","name":{"kind":"Name","value":"principal"}},{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"scope"}},{"kind":"Field","name":{"kind":"Name","value":"service"}},{"kind":"Field","name":{"kind":"Name","value":"operation"}},{"kind":"Field","name":{"kind":"Name","value":"resource"}},{"kind":"Field","name":{"kind":"Name","value":"line"}}]}},{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}},{"kind":"Field","name":{"kind":"Name","value":"accessControl"}}]}}]}}]} as unknown as DocumentNode<PermissionSettingsGetQuery, PermissionSettingsGetQueryVariables>;
export const PermissionDefaultModeSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionDefaultModeSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"target"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionTarget"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"mode"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionMode"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionDefaultModeSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"target"},"value":{"kind":"Variable","name":{"kind":"Name","value":"target"}}},{"kind":"Argument","name":{"kind":"Name","value":"mode"},"value":{"kind":"Variable","name":{"kind":"Name","value":"mode"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionDefaultModeSetMutation, PermissionDefaultModeSetMutationVariables>;
export const PermissionModeSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionModeSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"target"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionTarget"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"account"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"mode"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionMode"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionModeSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"target"},"value":{"kind":"Variable","name":{"kind":"Name","value":"target"}}},{"kind":"Argument","name":{"kind":"Name","value":"account"},"value":{"kind":"Variable","name":{"kind":"Name","value":"account"}}},{"kind":"Argument","name":{"kind":"Name","value":"mode"},"value":{"kind":"Variable","name":{"kind":"Name","value":"mode"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionModeSetMutation, PermissionModeSetMutationVariables>;
export const PermissionModeClearDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionModeClear"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"target"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionTarget"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"account"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionModeClear"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"target"},"value":{"kind":"Variable","name":{"kind":"Name","value":"target"}}},{"kind":"Argument","name":{"kind":"Name","value":"account"},"value":{"kind":"Variable","name":{"kind":"Name","value":"account"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionModeClearMutation, PermissionModeClearMutationVariables>;
export const PermissionRuleAddDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionRuleAdd"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"input"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionRuleInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionRuleAdd"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"input"},"value":{"kind":"Variable","name":{"kind":"Name","value":"input"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionRuleAddMutation, PermissionRuleAddMutationVariables>;
export const PermissionRuleUpdateDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionRuleUpdate"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"input"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"PermissionRuleInput"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionRuleUpdate"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"input"},"value":{"kind":"Variable","name":{"kind":"Name","value":"input"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionRuleUpdateMutation, PermissionRuleUpdateMutationVariables>;
export const PermissionRuleRemoveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"PermissionRuleRemove"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"permissionRuleRemove"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"alwaysAsks"}}]}}]}}]} as unknown as DocumentNode<PermissionRuleRemoveMutation, PermissionRuleRemoveMutationVariables>;
export const ProviderCredentialsStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ProviderCredentialsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"providerCredentialsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"keyVariable"}},{"kind":"Field","name":{"kind":"Name","value":"source"}},{"kind":"Field","name":{"kind":"Name","value":"hasEnvironmentKey"}},{"kind":"Field","name":{"kind":"Name","value":"hasSavedKey"}},{"kind":"Field","name":{"kind":"Name","value":"failure"}}]}}]}}]} as unknown as DocumentNode<ProviderCredentialsStateWatchSubscription, ProviderCredentialsStateWatchSubscriptionVariables>;
export const ProviderKeySaveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ProviderKeySave"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"key"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"providerKeySave"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"providerID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}}},{"kind":"Argument","name":{"kind":"Name","value":"key"},"value":{"kind":"Variable","name":{"kind":"Name","value":"key"}}}]}]}}]} as unknown as DocumentNode<ProviderKeySaveMutation, ProviderKeySaveMutationVariables>;
export const ProviderKeyDeleteDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ProviderKeyDelete"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"providerKeyDelete"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"providerID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"providerID"}}}]}]}}]} as unknown as DocumentNode<ProviderKeyDeleteMutation, ProviderKeyDeleteMutationVariables>;
export const SandboxCloudAccessGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"SandboxCloudAccessGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxCloudAccessGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"on"}}]}}]}}]} as unknown as DocumentNode<SandboxCloudAccessGetQuery, SandboxCloudAccessGetQueryVariables>;
export const SandboxConsoleStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"SandboxConsoleStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxConsoleStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"run"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"number"}},{"kind":"Field","name":{"kind":"Name","value":"command"}},{"kind":"Field","name":{"kind":"Name","value":"clusterID"}},{"kind":"Field","name":{"kind":"Name","value":"network"}},{"kind":"Field","name":{"kind":"Name","value":"running"}},{"kind":"Field","name":{"kind":"Name","value":"output"}},{"kind":"Field","name":{"kind":"Name","value":"discarded"}},{"kind":"Field","name":{"kind":"Name","value":"exitCode"}},{"kind":"Field","name":{"kind":"Name","value":"notice"}},{"kind":"Field","name":{"kind":"Name","value":"durationMs"}}]}}]}}]}}]} as unknown as DocumentNode<SandboxConsoleStateWatchSubscription, SandboxConsoleStateWatchSubscriptionVariables>;
export const SandboxConsoleRunDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxConsoleRun"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"command"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"network"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxConsoleRun"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"command"},"value":{"kind":"Variable","name":{"kind":"Name","value":"command"}}},{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"network"},"value":{"kind":"Variable","name":{"kind":"Name","value":"network"}}}]}]}}]} as unknown as DocumentNode<SandboxConsoleRunMutation, SandboxConsoleRunMutationVariables>;
export const SandboxConsoleStopDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxConsoleStop"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"number"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"Int"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxConsoleStop"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"number"},"value":{"kind":"Variable","name":{"kind":"Name","value":"number"}}}]}]}}]} as unknown as DocumentNode<SandboxConsoleStopMutation, SandboxConsoleStopMutationVariables>;
export const SandboxDefaultStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"SandboxDefaultStateWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDefaultStateWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDisabled"}},{"kind":"Field","name":{"kind":"Name","value":"clusterOverride"}}]}}]}}]} as unknown as DocumentNode<SandboxDefaultStateWatchSubscription, SandboxDefaultStateWatchSubscriptionVariables>;
export const SandboxDefaultSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxDefaultSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDefaultSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"sandboxDisabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDisabled"}},{"kind":"Field","name":{"kind":"Name","value":"clusterOverride"}}]}}]}}]} as unknown as DocumentNode<SandboxDefaultSetMutation, SandboxDefaultSetMutationVariables>;
export const SandboxDefaultResetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxDefaultReset"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ClusterID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDefaultReset"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"clusterID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"clusterID"}}},{"kind":"Argument","name":{"kind":"Name","value":"sandboxDisabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxDisabled"}},{"kind":"Field","name":{"kind":"Name","value":"clusterOverride"}}]}}]}}]} as unknown as DocumentNode<SandboxDefaultResetMutation, SandboxDefaultResetMutationVariables>;
export const SandboxExecutablesStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"SandboxExecutablesStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxExecutablesStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"probing"}},{"kind":"Field","name":{"kind":"Name","value":"probes"}},{"kind":"Field","name":{"kind":"Name","value":"executables"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"invocation"}},{"kind":"Field","name":{"kind":"Name","value":"registered"}},{"kind":"Field","name":{"kind":"Name","value":"probed"}},{"kind":"Field","name":{"kind":"Name","value":"resolved"}},{"kind":"Field","name":{"kind":"Name","value":"shim"}},{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"ok"}},{"kind":"Field","name":{"kind":"Name","value":"version"}},{"kind":"Field","name":{"kind":"Name","value":"error"}},{"kind":"Field","name":{"kind":"Name","value":"refusal"}}]}}]}}]}}]} as unknown as DocumentNode<SandboxExecutablesStateWatchSubscription, SandboxExecutablesStateWatchSubscriptionVariables>;
export const SandboxExecutablesProbeDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxExecutablesProbe"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxExecutablesProbe"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]} as unknown as DocumentNode<SandboxExecutablesProbeMutation, SandboxExecutablesProbeMutationVariables>;
export const SandboxExecutableRegisterDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxExecutableRegister"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"name"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"invocation"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxExecutableRegister"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"name"},"value":{"kind":"Variable","name":{"kind":"Name","value":"name"}}},{"kind":"Argument","name":{"kind":"Name","value":"invocation"},"value":{"kind":"Variable","name":{"kind":"Name","value":"invocation"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]} as unknown as DocumentNode<SandboxExecutableRegisterMutation, SandboxExecutableRegisterMutationVariables>;
export const SandboxExecutableRemoveDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxExecutableRemove"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"name"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxExecutableRemove"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"name"},"value":{"kind":"Variable","name":{"kind":"Name","value":"name"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}}]}}]}}]} as unknown as DocumentNode<SandboxExecutableRemoveMutation, SandboxExecutableRemoveMutationVariables>;
export const SandboxFoldersStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"SandboxFoldersStateWatch"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxFoldersStateWatch"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"always"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"write"}},{"kind":"Field","name":{"kind":"Name","value":"refusal"}}]}},{"kind":"Field","name":{"kind":"Name","value":"chat"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"write"}},{"kind":"Field","name":{"kind":"Name","value":"refusal"}}]}},{"kind":"Field","name":{"kind":"Name","value":"never"}},{"kind":"Field","name":{"kind":"Name","value":"closed"}},{"kind":"Field","name":{"kind":"Name","value":"wide"}}]}}]}}]} as unknown as DocumentNode<SandboxFoldersStateWatchSubscription, SandboxFoldersStateWatchSubscriptionVariables>;
export const FolderGrantDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"FolderGrant"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"path"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"write"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"duration"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"GrantDuration"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"folderGrant"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}},{"kind":"Argument","name":{"kind":"Name","value":"path"},"value":{"kind":"Variable","name":{"kind":"Name","value":"path"}}},{"kind":"Argument","name":{"kind":"Name","value":"write"},"value":{"kind":"Variable","name":{"kind":"Name","value":"write"}}},{"kind":"Argument","name":{"kind":"Name","value":"duration"},"value":{"kind":"Variable","name":{"kind":"Name","value":"duration"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"wide"}}]}}]}}]} as unknown as DocumentNode<FolderGrantMutation, FolderGrantMutationVariables>;
export const FolderRevokeDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"FolderRevoke"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}},"type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"duration"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"GrantDuration"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"folderRevoke"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"chatID"},"value":{"kind":"Variable","name":{"kind":"Name","value":"chatID"}}},{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"duration"},"value":{"kind":"Variable","name":{"kind":"Name","value":"duration"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"wide"}}]}}]}}]} as unknown as DocumentNode<FolderRevokeMutation, FolderRevokeMutationVariables>;
export const SandboxPathGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"SandboxPathGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxPathGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"value"}},{"kind":"Field","name":{"kind":"Name","value":"refusal"}},{"kind":"Field","name":{"kind":"Name","value":"folders"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"path"}},{"kind":"Field","name":{"kind":"Name","value":"origin"}},{"kind":"Field","name":{"kind":"Name","value":"target"}},{"kind":"Field","name":{"kind":"Name","value":"searched"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"addable"}},{"kind":"Field","name":{"kind":"Name","value":"shared"}}]}}]}},{"kind":"Field","name":{"kind":"Name","value":"sandboxPathFaultGet"}}]}}]} as unknown as DocumentNode<SandboxPathGetQuery, SandboxPathGetQueryVariables>;
export const SandboxPathSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxPathSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"value"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxPathSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"value"},"value":{"kind":"Variable","name":{"kind":"Name","value":"value"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"value"}}]}}]}}]} as unknown as DocumentNode<SandboxPathSetMutation, SandboxPathSetMutationVariables>;
export const SandboxPathRefreshDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"SandboxPathRefresh"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxPathRefresh"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"value"}}]}}]}}]} as unknown as DocumentNode<SandboxPathRefreshMutation, SandboxPathRefreshMutationVariables>;
export const ChatSandboxDisabledSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ChatSandboxDisabledSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"id"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"ChatID"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"chatSandboxDisabledSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"id"},"value":{"kind":"Variable","name":{"kind":"Name","value":"id"}}},{"kind":"Argument","name":{"kind":"Name","value":"sandboxDisabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"sandboxDisabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"id"}},{"kind":"Field","name":{"kind":"Name","value":"sandboxDisabled"}}]}}]}}]} as unknown as DocumentNode<ChatSandboxDisabledSetMutation, ChatSandboxDisabledSetMutationVariables>;
export const SandboxStateGetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"query","name":{"kind":"Name","value":"SandboxStateGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"sandboxStateGet"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"available"}},{"kind":"Field","name":{"kind":"Name","value":"reason"}},{"kind":"Field","name":{"kind":"Name","value":"networkAvailable"}},{"kind":"Field","name":{"kind":"Name","value":"networkReason"}}]}}]}}]} as unknown as DocumentNode<SandboxStateGetQuery, SandboxStateGetQueryVariables>;
export const ToolSettingsStateWatchDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"subscription","name":{"kind":"Name","value":"ToolSettingsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"toolSettingsStateWatch"},"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"label"}},{"kind":"Field","name":{"kind":"Name","value":"group"}},{"kind":"Field","name":{"kind":"Name","value":"description"}},{"kind":"Field","name":{"kind":"Name","value":"enabled"}},{"kind":"Field","name":{"kind":"Name","value":"alwaysOn"}}]}}]}}]} as unknown as DocumentNode<ToolSettingsStateWatchSubscription, ToolSettingsStateWatchSubscriptionVariables>;
export const ToolSettingSetDocument = {"kind":"Document","definitions":[{"kind":"OperationDefinition","operation":"mutation","name":{"kind":"Name","value":"ToolSettingSet"},"variableDefinitions":[{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"name"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"String"}}}},{"kind":"VariableDefinition","variable":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}},"type":{"kind":"NonNullType","type":{"kind":"NamedType","name":{"kind":"Name","value":"Boolean"}}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"toolSettingSet"},"arguments":[{"kind":"Argument","name":{"kind":"Name","value":"name"},"value":{"kind":"Variable","name":{"kind":"Name","value":"name"}}},{"kind":"Argument","name":{"kind":"Name","value":"enabled"},"value":{"kind":"Variable","name":{"kind":"Name","value":"enabled"}}}],"selectionSet":{"kind":"SelectionSet","selections":[{"kind":"Field","name":{"kind":"Name","value":"name"}},{"kind":"Field","name":{"kind":"Name","value":"enabled"}}]}}]}}]} as unknown as DocumentNode<ToolSettingSetMutation, ToolSettingSetMutationVariables>;