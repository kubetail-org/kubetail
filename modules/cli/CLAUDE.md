# cli — the `kubetail` command

A Go CLI built with [Cobra](https://github.com/spf13/cobra): `logs`, `serve`, `cluster` (Kubetail's Helm release, through the Helm library in `modules/shared/helm`) and `config init`. It also installs as a kubectl plugin, where it calls itself `kubectl kubetail` (`getCommandDisplayName`).

**The binary embeds the UI through the Dashboard module.** `make` from the repo root builds the UI, copies it in and builds `bin/kubetail`. A plain `go build` or `go test` here works without a built UI; `serve` then answers 404 for every page. A change in `modules/shared`, `modules/dashboard/pkg/app` or `modules/dashboard/pkg/config` changes this binary.

## Commands

- **Each command is a package-level `var xCmd = &cobra.Command{…}` in a file of its own under `cmd/`**, wired in that file's `init()` with `AddCommand` on its parent. Keep its `Run` body thin: parse flags into a config struct, then call a function that takes the struct.
- **A command's logic is a function tests can call**: `loadLogsCmdConfig(cmd)`, `selectBackend(flag, tryKubetail)`, `loadServerConfig(cmd)`. Tests never execute `rootCmd`; they build a bare `&cobra.Command{}`, add the flags with `addLogsCmdFlags(cmd)` or `addServerCmdFlags(cmd)`, set them and call the function.
- **`headFlag` and `tailFlag` are package variables** shared by every command built with `addLogsCmdFlags`, so a value set in one subtest is still set in the next, and no test resets them. Don't add another flag that way.
- **Flag names are kebab-case.** `applyFlagNormalization` accepts `_` for `-` in any flag and `--context` for `--kube-context`. `-h` is `--head`; `--help` has no shorthand.
- **A fatal error exits the process**: `cli.ExitOnError(err)` in `logs` and `cluster`, `zlog.Fatal()` in `serve` and `config init`. Neither returns, so code behind one cannot be tested: decide in a function that returns an error and exit in `Run`.
- **The update check runs around every command** (`PersistentPreRunE`, `PersistentPostRunE`) and is skipped when the version is `dev`.

## Config

- **Precedence is flag, then file, then `DefaultCLIConfig()`.** The file is `~/.kubetail/config.yaml` unless `--config` names another. No environment variable is read by name; the file's text goes through `os.ExpandEnv`, so a literal `$` in a value is consumed.
- **`config/default/cli.{yaml,toml,json}` are the source and `assets/config.*` are copies**, which `config init` writes out. After editing one, run `go generate ./...` here and commit both. Nothing checks they match.
- **`cluster` commands read `--kubeconfig` from the flag alone**, not the config file.

## logs

- **A source is `[namespace:]<type>/<name>[/<container>]`**, a bare pod name, or `*` for a name or container. Parsing and the type aliases are in `modules/shared/logs`.
- **`--backend auto|kubetail|kubernetes`** picks the reader in `selectBackend`. `kubetail` is `internal/clusterapi`: GraphQL through the kube-apiserver, a POST to fetch and a `graphql-transport-ws` WebSocket to follow. `kubernetes` is `logs.NewStream` from `shared`. In `auto` a 404 from the probe means the Cluster API is not installed and falls back with a warning; any other error is fatal.
- **The Cluster API client uses client-go's transports** (`rest.TransportFor` and its WebSocket round-tripper), so the kubeconfig's credentials and TLS apply. A plain `http.Client` or dialer would drop them.
- **`--grep` runs on the server with the Cluster API and in the client without it**, with a warning.
- **Records go to `cmd.OutOrStdout()`; warnings and paging cursors go to stderr.** `cli.ExitOnError` prints its error to stdout.
- **Which records**: `--head`, else `--tail`, else `--all`, else head when `--since` is set, else tail. The three are mutually exclusive.

## serve

`loadServerConfig` starts from the Dashboard's `DefaultConfig()` and sets the environment (`desktop`, or `cluster` with `--in-cluster`), the address, the kubeconfig, the CLI's version and the local storage directory. If the port is taken it listens on a free one, unless `--port` was given: then it exits with an error. `--test` loads the config, prints `ok` and exits without starting a server; CI's Alpine build uses it as a smoke test.

## Tests

- **Seams are fields and interfaces.** `updatecheck.Options` takes the `Checker`, the `HelmLister`, the cache paths and a `Runner` (a test passes one that runs synchronously). `internal/clusterapi` has `newClientForTest` and `newStreamForTest`, and `Stream` talks to an unexported `clientIface` a test fakes.
- **A test that loads config sets `HOME` to a `t.TempDir()`** (`t.Setenv`), or it reads the developer's own `~/.kubetail/config.yaml`.
