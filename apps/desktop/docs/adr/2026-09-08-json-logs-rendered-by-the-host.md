---
title: JSON in both log files, text rendered by the host
date: 2026-09-08
scope: cross-cutting
status: Accepted
---

# JSON in both log files, text rendered by the host

## Context

[Two processes, two log files](2026-09-08-two-processes-two-log-files.md) gave the sidecar its own
file and made both processes stamp records with the same clock. It left the two lines looking
nothing alike — the host wrote `tracing`'s default, the sidecar wrote `slog`'s — which is most
obvious in a debug build, where both streams land on one terminal and the level sits in a different
column on alternating lines.

Matching them by hand does not work. Neither ecosystem can produce the other's shape from a
built-in: `slog` renders every field as `key=value`, including the three built-in ones, and
`ReplaceAttr` can change a value but never the encoding. Writing a `slog.Handler` to emit
`tracing`'s shape is about ninety lines, and it moves a security invariant into the sidecar — the
message is written bare, so quoting it correctly becomes the only thing keeping cluster-controlled
text from forging a line.

That terminal has a second problem the format alone would not fix. Three parts of the app write to
it — host, sidecar, webview — and only `tracing`'s target distinguished them, in a column whose
width varies with the module path.

## Decision

**Both files are JSON, from each ecosystem's own encoder.** The sidecar uses `slog.JSONHandler`
with a `ReplaceAttr` that renames `time`→`timestamp` and `msg`→`message` and formats the timestamp;
the host uses `tracing_subscriber`'s `.json().flatten_event(true)`. Renaming keys is what
`ReplaceAttr` is for, so no hand-written encoder survives on either side, and a record from either
process is one object with the same keys.

**The host renders the only human-readable stream.** The sidecar's pipe carries the same JSON, and
`logs.rs` parses each line and re-emits it as an event of the host's own, at the level the record
names, under `logging::SIDECAR_TARGET`. `tracing` then stamps, levels, filters and colours it
exactly as it does a host event. Where it lands afterwards is the file layer's decision, not
`logs.rs`'s: `logging::is_host_log` keeps a forwarded line out of `main.log` in a debug build, where
`sidecar.log` already holds it, and lets it through in a release build, where the pipes carry only
what the sidecar's logger could not write.

**A line that is not a record passes through at `warn`.** A Go panic and the sidecar's own report
that it could not open its file reach the pipes precisely because its logger did not write them.
Parse failure is not an error condition; it selects the pass-through path. Nothing says how serious
such a line is, and neither of the two that reach us is routine. Each arrives on its own pipe read,
so a panic stays one line per line.

**The terminal's format is the host's own** — `TerminalFormat`, a `FormatEvent` impl:

```
2026-09-08T13:29:49.726026Z  INFO [sidecar] sidecar starting pid=95679 socket="/tmp/x.sock"
2026-09-08T13:29:49.725958Z  WARN [main]    kubetail_app_lib::tray: auth-state tray watch failed
2026-09-08T13:29:49.726031Z  WARN [webview] Failed to fetch window="main" source="graphql"
```

The timestamp leads, as it does in every other tool that prints logs, and the tag naming the part of
the app that wrote the line is padded to one width so the column can be scanned. A host line keeps
its module after the tag; a forwarded line has none of its own, and its target is the name the tag
already carries. The two are the `service.name` and `log.logger` of the OpenTelemetry conventions,
and they answer different questions.

This is a formatter of our own, which the `slog.Handler` above was rejected for being. The reasons
do not carry: that one would have lived in the process holding every credential, with the
bare-message quoting rule the only thing standing between cluster text and a forged line. This one
renders the terminal alone — the files stay JSON from each ecosystem's own encoder, the message is
sanitized before it is emitted, and a bug in it costs a misaligned column rather than an invariant.

## Alternatives considered

**A `slog.Handler` emitting `tracing`'s shape.** Built and rejected. It works, but it is ninety
lines of formatter in the process that holds every credential, and the bare-message quoting rule
becomes ours to keep right forever.

**A third-party console handler** (`lmittmann/tint`, `phsym/console-slog`). None emits `tracing`'s
shape either, so each needs the same customization *plus* a dependency.

**Move the host to `slog`'s logfmt shape instead.** `tracing`'s format is what every Rust reader
recognises, and matching it from Rust needs `tracing-logfmt`, a third-party crate — the same trade
in the other direction.

**Leave the two formats alone.** What this exists to fix.

**Render the forwarded line with `format!` rather than re-emitting it as an event.** Chosen first,
and reversed the same day. The objection was that a `tracing` event reaches every layer, so the
sidecar's whole log would be stored in `main.log` as well as its own file — which
[webview records get their own log file](2026-09-08-webview-records-get-their-own-log-file.md)
dissolved by making the destination a per-file routing decision. Two lesser objections stood and
were accepted rather than answered: an event is stamped when *received*, and `tracing` takes only
static field names. What the hand-written line cost was worse than either — it bypassed
`EnvFilter`, so `KUBETAIL_LOG_LEVEL=warn` silenced the host and left every sidecar `INFO` on screen,
and it carried no ANSI, so the sidecar's lines were the plain ones between coloured ones.

**Tag the line with a `MakeWriter` instead of a formatter.** `make_writer_for` is handed the
event's metadata, so the tag needs nothing else, and the stock format survives untouched — but a
`MakeWriter` only wraps the writer the formatter is handed, so it can prepend and nothing else. That
puts the tag ahead of the timestamp, displacing it from the left edge and spending ten columns
before anything informative.

## Consequences

Neither file is pleasant in a plain editor any more; reading one directly wants `jq`. That
supersedes "the file is text, not JSON, because the file is for a person to open" in the previous
ADR — the person now reads the *terminal*, which the host renders, and reaches for a file only when
a run is already over.

**The one-line invariant lives in the host.** JSON encoding escapes control characters by
construction on both sides, so nothing hand-written guards either file. What needs guarding is the
line the host emits, because `serde_json` hands back a *decoded* message with its control characters
real again — `logs.rs` sanitizes before emitting, and `a_forwarded_message_stays_on_one_line` pins
it.

**A forwarded line is stamped when the host receives it.** The sidecar's own timestamp is not lost —
`sidecar.log` has it, written by the process that knows it — so the file stays the record of when
something happened, and the terminal is ordered by one clock rather than two that can invert
neighbouring lines.

**The host's log level governs the sidecar's forwarded lines too**, since they pass `EnvFilter` like
any event. The sidecar inherits `KUBETAIL_LOG_LEVEL` from the host that spawned it, so both ends
already moved together and one knob now means what it says.

**`TerminalFormat` does not render spans**, which the stock format does and the host does not use.
The first `#[instrument]` in the host needs it added.

`logs.rs` parses, which the previous ADR removed. The justification is different: that parse existed
to undo an encoding and rebuild the host's own file, and this one exists only to recover a level and
a message. The release path still carries a panic to `main.log`.

## Revisit when

The host starts using spans.

Or something other than a person reads these files — a log shipper, or the app reading its own log.
That was the previous ADR's trigger too, and it is now met halfway: the files are already
structured, so such a reader would need no format change, only a decision about retention.
