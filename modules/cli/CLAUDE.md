# cli — the `kubetail` command

A Go CLI built with [Cobra](https://github.com/spf13/cobra). It also installs as a kubectl plugin, where it calls itself `kubectl kubetail` (`getCommandDisplayName`). [`modules/CLAUDE.md`](../CLAUDE.md) holds what every Go module follows; the root [`CLAUDE.md`](../../CLAUDE.md), what crosses areas.

- **`kubetail logs [source...]`** prints container logs in the terminal, from the Cluster API when it is installed and from the Kubernetes API when it is not.
- **`kubetail serve`** runs the Dashboard on this machine and opens the browser: the `desktop` environment, or `cluster` with `--in-cluster`.
- **`kubetail cluster install|upgrade|uninstall|list`** and **`cluster repo add|update|remove`** manage Kubetail's Helm release with the Helm library; no `helm` binary is needed.
- **`kubetail config init`** writes a default config file.
- **`kubetail --version`** prints the version the build set, or `dev`.

**The binary embeds the UI through the Dashboard module.** `make` (from the repo root) builds the UI, copies it to `modules/dashboard/website/` and builds `bin/kubetail`. A plain `go build` or `go test` here works without a built UI; `serve` then answers 404 for every page. A change in `modules/shared`, `modules/dashboard/pkg/app` or `modules/dashboard/pkg/config` changes this binary.

## Layout

| Path | Holds |
| --- | --- |
| `main.go` | `cmd.Execute()` and nothing else. |
| `cmd/` | The commands, one file each: `root.go`, `logs.go`, `serve.go`, `cluster.go` and `cluster_<name>.go`, `cluster_repo*.go`, `config.go`, `config_init.go`. |
| `pkg/config/` | The CLI's config: `Config`, `DefaultCLIConfig`, the loader `NewConfig(configPath, v)`. |
| `internal/clusterapi/` | The GraphQL client for the Cluster API (`client.go`) and its adapter to `logs.Stream` (`stream.go`). |
| `internal/tablewriter/` | The streaming fixed-width table `logs` prints. |
| `internal/updatecheck/` | The update notice: cache files, staleness, messages. |
| `internal/cli/` | `ExitOnError`. |
| `assets/` | Embedded copies of `config/default/cli.{yaml,toml,json}`, which `config init` writes out. |

## Commands

- **Each command is a package-level `var xCmd = &cobra.Command{…}` wired in its file's `init()`** with `AddCommand` on its parent (`rootCmd`, `clusterCmd`, `clusterRepoCmd`, `configCmd`). Importing `cmd` also configures the global logger. A new command follows the same shape and keeps its `Run` body thin: parse flags into a config struct, then call a function that takes the struct.
- **A command's logic is a function tests can call**: `loadLogsCmdConfig(cmd)`, `selectBackend(flag, tryKubetail)`, `buildClusterAPIStreamConfig`, `printLogs`, `loadServerConfig(cmd)`. Tests never execute `rootCmd`; they build a bare `&cobra.Command{}`, add the flags with `addLogsCmdFlags(cmd)` or `addServerCmdFlags(cmd)`, set them and call the function.
- **`headFlag` and `tailFlag` are package variables** shared by every command built with `addLogsCmdFlags`, so a value set in one subtest is still set in the next, and no test resets them. Don't add another flag that way.
- **Flag names are kebab-case.** `applyFlagNormalization` accepts `_` for `-` in any flag and `--context` for `--kube-context`. `--kubeconfig`, `--in-cluster` and `--config` are persistent on the root; `--kube-context` is local to each command that takes it.
- **A fatal error exits the process**: `cli.ExitOnError(err)` in `logs` and `cluster`, `zlog.Fatal()` in `serve` and `config init`. Neither returns, so code behind one cannot be tested: decide in a function that returns an error and exit in `Run`.
- **The update check runs around every command** (`PersistentPreRunE` starts a background refresh, `PersistentPostRunE` prints the notice to stderr) and is skipped when the version is `dev`.

## Config

- **The file is `~/.kubetail/config.yaml`** unless `--config` names another; its extension picks the format. A missing default file is fine; a missing `--config` file is an error.
- **Precedence is flag, then file, then `DefaultCLIConfig()`.** A flag is bound to its key per command (`general.kubeconfig`, `commands.logs.*`, `commands.serve.*`). No environment variable is read by name; the file's text goes through `os.ExpandEnv`, so a literal `$` in a value is consumed.
- **`config/default/cli.{yaml,toml,json}` are the source and `assets/config.*` are copies.** After editing one, run `go generate ./...` here and commit both. Nothing checks they match.
- **`cluster` commands read `--kubeconfig` from the flag alone**, not the config file.

## logs

- **A source is `[namespace:]<type>/<name>[/<container>]`**, a bare pod name, or `*` for a name or container. Parsing and the type aliases (`deploy`, `ds`, `sts`, `cj`…) are in `modules/shared/logs`.
- **`--backend auto|kubetail|kubernetes`** (default `auto`) picks the reader in `selectBackend`. `kubetail` is `internal/clusterapi`: GraphQL at `<apiserver>/apis/api.kubetail.com/v1/graphql`, a POST to fetch and a `graphql-transport-ws` WebSocket to follow. `kubernetes` is `logs.NewStream` from `shared`. In `auto` a 404 from the probe means the Cluster API is not installed and falls back with a warning on stderr; any other error is fatal. → [ADR: read logs through two backends](../../docs/adr/2026-11-09-read-logs-through-two-backends-behind-logfetcher.md)
- **The Cluster API client uses client-go's transports** (`rest.TransportFor` and its WebSocket round-tripper), so the kubeconfig's credentials and TLS apply. A plain `http.Client` or dialer would drop them.
- **`--grep` runs on the server with the Cluster API and in the client without it**, with a warning.
- **Which records**: `--head`, else `--tail`, else `--all`, else head when `--since` is set, else tail. The three are mutually exclusive. A bare `--tail` or `--head` means the configured default count. `-h` is `--head`; `--help` has no shorthand.
- **Output** goes through `tablewriter` to `cmd.OutOrStdout()`, flushed per row. Columns come from `--columns`, `--add-columns`, `--remove-columns`; `--raw` prints messages alone. The `dot` column is dropped when stdout is not a terminal or `NO_COLOR` is set. Warnings and paging cursors go to stderr. `cli.ExitOnError` prints its error to stdout.

## serve

`loadServerConfig` starts from the Dashboard's `DefaultConfig()` and sets the environment (`desktop`, or `cluster` with `--in-cluster`), the address, the kubeconfig, the CLI's version and the local storage directory (`~/.kubetail`, or the directory of `--config`). It listens on `localhost:7500` by default; `--host` widens that. **If the port is taken it listens on a free one** and logs which. Shutdown is the Dashboard's (`modules/CLAUDE.md`, *The GraphQL servers*). `--test` loads the config, prints `ok` and exits without starting a server; CI's Alpine build uses it as a smoke test.

## cluster

The logic is in `modules/shared/helm`. For install, upgrade and uninstall the release name and namespace are fixed at `kubetail`; `list` looks in every namespace. `cluster install` adds the chart repository if it is missing and installs the chart with the Dashboard disabled.

## Tests

The shared rules are in `modules/CLAUDE.md`. Here:

- **Seams are fields and interfaces.** `updatecheck.Options` takes the `Checker`, the `HelmLister`, the cache paths and a `Runner` (a test passes one that runs synchronously). `internal/clusterapi` has `newClientForTest` and `newStreamForTest`, and `Stream` talks to an unexported `clientIface` a test fakes.
- **A test that loads config sets `HOME` to a `t.TempDir()`** (`t.Setenv`, as `cmd/serve_test.go` does), or it reads the developer's own `~/.kubetail/config.yaml`.
- **References**: `internal/clusterapi/stream_test.go` (a fake client, no network), `internal/clusterapi/client_test.go` (`httptest` and a WebSocket server with frame helpers), `internal/updatecheck/updatecheck_test.go`, `cmd/logs_backend_test.go` (a pure decision function).

## Releasing

A person cuts a release with the `release-cli` skill, which tags `cli/v<semver>`. `.github/workflows/release-cli.yml` builds every target with the version set through `-X …/cmd.version=<v>`, signs the macOS and Windows binaries, and opens a draft GitHub release. The update check and the package-manager workflows parse that tag. → [ADR: release each component by its own tag](../../docs/adr/2026-11-09-release-each-component-by-its-own-tag.md)

When you change a command, a flag, the config or the release flow, update this file in the same change.
