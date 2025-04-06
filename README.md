# Kubetail

_Kubetail is a real-time logging dashboard for Kubernetes_

<a href="https://youtu.be/Hm___X0VzAc">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/c68cd271-a2c7-4e4b-88a0-4c188860b2d5">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/kubetail)](https://artifacthub.io/packages/search?repo=kubetail)

## Introduction

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** is a general-purpose logging dashboard for Kubernetes, optimized for real-time log access and tailing across multi-container workloads. With Kubetail, you can view logs from all the containers in a workload (like a Deployment or DaemonSet) in a single, beautifully organized timeline - in real-time.

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

### Option 1: Homebrew

First, install the Kubetail CLI tool (`kubetail`) via [homebrew](https://brew.sh/):

```console
brew install kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. Have fun viewing your Kubernetes logs in realtime!

### Option 2: Shell

First, download and run the [install.sh](/install.sh) script:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser.

### Option 3: Binaries

Download the binary for your OS/Arch (from the latest [release binaries](https://github.com/kubetail-org/kubetail/releases/latest)):

* [Darwin/amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-darwin-amd64)
* [Darwin/arm64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-darwin-arm64)
* [Linux/amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-linux-amd64)
* [Linux/arm64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-linux-arm64)
* [Windows/amd64](https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-windows-amd64)

Rename the file and make it executable:

```console
mv <filename> kubetail
chmod a+x kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser.

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

Visit [http://localhost:8080](http://localhost:8080). Have fun viewing your Kubernetes logs in realtime!

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

Visit [http://localhost:8080](http://localhost:8080). Have fun viewing your Kubernetes logs in realtime!

### Option 3: Glasskube

To install Kubetail using [Glasskube](https://glasskube.dev/), you can select "Kubetail" from the "ClusterPackages" tab in the Glasskube GUI then click "install" or you can run the following command:

```console
glasskube install kubetail
```

Once Kubetail is installed you can use it by clicking "open" in the Glasskube GUI or by using the `open` command:

```console
glasskube open kubetail
```

Have fun viewing your Kubernetes logs in realtime!

## Documentation

Visit the [Kubetail documentation](https://www.kubetail.com/)

## Development

### Repository Structure

This monorepo contains the following modules:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([modules/cluster-agent](modules/cluster-agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

It also contains the source code for the Kubetail Dashboard's frontend:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### Setting up the Development Environment

1. Create a Kubernetes dev cluster:

```console
ctlptl apply -f hack/ctlptl/minikube.yaml
```

You can use any type of cluster that [works with Tilt](https://docs.tilt.dev/choosing_clusters#microk8s).

2. Start the dev environment:

```console
tilt up
```

3. Run the Dashboard UI locally:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

Now access the dashboard at [http://localhost:5173](http://localhost:5173). 

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
