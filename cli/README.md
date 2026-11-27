# Kubetail CLI

The `kubetail` command, written in Go:

* `kubetail logs` prints container logs in the terminal.
* `kubetail serve` runs the Dashboard on your machine.
* `kubetail cluster` installs and manages Kubetail in a cluster.
* `kubetail config init` writes a default config file.

It also installs as a kubectl plugin, `kubectl kubetail`.

## Build

From the repo root:

```console
make
```

This builds the Dashboard UI, embeds it and writes the binary to `bin/kubetail`. It needs Go, Node.js and pnpm. A plain `go build` here works without the UI, but `kubetail serve` then answers 404 for every page.

## Checks

[The root guide's *Go modules › Checks*](../CLAUDE.md#checks) holds the commands every Go module shares.

While working:

```sh
make vet-cli-changed test-cli-changed   # from the repo root: what the change reaches, with dashboard/
go generate ./...                       # after editing config/default/cli.*: copies them to assets/; commit both
```

Before you push:

```sh
make lint-cli vet-cli-changed test-cli-changed cover-cli-changed   # from the repo root, with dashboard/
```

