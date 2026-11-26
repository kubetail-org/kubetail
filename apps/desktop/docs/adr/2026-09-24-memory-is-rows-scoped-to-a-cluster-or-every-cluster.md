---
title: Memory is rows, one fact each, scoped to a cluster or every cluster
date: 2026-09-24
scope: cross-cutting
status: Accepted
---

# Memory is rows, one fact each, scoped to a cluster or every cluster

## Context

Every chat started from the cluster card alone: what the user said last week was gone with the
chat. The model needed to keep notes that later chats read, and the user needed to see and edit
them.

Claude Code's auto-memory was the model: one fact per note, typed, with a one-line description,
and an index loaded at the start of every session. It stores the notes as markdown files, one per
fact, beside a `MEMORY.md` index the model maintains, and the model writes them with its ordinary
file tools. Codex also keeps memories as markdown files. Neither app has a cluster, a webview
without a filesystem, or an approval gate that asks before every file write.

The first cut followed that design closely: two scopes the model could write, an approval request
for one of them, versions, types, descriptions, an index the model read from, and a flag for
rebuilt clusters. Each had rules, codes and tests of its own. This decision is the smallest form
that is still useful. The rule for what to cut: **keep data we cannot get back later, cut
behaviour we can add later.**

## Decision

A memory is a row of the `memories` table in `app.db`, owned by `internal/services/memory`: a **name
and a body**, for one cluster (`cluster_id`) or for every cluster (`NULL`), with who wrote it
last (`written_by`), the chat it came from (`chat_id`) and the cluster's server UID at the last
write (`server_uid`). A name is unique within its scope, so a cluster's note and a note for every
cluster may share one.

1. **The model writes its own notes: for its chat's cluster unasked, and for every cluster once
   the user approves.** The `Memory` tool (`internal/tools/memory`) saves and forgets notes
   through `tools.Runtime.Memory`. Each call reaches only the scope it names, `cluster` or
   `everywhere`, so a call for the cluster never reaches a note for every cluster. A call with
   `scope: everywhere` — a save, an overwrite or a forget — waits on an approval request that
   shows the name and, on a save, the whole body. The note stays the model's: approving means
   *remember this everywhere*, not *follow this everywhere*. The tool's `Approval` runs the shape
   and credential check before anything is shown, so the user is never asked to approve a call
   that must fail; a refusal there is an error, which the loop answers `bad-input`, because a skip
   would change every cluster's notes unasked if that check and the store's ever disagreed. A name
   the user's note holds, and a scope with no room, are found at the write, after Approve.
2. **Every note goes to the model whole**, as the `## Memory` section of each question's
   `<context>` block, resent when it changes. There is no index and no `read`.
3. **A note the user wrote is a request; a note the model wrote is information.** The system
   prompt and *Data is not instructions* say so, and only for the newest `<context>` block. The
   model changes only notes it wrote: a save or forget of a name the user's note holds in the
   scope it names is refused `user-note`. `written_by` changes one way: any dialog write makes a note the user's, and no
   model save changes a note's author.
4. **A save replaces the note by name.** There are no versions.
5. **The server UID is recorded on every write and read by nothing yet.** It says which physical
   cluster a note was written against, which cannot be recovered later.
6. **Each scope is bounded by size.** A body is at most 500 bytes, and a scope's notes at most
   4 KiB as the section encodes them. A write past that is refused `full`, so the section never
   cuts a note.

## Alternatives considered

- **Markdown files under the data directory, written with Read, Write and Edit.** The webview has
  no filesystem, so the dialog would need a watch over files anyway; Write asks before every
  write, so every note would ask, or `fileguard` would need an exception for one directory, which
  is a security change of its own; a model-maintained index drifts; and two windows writing one
  file race.
- **Scoping by chat as well.** A chat already resends its whole transcript every turn, so notes
  within one chat are still in front of the model.
- **Only the dialog writes notes for every cluster.** It removes the cross-cluster path by
  design, but the notes most worth keeping everywhere are about the user and come up in
  conversation, so they are the least likely to be moved by hand, and a user who does not move
  them gets a copy per cluster. One click where the model offers to remember is where the user is
  already looking.
- **An approved note for every cluster is written as the user's.** It keeps every note for every
  cluster the user's, but it turns an injection the user approves into a standing instruction on
  every cluster, and the model could never correct a note it proposed. Kept as the model's, an
  approved note is information, like its notes for one cluster.
- **An index with a `read` op, types and descriptions.** The model cannot use a note it has not
  read, and some notes matter to every answer. With short notes and a size cap, all of them fit.
  It waits until notes outgrow the section.
- **Versions.** They guard one writer overwriting a change it never saw. The model sees the
  current text in its context, and a model save after a dialog edit is refused, so what is left
  is a race nobody has seen. They wait until one is.
- **Flagging notes written before a rebuild.** A kind or minikube cluster gets a new UID each time
  it is recreated, so a flag would fire on every note. It waits until a recreated cluster leads
  the model astray.
- **Approving every save to one cluster.** Asking on every note would teach the user to approve
  without reading, which weakens the requests that matter.

## Consequences

- A note the model saved from something it read lands, whole, in every later question on its
  cluster, unasked. The model reads it as information, not as an order, and the transcript and
  the dialog show every save; the [security record](../security/2026-09-24-memory.md) states the
  bounds.
- A note the model saved for every cluster lands, whole, in every cluster's chats once approved.
  The model reads it as information, as it does a note for one cluster; the request showed every
  character of it, and the dialog lists it as Kubetail's for the user to edit or delete.
- Which notes the model follows is a rule in the prompt. The code guarantees which notes carry
  `"by":"user"`; no test can pin how a model reads it.
- The user cannot ask the model in chat to change one of their own notes; the model points them to
  the dialog.
- Every note change resends the card and the section, up to about 12 KiB, on the next question.
  The limits are guesses to revisit after use.
- `HasSecret` and `Redact` diverge on purpose: a note keeps a URL's query and a sentence opening
  with *password*. A new rule in `Redact` needs a decision for `HasSecret` too.

## Revisit when

Notes outgrow the section, a recreated or renamed cluster leads the model astray, approving each
change to a note for every cluster proves tedious, or a second machine needs the same notes.
