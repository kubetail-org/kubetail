# Security record — Log windows, 4 November 2026

**Subject:** a log view can move from an app window's sidebar into a window of its own
(`logs-N`), paired by the host to the app window that opened it. The host keeps the pair
(`src-tauri/src/log_windows.rs`); the log window draws the viewer (`src/components/widgets/logs-window.tsx`)
and pushes its viewport to the app window, whose composer can send it
(`src/lib/log-window-push.ts`, `src/lib/log-windows.tsx`). The decision is
[a log window belongs to one app window](../adr/2026-11-04-a-log-window-belongs-to-one-app-window-and-the-host-keeps-the-pair.md);
the living model is [security-model.md](../security-model.md).

## What widens

**A second window kind holds the whole GraphQL surface.** The log window reads logs through
`graphql_query`, which forwards operations unexamined. The CSP stays the containment, as for an app
window.

**The app window gains four commands** (`log_window_subscribe`, `log_window_open`,
`log_window_focus`, `log_window_close_all`), each acting only on its own log windows.

**Log lines cross from one window to another.** A log window's viewport, its selected lines
included, reaches its app window through the host and can ride a send from the chat.

**Cluster and model text reach the OS.** A log window's native title names its first source, the
chat's title and the cluster.

## What narrows

**Every host command needs a capability allow**, and no page can emit an event or call the menu,
tray, image, resources, path or app API. A log window cannot open a window, change a preference,
quit or open the account URL.

## What holds it

- **The capabilities are pinned.** `default_capability_grants_what_the_app_window_calls` and
  `logs_capability_grants_the_viewer_and_nothing_else` pin the two lists, and
  `every_command_is_in_the_app_manifest` holds the command list to `invoke_handler`'s.
- **A page never names a target window.** Every lookup starts from the caller's label, and each
  command checks its caller's window kind beside the capability
  (`a_log_window_command_is_refused_to_the_wrong_kind_of_window`).
- **What the host tells an app window rides a channel that page made.** No other page hears it,
  and a command naming a page that ended changes nothing
  (`own_frames_reach_the_paired_app_window_alone`, `page_an_ended_page_is_refused_by_every_app_window_call`).
- **A log window cannot outlive its app window.** The host destroys the log windows it closes,
  so no page can refuse the close (the lifecycle tests, `the_host_destroys_the_log_windows_it_closes`).
- **A push is bounded and stamped.** The page carries at most 192 KiB of selected lines
  (`cutSelection`'s tests), the host refuses a push over 256 KiB or from an earlier page load
  (`own_every_bound_is_refused`, `own_a_push_from_a_stale_generation_is_refused`). Messages cross
  whole, so the sidecar redacts each whole one before the model reads it. The app window draws a
  push only as a count in the chip, never as HTML.
- **The title is built by the host** from at most three parts, each stripped of invisible
  characters and cut to 60 characters (`window_title`'s tests, `own_every_bound_is_refused`).
- **A view travels in the host's pair**, never in `localStorage`, which every window shares. The
  log window's bar draws the chat's title and the cluster's name through `VisibleText`.

## Residuals

- **A compromised log window page can do to its cluster what a compromised app window page can**:
  it holds the same GraphQL surface.
- **Selected lines past the page's bound do not ride**, though the sidecar could have sent them to
  the model. This is a transport cost, not a protection.
