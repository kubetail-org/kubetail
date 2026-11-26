---
title: The user's variables reach commands on either side of the sandbox
date: 2026-11-20
scope: sidecar, webview
status: Accepted
---

# The user's variables reach commands on either side of the sandbox

## Context

A sandboxed command's environment is one built table, so a user had no way to give their commands
a variable such as `HELM_NAMESPACE` or `KUBECTL_EXTERNAL_DIFF` ([#1080](https://github.com/kstackhq/kstack/issues/1080)). The first design added
variables for sandboxed commands alone, under Settings › Sandbox.

That tied the variables to the sandbox, and three things did not fit. A chat the user switches
outside the sandbox would lose its variables without a word, and a missing `HELM_NAMESPACE`
changes what a command touches. A machine with no sandbox would get nothing from the feature.
And some variables only make sense on one side: the sandbox never passes `AWS_PROFILE` and sets
`HOME` itself, while outside it both are ordinary.

Outside the sandbox the approval request is the whole gate
([outside the sandbox is the user's choice](../security/2026-09-30-outside-the-sandbox-is-the-users-choice.md)).
A variable changes what an approved command does and is not part of its text.

## Decision

**A variable is the user's, for their commands, and names its target**: commands in the sandbox,
commands outside it, or both. It is scoped to every cluster or to one; a command gets every
cluster's variables with its chat's cluster's value in place of one of the same name.

**The check follows the target.** A name that changes how a program starts or makes a shell run
code, and a name Kubetail sets on every command, is refused for any target. A name the sandbox
never passes or sets itself is refused only for a target that includes the sandbox.

**A sandboxed command reads its variables as it starts.** It runs unasked, and the sandbox bounds
what a variable can do there.

**A command outside the sandbox is shown its variables.** They are read once, when its approval
request is made, kept on the call's row beside its directory (`tool_calls.env`), drawn in the
request in full, and handed to the run from the approval. The run reads the list from nowhere
again, so what the user saw is what the command gets.

**Settings › Environment edits them**, with the scope picker Memory has. Settings › Sandbox ›
Environment lists what a sandboxed command gets and edits nothing.

## Alternatives considered

**Variables for sandboxed commands alone.** The user's own shell profile already reaches a
command outside the sandbox. It lost to the silent loss on a switched chat and to a feature that
does nothing where there is no sandbox.

**Read the variables when the command starts, outside the sandbox too.** One read path. A change
between the request and the approval would then run a command with variables the user never saw
under it.

**Derive the side from the name alone**, so that `AWS_PROFILE` is stored once and reaches only
commands outside the sandbox. The user could not then keep an ordinary variable out of one side,
and a row's reach would change when a list in the sandbox's code did.

**Fold the variables behind a count in the request.** A shorter request, and a gate the user can
approve without seeing what it hides.

## Consequences

An approved command's request grows by one line per variable.

A variable for both sides must pass the sandbox's check, so `AWS_PROFILE` is saved for commands
outside the sandbox alone.

A cluster's variable replaces every cluster's of the same name only where its own target reaches.

Chat-scoped variables and secret values are not part of this. A secret would be kept out of the
command and added at the run's proxy, as a cloud platform's credential is.
