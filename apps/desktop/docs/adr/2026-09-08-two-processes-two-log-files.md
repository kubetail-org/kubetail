---
title: Two processes, two log files
date: 2026-09-08
scope: cross-cutting
status: Accepted
amended_by:
  - [JSON in both log files, text rendered by the host](2026-09-08-json-logs-rendered-by-the-host.md)
  - [The sidecar places Kubetail's folders itself](2026-11-23-the-sidecar-places-kubetails-folders-itself.md)
---

# Two processes, two log files

## Context

The sidecar wrote JSON lines to stderr and the host did the rest: it parsed every line, mapped
the level, re-nested the fields under `sidecar.fields`, and re-emitted the line as one of its own
events. The host's `main.log` was the only file. That works while the host is the only thing
that runs the sidecar.

It is not going to be. The sidecar is going to run as a daemon that a CLI and an MCP client
reach directly. A client that *spawns* the sidecar could read its stderr as the host does, but a
detached daemon has nobody holding its pipes, so it has to keep its own log.

Two smaller things came with the old arrangement. Every sidecar line was stamped with the time
the host received it, because the host parsed the sidecar's `time` and threw it away. And the
host carried a parse-and-classify layer whose only job was to undo the JSON encoding the sidecar
had just done.

## Decision

The sidecar writes its own file. `internal/lib/logging`'s `Open(path, stderr, level)` builds the
writers two new flags describe — `--log-file`, `--log-stderr`, both off by default — and hands
them to `Init`. Rotation is lumberjack, at the host's numbers (2 MB, five archives). The host
passes `<log_dir>/sidecar.log`, so `main.log` and `sidecar.log` sit side by side, rotate the
same way, and stamp records with the same clock: UTC, RFC 3339, microseconds. The sidecar's
handler is now text rather than JSON, because the file is for a person to open.

The host stops parsing. `logs.rs` keeps one rule, and the build picks the sink. In a release
build the pipes carry only what the sidecar's own logger could not — a Go panic, its report that
it could not open its file, or the whole stream when the host had no directory to give it — and
those go into `main.log` at `warn`, sanitized onto one line, under `target: "sidecar"`. In a
debug build the host also passes `--log-stderr`, so the pipe is the sidecar's whole log, already
quoted; the host writes it straight to its own stderr and nothing to `main.log`.

`main.log` gains the sidecar's lifecycle from the host's own side: `service.rs` logs the spawn
(with the pid and the log path it passed), the arrival of `READY`, and the exit.

## Alternatives considered

**Leave it as it is, and let the daemon's clients read its pipes.** This is the whole reason the
change exists: a detached daemon has no parent holding a pipe, so its log would have nowhere to
go.

**Keep one file by having the sidecar write into `main.log` directly.** Two processes appending
to one rotating file means two rotators, and lumberjack and `tracing-appender` would each rename
out from under the other. A shared file needs a lock protocol neither library offers.

**Filter the sidecar's pipes out of `main.log` entirely in release.** `docs/TODO.md` sketched
this. It would also drop the Go panic, which is the one sidecar line a user cannot get any other
way — a sidecar that dies before it opens its file leaves nothing behind at all.

**Keep the JSON encoding and let the host keep parsing for the terminal.** The parse exists only
to undo the encoding; text on both sides is what a reader wants, and the host's debug path then
costs one `writeln!`.

## Consequences

A failed startup is now two files. They are in one directory, rotate at the same size, and share
a clock format, so lining them up is mechanical; in development `--log-stderr` puts both on one
terminal.

The sidecar now owns the one-line-per-record guarantee that the host's `sanitize_text` used to
provide for it. The text handler's quoting is what provides it, and
`TestInitWritesOneLinePerRecord` is what keeps it — a handler swapped back to something that does
not quote would let cluster-controlled text forge a log line. The cap also moved to before the
escaping: `safe.String` cuts at 2048 bytes and the handler quotes after, so a string of control
characters can reach roughly 8 KB on the line.

`Init` stays exported because `internal/lib/testutil`'s `CaptureLogs` builds the default logger over
a buffer with it, which is how the redaction sentinels read what the process would have written.

## Revisit when

The sidecar grows a second consumer that wants structured records off the file — a log shipper,
or the app reading its own log. Text is a choice for human readers; a machine reader would argue
for JSON in the file and text on stderr.
