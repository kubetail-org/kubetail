# The webview and the windows

[← Security model](../security-model.md)

This page covers what keeps hostile code and markup out of the webview, and what a window may do. The production CSP admits no remote script, no `eval`, no frames and no form posts, and top-level navigation is restricted to the app origin. Cluster data and a chat answer are drawn as text: the webview renders no HTML from them. No permission is granted to a window ahead of a consumer, and a log window holds only the viewer's commands. The host does not restrict which GraphQL operations it forwards, and the schema's breadth is held by review; both are decisions.

## Protections

| ID | Protection | Status | Residual |
| --- | --- | --- | --- |
| WEB-1 | The production CSP admits no remote script, no `eval`, no frames and no form posts. | **Enforced** | — |
| WEB-2 | Top-level navigation is restricted to the app origin: `build_window` installs `on_navigation` and `on_new_window`, both default-deny. | **Enforced** | CSP does not govern navigation. Per-engine firing is verified by hand, not by a test. |
| WEB-3 | The navigation allowlist is `is_app_origin`: the bundled app origins are admitted, and the dev server only by a debug build. | **Enforced** | — |
| WEB-4 | The webview renders no HTML from cluster data: no `innerHTML`, no `dangerouslySetInnerHTML`. | **Enforced** | — |
| WEB-5 | A chat answer is rendered to React elements, never an HTML string. Raw HTML in it stays text. | **Enforced** | — |
| WEB-6 | Printer columns are evaluated by a restricted reader, not a template engine. | **Enforced** | — |
| WEB-7 | An unverified cluster connection is visible in the UI. | **Enforced** | The sidecar draws no verdict, so the frontend tests are the fence. |
| WEB-8 | No permission is granted to the webview ahead of a consumer: a window calls only the commands its capability allows, and no page emits an event or calls the menu, tray, image, resources, path or app API. An app window holds sixteen commands. | **Enforced** | — |
| WEB-9 | A log window holds the viewer's commands and cannot open a window, change a preference, quit or open the account URL. | **Enforced** | — |
| WEB-10 | The command list in `build.rs` is held to `invoke_handler`'s. | **Enforced** | — |
| WEB-11 | Each log-window command also checks its caller's window kind. | **Enforced** | — |
| WEB-12 | A webview's subscriptions are torn down by the host on reload and close. | **Enforced** | — |
| WEB-13 | `tauri.conf.json` declares no windows. Chrome decisions stay in `build_window`. | **Enforced** | — |
| WEB-14 | No GraphQL field is served ahead of a consumer. | **By decision** | A mutation with no caller is what review is for. |
| WEB-15 | The host forwards only operations the app ships. | **By decision**, not built | CSP reduces injection risk but does not restrict the authority of permitted bundled code. |
| WEB-16 | No window may send an OS notification. The host shows each monitoring alert as fixed text around the subject's, target's and signal's labels, each stripped of what draws as nothing and bounded. | **Enforced** | A label within its bounds shows as the cluster wrote it; macOS's center is checked by hand in a signed build. |
| WEB-17 | A Linux notification's body reaches the notification server with `&`, `<` and `>` escaped, so a label draws no link and no bold text on a server that reads markup. | **Enforced** | — |
| WEB-18 | A notification's press hands a page an incident's id and nothing else, and only the page of the window it surfaced takes it: `notification_press_take` reads the caller's label off its `Webview`. | **Enforced** | — |

## Evidence

### WEB-1

- **Where:** `src-tauri/tauri.conf.json`
- `production_csp_admits_no_remote_code` — reads the shipped config: `script-src` is `'self'` alone, the four `'none'` directives hold, and no directive admits `unsafe-eval` or an origin off this machine.

### WEB-2

- **Where:** `src-tauri/src/window_manager.rs`
- `build_window_installs_the_navigation_policy` — `build_window` installs `on_navigation` and `on_new_window`.

### WEB-3

- **Where:** `src-tauri/src/window_manager.rs`
- `the_bundled_app_origins_are_admitted`, `everything_that_is_not_the_app_is_refused`, `the_dev_server_is_admitted_only_by_a_debug_build` — the allowlist, `is_app_origin`.

### WEB-4

- **Where:** `src/`
- `lint/oxlint-plugin.test.ts` — pins each ban the config gives the rule.
- Note: the rules are `kubetail/no-restricted-syntax` (`lint/oxlint-plugin.js`), and `react/no-danger` and `no-restricted-imports` in `.oxlintrc.json`.

### WEB-5

- **Where:** `src/components/primitives/markdown/markdown.tsx`
- `markdown/markdown.test.tsx` — pins a tag rendering as its characters and a link drawing without an `href`.
- Note: the transcript test pins the same through `ChatTranscript`.
- Note: the model's text is cluster data at one remove, so it sits behind the same lint rules.

### WEB-6

- **Where:** `src/lib/jsonpath.ts`
- `src/lib/jsonpath.test.ts` — pins the reader.
- Note: the `custom/cluster-data-is-text` config object refuses a template engine, by import and by call.

### WEB-7

- **Where:** `src/lib/kube-config.tsx`, `sidecar/internal/services/cluster/clusters.go`
- `src/lib/kube-config.test.tsx` — `tlsUnverifiedReason`'s tests.
- `src/components/cluster/cluster-picker/list.test.tsx` — the cluster picker list's badge test.
- Note: the omnibox's two badge tests pin it too.
- Note: `src/lib/kube-config.tsx` holds the verdict, and `services/cluster/clusters.go` the mirrored entry.

### WEB-8

- **Where:** `src-tauri/capabilities/default.json`, `src-tauri/capabilities/logs.json`, `src-tauri/build.rs`
- `default_capability_grants_what_the_app_window_calls` — pins the app window's permission list.

### WEB-9

- **Where:** `src-tauri/capabilities/logs.json`
- `logs_capability_grants_the_viewer_and_nothing_else` — pins the log window's permission list.

### WEB-10

- **Where:** `src-tauri/build.rs`
- `every_command_is_in_the_app_manifest` — holds `build.rs`'s command list to `invoke_handler`'s.

### WEB-11

- **Where:** `src-tauri/src/commands.rs`
- `a_log_window_command_is_refused_to_the_wrong_kind_of_window` — each log-window command checks its caller's window kind.

### WEB-12

- **Where:** `src-tauri/src/lib.rs`
- `cancel_webview_drops_only_that_webviews_subscriptions` — only that webview's subscriptions are dropped.

### WEB-13

- **Where:** `src-tauri/src/window_manager.rs`
- `window_manager.rs` — the unit test in it.

### WEB-14

- **Where:** `sidecar/graph/schema.graphqls`
- **Records:** [the schema's breadth is held by review](../adr/2026-09-03-schema-breadth-is-held-by-review.md)

### WEB-15

- **Records:** [no GraphQL operation allowlist](../adr/2026-09-03-no-graphql-operation-allowlist.md), [TODO](../TODO.md#security)
- Note: what the schema's breadth costs is S-9's other half, in the TODO.

### WEB-16

- **Where:** `src-tauri/src/alerts/mod.rs`, `src-tauri/capabilities/default.json`, `src-tauri/capabilities/logs.json`
- `no_window_may_send_a_notification` — neither capability grants a notification permission.
- `alert_body_strips_control_characters_and_bounds_each_part` — a newline, an escape or 10,000 bytes yields one bounded line.
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)

### WEB-17

- **Where:** `src-tauri/src/alerts/mod.rs`
- `a_linux_body_escapes_markup_in_labels` — a label's link and bold tags reach the server as text.
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)

### WEB-18

- **Where:** `src-tauri/src/window_manager.rs`, `src-tauri/src/commands.rs`
- `notification_press_take_takes_the_callers_press` — another window's page takes nothing, and a take clears the press.
- `a_press_waits_in_the_app_window_last_focused` — the press waits for the window it surfaced, a log window's focus aside.
- **Records:** [monitoring alerts](../security/2026-11-27-monitoring-alerts.md)
