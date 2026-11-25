# Security record — the user's variables, 20 November 2026

**Subject:** the user can give their commands environment variables, from Settings ›
Environment: for every cluster or one, and for commands in the sandbox, outside it, or both. They
are rows of `app.db`'s `env_vars` table, owned by `internal/services/envvars`
([rows](../adr/2026-11-20-a-setting-with-scopes-is-rows.md),
[either side of the sandbox](../adr/2026-11-20-the-users-variables-reach-commands-on-either-side-of-the-sandbox.md)).
The living model is [security-model.md](../security-model.md); the table the variables join in
the sandbox is [the sandbox's own environment](2026-10-02-the-sandboxs-own-environment.md).

## What it widens

**In the sandbox.** Every sandboxed run reads the variables whose target includes the sandbox:
each chat's and subagent's commands, a background command, a monitor's commands, and the
executable probe, since all of them start through one `sandboxedRunFor`. A run of a chat reads
its cluster's too; the probe has no cluster. A value can change what a tool does or where it
connects: `HELM_NAMESPACE` moves a release, `KUBECTL_EXTERNAL_DIFF` names a program `kubectl
diff` runs. Each runs inside the sandbox, under the run's policy.

**Outside the sandbox.** A command there gets the variables whose target includes it, over the
environment Kubetail runs with. The same `KUBECTL_EXTERNAL_DIFF` or `GIT_SSH_COMMAND` names a
program that runs with the user's full access. The approved text is no longer everything that
decides what the command does.

A value is not a secret. It is written in `app.db` in the clear, in a call's row once shown, and
any command can print it, so what the model reads can include it. The form says so, and Kubetail
keeps no value in the keyring.

## What holds

- Only the user writes the list: `envVarSet` and `envVarRemove`, from Settings. No tool and no
  model output reaches either, and no file holds the list for a hand edit.
- `envvars.Check` checks a variable against its target on write and again on every read
  (`TestCheckRefusesForEveryCommand`, `TestCheckRefusesForASandboxedCommandAlone`,
  `TestSetRefusesWhatTheCheckRefusesAndWritesNothing`). A name is a shell name of at most 128
  bytes; a value holds no NUL and is at most 4096 bytes; a scope holds at most 100
  (`TestAFullScopeTakesNoNewVariable`). Names below are matched without case, since Windows
  reads `Path` as `PATH`.
- **Refused for every command**: every `LD_`, `DYLD_` and `MALLOC` name, glibc's other unsecure
  variables (`GCONV_PATH`, `LOCPATH`, `RES_OPTIONS`, …) and the Go runtime's (`GODEBUG`,
  `GOMEMLIMIT`, …), which change how a program starts and would reach the sandbox's launchers;
  `BASH_ENV`, `ENV`, `PROMPT_COMMAND`, `SHELLOPTS`, `BASHOPTS` and `PS4`, which make a shell run
  code on its own; and `PATH`, `PWD` and `KUBETAIL*`, which Kubetail sets on every command. A user who
  wants one writes it in the command, where the request shows it.
- **Refused for a sandboxed command**: a name `sandbox.Unpassable` refuses (`SSH_AUTH_SOCK`,
  every `AWS_` and `AZURE_` name), one the run's table sets itself (`sandbox.KubetailSets`: `HOME`,
  `TMPDIR`, `KUBECONFIG`, the tool-home and toolchain variables, each cloud platform's mount's,
  the proxy variables), and `REQUEST_METHOD`, which turns Go's proxy lookup off. `KubetailSets` is
  kept whole by tests (`TestTheSandboxedEnvironmentIsFixed`,
  `TestKubetailSetsEveryToolchainVariable`, `assertNoUserCanSet` in each mount's test).
- In a sandboxed run the variables follow `LANG` and `TZ`, before `TERM` and the rest Kubetail sets.
  A run reads them once, before anything is made, and one that cannot read them fails
  (`TestARunThatCannotReadTheUsersVariablesFails`).
- **What the request shows is what runs.** A command outside the sandbox reads its variables when
  its approval is made (`TestApprovalOutsideCarriesTheUsersVariables`); they are written on the
  call's row with its directory, drawn in the request each on a line of its own through
  `VisibleText`, none folded, the value's whitespace kept and a newline at either end of it or
  whitespace ending a line spelled, since a newline can separate commands (`lists every variable
  a command outside the sandbox gets`, `spells the edges of a value`), and
  handed to the run from the approval, which reads them from nowhere again
  (`TestTheToolCallRowHoldsTheVariablesItWasShownWith`,
  `TestAnApprovedCommandGetsTheVariablesItWasShownWith`). A variable changed after the request
  does not reach the command. Variables that cannot be read refuse the call before any request.
- A variable replaces the same name in the environment Kubetail runs with, and never one Kubetail
  adds: on Windows the block is built keeping the last entry of each name
  (`TestLastOfEachNameKeepsTheLaterEntryInOrder`).
- A stored row the check refuses, such as one an older build kept under a looser check, is listed
  with its `refusal`, given to no command, and removed like any other
  (`TestARefusedRowIsListedWithItsReasonAndGivenToNoCommand`). A cluster's refused value still
  replaces every cluster's, so the command gets neither (`TestARefusedValueIsCheckedForItsSide`).
- A cluster's variables go with it (`TestMigration0004AddsTheUsersVariables`,
  `TestAClustersVariablesGoWithIt`).

## Residual risk

In the sandbox, a variable can point a tool at a program, a host or a config file the sandbox
lets it reach; with network on, an endpoint override can send that tool's requests elsewhere. The
sandbox's file and network policy still bounds what the run reaches.

Outside the sandbox nothing bounds it but the user's reading of the request. A user who approves
without reading the variables runs the command under them, as one who approves without reading
the command runs it. Code running in the webview can write a variable, as it can call every
other mutation ([security model](../security-model.md)); the request shows that variable too.
