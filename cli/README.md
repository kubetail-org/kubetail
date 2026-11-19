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

## Test

```console
go test -race ./...
```
