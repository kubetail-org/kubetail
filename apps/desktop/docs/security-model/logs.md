# Logs

[← Security model](../security-model.md)

Two kinds of log are covered here: the logs Kubetail writes about itself, and the container logs it reads from a cluster. Both carry attacker-controlled text. Kubetail's own records are written one per line, redacted and bounded at the sink, and a page's reports go to a file of their own, so cluster or page text cannot forge a line or evict the host's history. A container's log line leaves the sidecar with no terminal control sequence but its SGR ones, the viewer draws it as text, and the model reads it stripped, redacted and capped. Redaction there is hygiene, never a boundary: the lines are the ones `kubectl logs` would show. A log window reaches only its paired app window, through the host. The log cache holds lines in the clear, and a loud container can slow the pages that read it; both are decisions.

A log view in a window of its own is reviewed in [log windows](../security/2026-11-04-log-windows.md).

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| LOG-1 | The sidecar writes one record per line, control characters escaped by the JSON encoder, so cluster-controlled text cannot forge a line in its log. | **Enforced** | — |
| LOG-2 | A line off the sidecar's pipes is sanitized onto one line before it is emitted, under `target: "sidecar"`. | **Enforced** | — |
| LOG-3 | A debug build keeps a forwarded sidecar line out of `main.log`, which `sidecar.log` already holds. | **Enforced** | — |
| LOG-4 | Webview-reported log fields ride under `reported`, never as host fields. The sanitized message is the one page-controlled value at top level. | **Enforced** | — |
| LOG-5 | Every string a webview reports is sanitized and capped. | **Enforced** | — |
| LOG-6 | Each webview's log reports are rate limited. | **Enforced** | — |
| LOG-7 | Webview records go to `webview.log`, so a storm cannot evict the host's own history. | **Enforced** | — |
| LOG-8 | GraphQL error logs exclude request text, values, names, aliases and error messages. | **Enforced** | — |
| LOG-9 | Every log record is rendered redacted and bounded at the sink, whoever wrote it. | **Enforced** | — |
| LOG-10 | A logged value that is neither string nor error is reduced to its type name rather than encoded field by field. | **Enforced** | — |
| LOG-11 | A diagnostic persisted on a record is rendered where it is recorded. | **Enforced** | — |
| LOG-12 | A log line leaves the sidecar with no terminal control sequence but its SGR ones, cut at 16 KiB. | **Enforced** | Colour can hide text from the eye. |
| LOG-13 | Grep matches a log line's text without its control sequences. | **Enforced** | — |
| LOG-14 | The model reads no SGR. | **Enforced** | — |
| LOG-15 | The viewer draws every cell as text, through `VisibleText`, and a hole's notice the same way. | **Enforced** | — |
| LOG-16 | The viewer draws a message's colours through `AnsiText`, which reads fancy-ansi's escaped output back from an inert document and inserts no HTML. | **Enforced** | Colour can hide text from the eye. |
| LOG-17 | A selected log line reaches the model escaped, stripped, redacted and capped. | **Enforced** | Redaction is hygiene, never a boundary: the boundary is the user's selection. |
| LOG-18 | A selected log line reaches the model only as this chat's view. | **Enforced** | — |
| LOG-19 | A log line the model reads through `LogsRead` reaches it stripped, redacted, then cut by the service at the call's cap, 2 KiB by default. | **Enforced** | Redaction is hygiene, never a boundary: the lines are the ones `kubectl logs` would show. |
| LOG-20 | A `LogsRead` page holds at most 100 lines and 16 KiB of messages. | **Enforced** | — |
| LOG-21 | A `LogsRead` page never starts a line as the sidecar's. | **Enforced** | — |
| LOG-22 | A `LogsRead` page carries a token the service checks. | **Enforced** | — |
| LOG-23 | A watch the model starts through `LogsWatch` follows a scope unasked, held by the chat's task slots and a day's cap. | **Enforced** | — |
| LOG-24 | A watch's lines reach the model stripped, redacted, then cut by the service at the call's cap, 2 KiB by default, at most 100 lines and 16 KiB in one escaped notice. | **Enforced** | — |
| LOG-25 | A watch's end starts a turn only under a user's send. | **Enforced** | A pattern that matches at once costs one turn. |
| LOG-26 | Neither a subagent nor the monitor can start a watch. | **Enforced** | — |
| LOG-27 | What the host tells an app window about its log windows rides a channel that window's page made, and every lookup starts from the caller's label. No page reaches another window's log windows or hears its frames. | **Enforced** | — |
| LOG-28 | An app window's command names its current page, so a request from a page that is gone changes nothing. | **Enforced** | — |
| LOG-29 | A log window's native title is attacker-controlled text: the host builds it from bounded parts with every invisible character stripped. | **Enforced** | — |
| LOG-30 | A log window cannot outlive its app window: the host destroys the log windows it closes, so no page can refuse the close. | **Enforced** | — |
| LOG-31 | A log window's viewport, selected log lines included, reaches its paired app window alone. | **Enforced** | — |
| LOG-32 | A log window's viewport is bounded: 192 KiB of lines by the page, 256 KiB in all by the host. | **Enforced** | — |
| LOG-33 | A log window's viewport is stamped with a generation only the host steps, so a reloaded page's old lines cannot ride. | **Enforced** | — |
| LOG-34 | A cluster cache's log file holds the log lines its pages read, in the clear, under `<cache>/logs/`, a secret a container wrote included, as `kubectl logs` would show it. | **By decision** | A line reaches the model through `LogsRead` and the View section. The grep a page or a follow runs is the user's or the model's pattern over text the cluster wrote. |
| LOG-35 | A container's log can stop its own stream and slow the pages that read it, and can make a page read about one second, at most 1 MiB, past a window's reservation; no more. | **By decision** | A reservation is charged, not enforced, and a window is not stopped in its last second. |

## Evidence

### LOG-1

- **Where:** `sidecar/internal/lib/logging/logging.go`
- `TestInitWritesOneLinePerRecord` — one record per line.

### LOG-2

- **Where:** `src-tauri/src/services/sidecar/logs.rs`, `src-tauri/src/logging.rs`
- `a_forwarded_message_stays_on_one_line` — a forwarded message stays on one line.
- Note: the host decodes each record's JSON to build that line, so the message's control characters are real again by the time it is written.

### LOG-3

- **Where:** `src-tauri/src/services/sidecar/logs.rs`, `src-tauri/src/logging.rs`
- `a_forwarded_sidecar_line_reaches_main_log_only_in_a_release_build` — a debug build keeps it out of `main.log`.

### LOG-4

- **Where:** `src-tauri/src/webview_log.rs`, `src-tauri/src/logging.rs`
- `page_supplied_fields_ride_under_one_key` — page-supplied fields ride under `reported`.

### LOG-5

- **Where:** `src-tauri/src/webview_log.rs`
- `a_message_with_newlines_and_escapes_is_one_line_without_escape_bytes` — a string is sanitized.
- `over_long_fields_are_capped_and_the_fields_still_parse` — a string is capped.

### LOG-6

- **Where:** `src-tauri/src/webview_log.rs`
- `a_burst_past_the_bound_is_dropped_and_summarized_once_refilled` — each webview is rate limited.

### LOG-7

- **Where:** `src-tauri/src/webview_log.rs`, `src-tauri/src/logging.rs`
- `a_webview_record_goes_to_its_own_file` — the records go to `webview.log`.

### LOG-8

- **Where:** `sidecar/graph/server.go`
- `TestErrorLogOmitsRequestData` — the error log omits request data.
- Note: only operation type, Go error type, the wrapped cause's type, cancel flags and `code` are logged.

### LOG-9

- **Where:** `sidecar/internal/lib/logging/redact.go`, `sidecar/internal/lib/safe/safe.go`
- `safe_test.go` — pins the shapes stripped (a URL's query and userinfo, an echoed `Authorization`/`Set-Cookie`, a bearer/JWT token) and the cap.
- `TestInitRedactsWhatACallerDidNotRender` — covers message, attr, `With` attr and group, so a dependency logging its error whole (beehive, client-go, oauth2) is redacted too.
- `TestCachePassLogsAFailureWithoutTheCredential` — a subsystem sentinel through the same sink.

### LOG-10

- **Where:** `sidecar/internal/lib/logging/redact.go`
- `TestInitReducesAStructToItsType` — a struct is reduced to its type name.
- `TestInitReducesAStructWhicheverWayItArrives` — the three ways one arrives.

### LOG-11

- **Where:** `sidecar/internal/lib/supervisor/result.go`, `sidecar/internal/services/cluster/internal/kubesync/service.go`
- `TestFailRendersTheMessageItStores`, `TestArmRendersWhatADriverErrorCanSay` — a diagnostic is rendered where it is recorded.
- Note: a condition or discovery message outlives its log line and is served to the UI, so the sink (LOG-9) cannot cover it.
- Note: `Result.Err` keeps the original for a caller matching on it.

### LOG-12

- **Where:** `sidecar/internal/services/logs/logline/clean.go`, `sidecar/internal/services/logs/kubelet/read.go`, `sidecar/internal/services/logs/fetch.go`
- **Records:** [log lines keep their colours](../security/2026-10-19-log-lines-keep-their-colours.md)
- `TestStripControlRemovesSequences`, `TestStripControlCutsALongLine`, `TestKeepColorKeepsOnlySGR`, `TestKeepColorCutsBeforeASequence`, `TestALineIsCleaned` — no control sequence but the SGR ones, cut at 16 KiB.
- `TestARefusalIsItsStatus` — the API server's refusal text.

### LOG-13

- **Where:** `sidecar/internal/services/logs/logline/clean.go`
- `TestGrepMatchesAKeptLinesText` — grep matches a kept line's text.

### LOG-14

- **Where:** `sidecar/internal/services/logs/logline/formodel.go`
- `TestForModelStripsControlSequences` — the model reads no SGR.

### LOG-15

- **Where:** `src/components/logs/log-viewer/row.tsx`
- `draws markup in a line as its text`, `spells out what cluster text could hide` — the viewer draws every cell as text, through `VisibleText`.
- `spells out what cluster text could hide in a hole's notice` — a hole's notice the same way.

### LOG-16

- **Where:** `src/components/logs/ansi-text.tsx`
- **Records:** [log lines keep their colours](../security/2026-10-19-log-lines-keep-their-colours.md)
- `ansi-text.test.tsx` — `AnsiText` inserts no HTML.

### LOG-17

- **Where:** `sidecar/internal/agent/chat/view.go`, `sidecar/internal/services/logs/logline/formodel.go`
- **Records:** [the view rides the question](../security/2026-10-17-the-view-rides-the-question.md)
- `TestTheViewSectionEscapesAndCleansMessages` — a message is escaped and cleaned.
- `TestTheViewSectionCutsAMessageAtOneKiBMarshalled`, `TestTheViewSectionCountsTheLinesPastItsBudget` — the caps.
- `TestForModelRedacts` — the redaction.

### LOG-18

- **Where:** `sidecar/internal/agent/chat/view.go`
- **Records:** [the view rides the question](../security/2026-10-17-the-view-rides-the-question.md)
- `TestASendRefusesASnapshotItCannotVouchFor` — a send refuses a snapshot the sidecar cannot vouch for.

### LOG-19

- **Where:** `sidecar/internal/tools/logsread`, `sidecar/internal/services/logs/fetch.go`, `sidecar/internal/services/logs/logline/formodel.go`
- **Records:** [the model reads logs](../security/2026-10-17-the-model-reads-logs.md)
- `TestAMessageIsRedactedBeforeItIsCut` — the service, under `ForModel`.
- `TestEveryPageIsForTheModel` — every page is asked for the model.

### LOG-20

- **Where:** `sidecar/internal/tools/logsread`, `sidecar/internal/services/logs/fetch.go`
- **Records:** [the model reads logs](../security/2026-10-17-the-model-reads-logs.md)
- `TestTheActionIsThePage` — the action is the page.
- `TestNotesStopAtTheirBudget`, `TestNoticesStopAtTheirBudget` — the notes and the notices stop at their budgets.
- `TestTheWidestPagePrintsItsTokensWhole` — the widest page.
- Note: the widest page passes the inline limit and is saved to a file, as every tool's oversized result is.

### LOG-21

- **Where:** `sidecar/internal/tools/logsread`
- **Records:** [the model reads logs](../security/2026-10-17-the-model-reads-logs.md)
- `TestAHoleIsALineBeforeIt` — a hole is a line of its own before the line.

### LOG-22

- **Where:** `sidecar/internal/tools/logsread`, `sidecar/internal/services/logs/fetch.go`
- **Records:** [the model reads logs](../security/2026-10-17-the-model-reads-logs.md)
- `TestABadCursorIsRefused` — a token the service did not write is refused.
- `TestAServiceRefusalKeepsItsCodeAndReason` — a refusal of the service keeps its code and reason.

### LOG-23

- **Where:** `sidecar/internal/tools/logswatch`, `sidecar/internal/agent/chat/watches.go`
- **Records:** [the model watches logs](../security/2026-10-18-the-model-watches-logs.md)
- `TestTheReceiptNamesTheWatch` — the receipt names the watch.
- `TestAFifthWatchIsRefused` — the chat's task slots.
- `TestAnUntilPastTheCapIsCut` — the cap on a watch's end.

### LOG-24

- **Where:** `sidecar/internal/tools/logswatch`, `sidecar/internal/services/logs/tail.go`, `sidecar/internal/agent/chat/notices.go`
- **Records:** [the model watches logs](../security/2026-10-18-the-model-watches-logs.md)
- `TestATailRedactsAMessageBeforeItIsCut` — the service, under `ForModel`.
- `TestTheByteLimitEndsItOnTheLineThatReachesIt`, `TestNoSamplePassesTheInlineLimit` — the limits on a watch's lines.
- `TestNoticesPrintInTheHeader` — a watch's notices.
- `TestATailErrorEndsItWithNoText` — a follow's error ends the watch with none of its text.
- `TestAWatchsNoticeCarriesItsResult` — the one escaped notice.

### LOG-25

- **Where:** `sidecar/internal/agent/chat/watches.go`, `sidecar/internal/agent/chat/notices.go`
- **Records:** [the model watches logs](../security/2026-10-18-the-model-watches-logs.md)
- `TestAWatchStartsATurnOnlyUnderASend`, `TestAWatchFromANoticeTurnStartsNoTurn` — a watch's end starts a turn only under a user's send.

### LOG-26

- **Where:** `sidecar/internal/agent/chat/watches.go`
- **Records:** [the model watches logs](../security/2026-10-18-the-model-watches-logs.md)
- `TestASubagentStartsNoWatch` — a subagent.
- `TestTheMonitorStartsNoWatch` — the monitor.

### LOG-27

- **Where:** `src-tauri/src/log_windows.rs`
- `own_frames_reach_the_paired_app_window_alone` — frames reach the paired app window alone.

### LOG-28

- **Where:** `src-tauri/src/log_windows.rs`
- `page_an_ended_page_is_refused_by_every_app_window_call`, `page_a_rebuilt_main_gets_a_new_page_and_the_old_one_stays_ended` — a request from a page that is gone changes nothing.

### LOG-29

- **Where:** `src-tauri/src/window_title.rs`, `src-tauri/src/log_windows.rs`
- `window_title.rs` — `window_title`'s tests.
- `own_every_bound_is_refused` — the bounds on the parts.

### LOG-30

- **Where:** `src-tauri/src/log_windows.rs`
- `the_host_destroys_the_log_windows_it_closes` — reads the source for `destroy()` and no `close()`.
- Note: the lifecycle tests (`cancel_*`) pin it too.

### LOG-31

- **Where:** `src-tauri/src/log_windows.rs`
- `own_frames_reach_the_paired_app_window_alone` — the viewport reaches its paired app window alone.
- Note: the app window draws the viewport only as a count in the composer's chip and sends it as data in the snapshot.

### LOG-32

- **Where:** `src-tauri/src/log_windows.rs`, `src/lib/log-window-push.ts`
- `own_every_bound_is_refused` — the host's bounds.
- `src/lib/log-window-push.test.ts` — `cutSelection`'s tests pin the page's bound.

### LOG-33

- **Where:** `src-tauri/src/log_windows.rs`
- `own_a_push_from_a_stale_generation_is_refused` — a push from a stale generation is refused.

### LOG-34

- **Where:** `sidecar/internal/services/logs/logstore`, `sidecar/internal/services/logs/fetch.go`
- **Records:** [the log cache](../adr/2026-10-17-the-log-cache.md), [backward log reads assume normal logs](../adr/2026-10-18-backward-reads-assume-normal-logs.md), [the cache is ordinary application data](../adr/2026-09-02-the-cache-is-ordinary-application-data.md), [the model reads logs](../security/2026-10-17-the-model-reads-logs.md), [the view rides the question](../security/2026-10-17-the-view-rides-the-question.md)
- `TestALogFileIsOwnerOnly` — the file is owner-only like every other SQLite file we open.
- `TestAGrepPastMaxGrepIsRefused` — a grep is capped at `MaxGrep` on the wire as in the tool.
- Note: the decision is the log cache's, and for its 1 GiB budget that backward log reads assume normal logs, under the decision that the cache is ordinary application data. It is a row of its own since logs hold secrets more often than object bodies do.
- Note: the file is removed with its cache and cleared by `clusterCacheClear`.
- Note: the bash tool's `DeniedDirs` covers `<cache>/`, and `Read`, `Write` and `Edit` fence it.
- Note: a line reaches the model through `LogsRead` and the View section, both through `logs.ForModel`.
- Note: one is a read the model makes, whose lines the transcript shows (the model reads logs). The other is a line the user selects in the viewer and sends (the view rides the question).
- Note: Go's `regexp` is linear in both.

### LOG-35

- **Where:** `sidecar/internal/services/logs/kubelet/limits.go`
- **Records:** [backward log reads assume normal logs](../adr/2026-10-18-backward-reads-assume-normal-logs.md)
- `TestStreamsReadingAtOnceStayWithinThePage`, `TestAWindowsReservationIsChargedNotEnforced`, `TestTheTailLinesWindowIsCappedByItsAllowance`, `TestAViewOfOneTooLoudStreamAnswersThePage` — the limits below.
- Note: a page reads at most 16 MiB and 16 GETs a stream and 64 MiB a query.
- Note: a reservation is charged, not enforced, and a window is not stopped in its last second.
- Note: a second past 1 MiB stops its stream and names it in `skipped`, and the rest of the view goes on.
- Note: the `tailLines` window stays within its allowance.
