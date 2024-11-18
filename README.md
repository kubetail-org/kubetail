# Kubetail

Kubetail is a web dashboard for Kubernetes logs that lets you view multiple log streams simultaneously, in real-time (runs on desktop or in cluster)

<a href="https://www.youtube.com/watch?v=zyML-Pg12vw">
  <img width="350" alt="demo-light-thumbnail" src="https://github.com/user-attachments/assets/b37a6ca2-75b8-416a-a2e4-653c67f7fadd">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/kubetail)](https://artifacthub.io/packages/search?repo=kubetail)

## Introduction

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

Viewing application logs in a containerized environment can be challenging. Typically, an application consists of several services, each deployed across multiple containers which are load balanced to ensure an even consumption of resources. Although viewing individual container logs is easy using tools such as `kubectl` or the Kubernetes Dashboard, simultaneously monitoring logs from all the containers that constitute an application is more difficult. This is made even more difficult by the ephemeral nature of containers, which constantly cycle in and out of existence.

Kubetail solves this problem by providing an easy-to-use, web-based interface that allows you to view all the logs for a set of Kubernetes workloads (e.g. Deployment, CronJob, StatefulSet) simultaneously, in real-time. Under the hood, it uses your cluster's Kubernetes API to monitor your workloads and detect when a new workload container gets created or an old one deleted. Kubetail will then add messages from the new container to your viewing stream or update its UI to reflect that an old container will no longer produce messages. This allows you to follow your application logs easily as user requests move from one ephemeral container to another across services. Kubetail can also help you to debug application issues by allowing you to filter your logs by node properties such as availability zone, CPU architecture or node ID. This can be useful to find problems that are specific to a given environment that an application instance is running in.

The main entry point for Kubetail is a CLI tool called `kubetail` that you can use to run a web dashboard locally on your desktop. The web dashboard will make requests to your Kubernetes API using the current cluster specified in your local kube config file. In addition, you can run the web dashboard inside your cluster if you want to enable cluster users to use it without installing the CLI tool. Internally, Kubetail uses your Kubernetes API to request logs, so your log messages always stay in your possession and Kubetail is private by default. Most of Kubetail is written in Go and the web interface is written in Typescript/React.

Our goal is to build a powerful cloud-native logging platform designed from the ground up for a containerized environment and this project is a work-in-progress. If you notice a bug or have a suggestion please create a GitHub Issue or send us an email (hello@kubetail.com)!

## Features

* Clean, easy-to-use interface
* View log messages in real-time
* Filter logs by:
  * Workload (e.g. Deployment, CronJob, StatefulSet)
  * Absolute or relative time range
  * Node properties (e.g. availability zone, CPU architecture, node ID)
* Handles ephemeral container events automatically
* Uses your Kubernetes API to retrieve log messages so data never leaves your possession (private by default)
* Web dashboard can be installed on desktop or in cluster

## Quickstart

### Desktop Installation

First, install the Kubetail CLI tool (`kubetail`) via [homebrew](https://brew.sh/) (or the latest [release binaries](https://github.com/kubetail-org/kubetail/releases/latest)):

```console
brew install kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. To view the logs for a different cluster just change your `kubectl` context. Have fun viewing your Kubernetes logs in realtime!

### Cluster Installation

#### Option 1: Helm

First, add the Kubetail org's chart repository, then install the "kubetail" chart:

```console
helm repo add kubetail https://kubetail-org.github.io/helm-charts/
helm install kubetail kubetail/kubetail --namespace kubetail-system --create-namespace
```

For more information on how to configure the helm chart, see the chart's [values.yaml](https://github.com/kubetail-org/helm-charts/blob/main/charts/kubetail/values.yaml) file.

To access the web dashboard you can expose it as an ingress using the chart or you can use your usual access methods such as `kubectl port-forward`:

```console
kubectl port-forward -n kubetail-system svc/kubetail-server 7500:7500
```

Visit [http://localhost:7500](http://localhost:7500). Have fun viewing your Kubernetes logs in realtime!

#### Option 2: YAML Manifest

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
kubectl port-forward -n kubetail-system svc/kubetail-server 7500:7500
```

Visit [http://localhost:7500](http://localhost:7500). Have fun viewing your Kubernetes logs in realtime!

#### Option 3: Glasskube

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

This monorepo contains:

* CLI Tool ([modules/cli](modules/cli))
* Backend Server ([modules/server](modules/server))
* Backend Agent ([modules/agent](modules/agent))
* Dashboard UI ([dashboard-ui](dashbord-ui))

### Setting up the Development Environment

1. Create a Kubernetes dev cluster that [works with Tilt](https://docs.tilt.dev/choosing_clusters#microk8s):

```console
ctlptl apply -f hack/ctlptl/minikube.yaml
```

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

### Backend Server

To build a docker image for a production deployment of the backend server, run the following command:

```console
docker build -f build/package/Dockerfile.server -t kubetail-server:latest .
```

### Backend Agent

To build a docker image for a production deployment of the backend agent, run the following command:

```console
docker build -f build/package/Dockerfile.agent -t kubetail-agent:latest .
```

## Get Involved

We're building a **cloud-native logging platform** and would love your contributions! Here's how you can help:

* UI/UX design
* React frontend development
* Reporting issues and suggesting features

Reach us at hello@kubetail.com, or join our [Discord server](https://discord.gg/CmsmWAVkvX) or [Slack channel](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
