---
title: Webview records get their own log file
date: 2026-09-08
scope: host
status: Accepted
---

# Webview records get their own log file

## Context

`webview_log.rs` re-emits every record the webview reports as a host `tracing` event under
`target: "webview"`, so it landed in `main.log` beside the host's own events. That file is a
2 MB window with five archives.

The webview is the loudest producer the host has. A component throwing on every frame, or a
subscription flapping against an unreachable cluster, fills that window and every archive behind
it — evicting the sidecar-spawn and startup lines, which are exactly what a user is asked for
when the app will not start. The per-webview rate limiter blunts this, but it is pacing one
producer to protect an unrelated reader's history; the target makes the lines *filterable* and
does nothing about them being *evicting*.

## Decision

The host writes two files. `main.log` holds its own events; `webview.log` holds records carrying
`logging::WEBVIEW_TARGET`, with the same JSON shape, size cap and archive count. The two are
exclusive — a record written to both is one a reader has to skip twice — so the webview's budget
is its own and the limiter is back to bounding churn alone.

Stderr is unsplit: it carries both, and stays the one interleaved stream a `pnpm tauri dev` run
reads.

**The split is made by the writers, not by a per-layer filter.** `file_layer` builds a JSON fmt
layer per file, each over a writer filtered on `Metadata::target`. A `Filtered` layer registers
itself with the registry as the subscriber is built, and the file layer arrives later through
`reload` — `Handle::reload` documents that it cannot be used with `Filtered`. Routing inside the
`MakeWriter` needs no registration and keeps the layer's type as the one `ReloadHandle` names.

A layer each, rather than one over a combined writer, so `webview.log` can be written
`.with_target(false)`: the target is the routing key, and repeating it on every line of a file
that holds nothing else says only what the filename does. `main.log` keeps it, where the target
is the emitting module. The sidecar drops its own `target` field for the same reason —
`sidecar.log` holds nothing else, and the host labels the pipe lines it renders itself.

## Consequences

- A subscription failure has its host-side cause in `main.log` and its webview-side symptom in
  `webview.log`, correlated only by timestamp. The sidecar split already has this property
  ([Two processes, two log files](2026-09-08-two-processes-two-log-files.md)), and stderr still
  interleaves all three in a dev run.
- "Attach your log" is now two paths on a webview fault. `main.log` is still the one to ask for
  first: it holds the startup, spawn and shutdown lines.
- `install_file_layer` returns a `Vec<WorkerGuard>`, and `AppState::flush_log` drops all of them
  — a guard left behind loses the tail of its file, since the process exits without dropping
  managed state.
