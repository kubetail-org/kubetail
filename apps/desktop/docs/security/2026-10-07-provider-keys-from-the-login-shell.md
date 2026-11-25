# Security record — provider keys from the login shell, 7 October 2026

**Subject:** the sidecar reads the model-provider keys from the launch's run of the login shell
as well as from its environment. The living model is [security-model.md](../security-model.md);
the shell's run is [the login shell in the sandbox](2026-10-04-the-login-shell-in-the-sandbox.md),
and the keys' first path is [cloud providers from the environment](2026-09-12-cloud-providers-from-the-environment.md).

## The problem

A release build opened from Finder, the Dock or a `.desktop` entry inherits a minimal
environment. Keys exported in `~/.zshrc` or `~/.zprofile` are not in it, so the catalog listed no
models, and the composer's *Set an API key, then restart Kubetail* did not help: a restart from
Finder has the same environment. The login shell already ran at launch for `PATH`, but its
allowlist leaves out every secret, since `setShellEnv` sets it process-wide and kubeconfig
credential plugins inherit it.

## What changed

- **The shell is asked for the keys as a second list.** `loginshell.Resolve` takes the names of
  the keys the caller wants, prints each after the allowlist between the same markers, and
  answers them in `Result.Keys`, never in `Env`. `setShellEnv` never sees them.
- **The names are `catalog.KeyVars()`'s**, so the command is still built from constants alone,
  and a key the launch environment set wins over the shell's (`withShellKeys`).
- **Each key is registered with `safe.AddSecret` as `runShell` receives it**, before anything
  logs. `runShell` logs the elapsed time and a fixed fault reason, never a value, and the shell's
  output reaches no log line.
- **`build` merges the keys into `Config.LLMKeys` before the catalog is built**, on macOS and
  on Linux wherever the login shell runs, which there is wherever there is a sandbox.

## Why this is safe

- **No key reaches the process environment** (`TestAKeyFromTheShellIsRedactedAndNeverInherited`
  checks the variable and a child's environment), so no credential plugin, command or browser
  opener the sidecar spawns inherits one. `takeProviderKeys` still clears the launch's.
- **Every key from the shell is redacted** wherever `safe` renders a line
  (`TestAKeyFromTheShellIsRedactedAndNeverInherited`).
- **The keys come back apart from the allowlist** (`TestResolveReadsTheKeysItIsAskedFor`), and
  a key the launch set wins (`TestNewTakesTheProviderKeysTheShellSets`,
  `TestWithShellKeysPrefersTheLaunch`).
- **The shell that prints them is confined as before**: no network, nothing of Kubetail's, nothing
  on the denied-always list. Its stdout is a pipe to the sidecar alone.
- **A key read after startup is not in the sidecar's exec-time environment**, so a process that
  reads another's launch arguments and environment (`KERN_PROCARGS2` on macOS before 27) does not
  see it, where a key from the launch environment is still there (#28).

## What stays

- **A startup file chooses the key**, and so the account a chat is billed to and sent through.
  This is the trust a terminal launch already gives the same files. A key selects an account,
  never an endpoint, so it cannot redirect where a request goes.
- **On macOS with no sandbox** the launch's shell runs unconfined, as before, and on Linux with
  no sandbox it does not run, so a desktop launch there still needs the key in its environment.
- **A nushell login shell answers no keys**: its command reads `PATH` alone.
