# Kubetail

_Kubetail is a real-time logging dashboard for Kubernetes (browser/terminal)_ 

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

## Introduction

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** is a general-purpose logging dashboard for Kubernetes, optimized for tailing logs across multi-container workloads in real-time. With Kubetail, you can view logs from all the containers in a workload (e.g. Deployment or DaemonSet) merged into a single, chronological timeline, delivered to your browser or terminal.

The primary entry point for Kubetail is the `kubetail` CLI tool, which can launch a local web dashboard on your desktop or stream raw logs directly to your terminal. Behind the scenes, Kubetail uses your cluster's Kubernetes API to fetch logs directly from your cluster, so it works out of the box without needing to forward your logs to an external service first. Kubetail also uses your Kubernetes API to track container lifecycle events in order to keep your log timeline in sync as containers start, stop or get replaced. This makes it easy to follow logs seamlessly as user requests move from one ephemeral container to another across services.

Our goal is to build the most powerful, user-friendly logging platform for Kubernetes and we'd love your input. If you notice a bug or have a suggestion please create a GitHub Issue or send us an email (hello@kubetail.com)!

## Features

* Clean, easy-to-use interface
* View log messages in real-time
* Filter logs by:
  * Workload (e.g. Deployment, CronJob, StatefulSet)
  * Absolute or relative time range
  * Node properties (e.g. availability zone, CPU architecture, node ID)
  * Grep 
* Uses your Kubernetes API to retrieve log messages so data never leaves your possession (private by default)
* Web dashboard can be installed on desktop or in cluster
* Switch between multiple clusters (Desktop-only)

## Quickstart (Desktop)

### Option 1: Package Managers

First, install the Kubetail CLI tool (`kubetail`) via your favorite package manager:

```console
# Homebrew
brew install kubetail

# Winget
winget install Kubetail.Kubetail

# Chocolatey
choco install kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. Have fun tailing your logs!

### Option 2: Shell

First, download and run the install script:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. Have fun tailing your logs!

### Option 3: Binaries

Download the binary for your OS/Arch (from the latest [release binaries](https://github.com/kubetail-org/kubetail/releases/latest)):

* Darwin ([amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-darwin-amd64), [arm64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-darwin-arm64))
* Linux ([amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-linux-amd64), [arm64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-linux-arm64))
* Windows ([amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-windows-amd64), [arm64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-windows-arm64))

Rename the file and make it executable:

```console
mv <filename> kubetail
chmod a+x kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. Have fun tailing your logs!

## Quickstart (Cluster)

### Option 1: Helm

First, add the Kubetail org's chart repository, then install the "kubetail" chart:

```console
helm repo add kubetail https://kubetail-org.github.io/helm-charts/
helm install kubetail kubetail/kubetail --namespace kubetail-system --create-namespace
```

For more information on how to configure the helm chart, see the chart's [values.yaml](https://github.com/kubetail-org/helm-charts/blob/main/charts/kubetail/values.yaml) file.

To access the web dashboard you can expose it as an ingress using the chart or you can use your usual access methods such as `kubectl port-forward`:

```console
kubectl port-forward -n kubetail-system svc/kubetail-dashboard 8080:8080
```

Visit [http://localhost:8080](http://localhost:8080). Have fun tailing your logs!

### Option 2: YAML Manifest

First, create a namespace for the Kubetail resources:

```console
kubectl create namespace kubetail-system
```

Next, choose your authentication mode (`cluster` or `token`) and apply the latest manifest file:

```console
# For cluster-based authentication use kubetail-clusterauth.yaml:
kubectl apply -f https://github.com/kubetail-org/helm-charts/releases/latest/download/kubetail-clusterauth.yaml

# For token-based authentication use kubetail-tokenauth.yaml:
kubectl apply -f https://github.com/kubetail-org/helm-charts/releases/latest/download/kubetail-tokenauth.yaml
```

To access the web dashboard you can use your usual access methods such as `kubectl port-forward`:

```console
kubectl port-forward -n kubetail-system svc/kubetail-dashboard 8080:8080
```

Visit [http://localhost:8080](http://localhost:8080). Have fun tailing your logs!

### Option 3: Glasskube

To install Kubetail using [Glasskube](https://glasskube.dev/), you can select "Kubetail" from the "ClusterPackages" tab in the Glasskube GUI then click "install" or you can run the following command:

```console
glasskube install kubetail
```

Once Kubetail is installed you can use it by clicking "open" in the Glasskube GUI or by using the `open` command:

```console
glasskube open kubetail
```

Have fun tailing your logs!

## Documentation

Visit the [Kubetail documentation](https://www.kubetail.com/)

## Roadmap and Status

This is our high-level plan for the Kubetail project, in order:

|   | Step                                                  | Status |
| - | ----------------------------------------------------- | ------ |
| 1 | Real-time container logs                              | ✅     |
| 2 | Real-time search and polished user experience         | 🛠️     |
| 3 | Real-time system logs (e.g. systemd, k8s events)      | 🔲     |
| 4 | Basic customizability (e.g. colors, time formats)     | 🔲     |
| 5 | Message parsing and metrics                           | 🔲     |
| 6 | Historic data (e.g. log archives, metrics timeseries) | 🔲     |
| 7 | Kubetail API and developer-facing client libraries    | 🔲     |
| N | World Peace                                           | 🔲     |

And here are some additional details:

**Real-time container logs**

Users can view the container logs from the pods currently running inside their clusters quickly and easily using a web dashboard. Users can view container logs organized by workloads and follow log messages as ephemeral containers get created and deleted. They can also narrow their viewing window by timestamp and filter logs by source properties such as region, zone and node.

**Real-time search and polished user experience**

Users can install Kubetail easily on their desktops and in their clusters. By default, Kubetail will use only the Kubernetes API to fetch basic data such as running workloads and container logs. If a user wants more advanced functionality they can install Kubetail custom services in their cluster (i.e. "Kubetail Cluster API" and "Kubetail Cluster Agent", collectively known as the "Kubetail API") and gain access to features such as log search, log file sizes and last event timestamps. The entire experience of installing, upgrading and uninstalling the Kubetail API is very polished and users are able to view their logs with equally powerful tools in the browser and the terminal using the Kubetail web dashboard and CLI tool.

**Real-time system logs**

Users who install the Kubetail API gain immediate access to their node-level logs (e.g. systemd) and cluster-level logs (e.g. kubernetes events) and view them in an integrated interface that shows their container logs in context with other system information such as CPU utilization, memory usage and disk space. System logs are viewable in real-time, in the same merged timeline with other logs. Users can filter system logs by timestamp and source properties.

**Basic customizability**

Users can fully customize their Kubetail experience when using the web dashboard and CLI tool by modifying their user settings. The user settings are modifiable by hand using a config file or via the dashboard UI. The experience is very polished and works seamlessly across upgrades that may add/remove/modify user settings. Users have the option to sync their settings across multiple devices.

## Development

### Repository Structure

This monorepo contains the following modules:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([modules/cluster-agent](modules/cluster-agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

It also contains the source code for the Kubetail Dashboard's frontend and the Rust binary that powers log search:

* Dashboard UI ([dashboard-ui](dashboard-ui))
* rgkl ([crates/rgkl](crates/rgkl))

### Setting up the Development Environment

#### Dependencies

* [Tilt](https://tilt.dev/)
* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [ctlptl](https://github.com/tilt-dev/ctlptl) (optional)

#### Next steps

1. Create a Kubernetes Dev Cluster

```console
ctlptl apply -f hack/ctlptl/minikube.yaml
```

You can use any type of cluster that [works with Tilt](https://docs.tilt.dev/choosing_clusters.html).

2. Start the dev environment:

```console
tilt up
```

3. Start the Dashboard server:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. Run the Dashboard UI locally:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

Now access the dashboard at [http://localhost:5173](http://localhost:5173).

### Optimize Development Environment for Rust (Optional)

By default, the dev environment compiles "release" builds of the Rust components when you run run `tilt up`. If you want to iterate more quickly, you can have Tilt compile the rust code locally using "debug" builds instead.

#### Dependencies

* [rustup](https://rustup.rs)
* [protobuf](https://protobuf.dev/installation/)

#### Next steps

First, install the Rust target required for your architecture:

```console
# x86_64
rustup target add x86_64-unknown-linux-musl

# aarch64
rustup target add aarch64-unknown-linux-musl
```

Next, install the tools required by Rust cross compiler:

```console
# macOS (Homebrew)
brew install FiloSottile/musl-cross/musl-cross

# Linux (Ubuntu)
apt-get install musl-tools
```

On macOS, add this to your `~/.cargo/config.toml` file:

```
[target.x86_64-unknown-linux-musl]
linker = "x86_64-linux-musl-gcc"

[target.aarch64-unknown-linux-musl]
linker = "aarch64-linux-musl-gcc"
```

Finally, to use the local compiler, just run Tilt using using the `KUBETAIL_DEV_RUST_LOCAL` env flag:

```console
KUBETAIL_DEV_RUST_LOCAL=true tilt up
```

## Build

### CLI Tool

To build the Kubetail CLI tool executable (`kubetail`), run the following command:

```console
make
```

When the build process finishes you can find the executable in the local `bin/` directory.

### Dashboard

To build a docker image for a production deployment of the Kubetail Dashboard server, run the following command:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

To build a docker image for a production deployment of the Kubetail Cluster API server, run the following command:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

To build a docker image for a production deployment of the Kubetail Cluster Agent, run the following command:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## Get Involved

We're building the most **user-friendly**, **cost-effective**, and **secure** logging platform for Kubernetes and we'd love your contributions! Here's how you can help:

* UI/UX design
* React frontend development
* Reporting issues and suggesting features

Reach us at hello@kubetail.com, or join our [Discord server](https://discord.gg/CmsmWAVkvX) or [Slack channel](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
