# Kubetail

Kubetail is a web dashboard for Kubernetes logs that lets you view multiple log streams simultaneously, in real-time (runs on desktop or in cluster)

<img src="https://github.com/user-attachments/assets/396a24b0-86e6-469b-9d32-379044aa1da1" width="300px" title="screenshot">

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/pXHXaUqt"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/kubetail)](https://artifacthub.io/packages/search?repo=kubetail)

## Introduction

<img src="assets/github-logo.svg" width="300" title="Kubetail">

Viewing application logs in a containerized environment can be challenging. Typically, an application consists of several services, each deployed across multiple containers which are load balanced to ensure an even consumption of resources. Although viewing individual container logs is easy using tools such as `kubectl` or the Kubernetes Dashboard, simultaneously monitoring logs from all the containers that constitute an application is more difficult. This is made even more difficult by the ephemeral nature of containers, which constantly cycle in and out of existence.

Kubetail solves this problem by providing an easy-to-use, web-based interface that allows you to view all the logs for a set of Kubernetes workloads (e.g. Deployment, CronJob, StatefulSet) simultaneously, in real-time. Under the hood, it uses your cluster's Kubernetes API to monitor your workloads and detect when a new workload container gets created or an old one deleted. Kubetail will then add messages from the new container to your viewing stream or update its UI to reflect that an old container will no longer produce messages. This allows you to follow your application logs easily as user requests move from one ephemeral container to another across services. Kubetail can also help you to debug application issues by allowing you to filter your logs by node properties such as availability zone, CPU architecture or node ID. This can be useful to find problems that are specific to a given environment that an application instance is running in.

The main entry point for Kubetail is a CLI tool called `kubetail` that you can use to run a web dashboard locally on your desktop. The web dashboard will make requests to your Kubernetes API using the current cluster specified in your local kube config file. In addition, you can run the web dashboard inside your cluster if you want to enable cluster users to use it without installing the CLI tool. Internally, Kubetail uses your Kubernetes API to request logs, so your log messages always stay in your possession and Kubetail is private by default. Most of Kubetail is written in Go and the web interface is written in Typescript/React.

Our goal is to build a powerful cloud-native logging platform designed from the ground up for a containerized environment and this project is a work-in-progress. If you notice a bug or have a suggestion please create a GitHub Issue or send us an email (hello@kubetail.com)!

## Key features

* View log messages in real-time
* View logs that are part of a specific workload (e.g. Deployment, CronJob, StatefulSet)
* Detects creation and deletion of workload containers and adds their logs to the viewing stream automatically
* Uses your Kubernetes API to retrieve log messages so data never leaves your possession (private by default)
* Filter logs based on time
* Filter logs based on node properties such as availability zone, CPU architecture or node ID
* Color-coded log lines to distinguish between different containers
* A clean, easy-to-use interface

## Quickstart

First, install the Kubetail CLI tool (`kubetail`) via [homebrew](https://brew.sh/) (or [release binaries](https://github.com/kubetail-org/kubetail/releases/tag/cli/v0.0.4)):

```console
brew install kubetail
```

Next, start the web dashboard using the `serve` subcommand:

```console
kubetail serve
```

This command will open [http://localhost:7500/](http://localhost:7500/) in your default browser. Have fun viewing your Kubernetes logs in realtime!

## Advanced Installation

There are several options for installing the Kubetail web dashboard in your cluster so cluster users can use it without installing the CLI tool.

### Option 1: 

### Option 1: Manifest file

To allow Kubetail to use an internal cluster service account to query your Kubernetes API, use the `-clusterauth` manifest file: 

```console
kubectl apply -f https://github.com/kubetail-org/helm-charts/releases/latest/download/kubetail-clusterauth.yaml
```

To require Kubetail users to utilize their own Kubernetes authentication token, use the `-tokenauth` manifest file: 

```console
kubectl apply -f https://github.com/kubetail-org/helm-charts/releases/latest/download/kubetail-tokenauth.yaml
```

### Option 2: Helm chart

To install Kubetail using helm, first add the "kubetail" repository, then install the "kubetail" chart:
```console
helm repo add kubetail https://kubetail-org.github.io/helm-charts/
helm install kubetail kubetail/kubetail --namespace kubetail-system --create-namespace
```

To configure the helm chart, please refer to [values.yaml](https://github.com/kubetail-org/helm-charts/blob/main/charts/kubetail/values.yaml) for valid values and their defaults. You can use a YAML file or specify each parameter using the `--set key=value[,key=value]` argument:
```console
helm install kubetail kubetail/kubetail \
  --namespace kubetail-system \
  --create-namespace \
  -f values.yaml \
  --set key1=val1,key2=val2
```

### Option 3: Glasskube

To install Kubetail using [Glasskube](https://glasskube.dev/), you can select "kubetail" from the "ClusterPackages" tab in the Glasskube GUI then click "install" or you can run the following command: 
```console
glasskube install kubetail
```

Once Kubetail is installed you can use it by clicking "open" in the Glasskube GUI or by using the `open` command:
```console
glasskube open kubetail
```

## Access

There are several ways to access the Kubetail dashboard once the application is running in your cluster. For simplicity, we recommend using `kubectl proxy` if your Kubetail deployment is using `auth-mode: cluster` and the `kubectl auth-proxy` plugin if it's using `auth-mode: token`.

### Option 1: kubectl proxy

The simplest way to access the dashboard, is using `kubectl proxy`:

```console
kubectl proxy
```

Now you can access the dashboard at: [http://localhost:8001/api/v1/namespaces/kubetail/services/kubetail-server:80/proxy/](http://localhost:8001/api/v1/namespaces/kubetail/services/kubetail-server:80/proxy/).

### Option 2: kubectl port-forward

Another way to access the dashboard is using `kubectl port-forward`:

```console
kubectl port-forward -n kubetail svc/kubetail-server 80:4000
```

Now you can access the dashboard at: [http://localhost:4000](http://localhost:4000).

### Option 3: kubectl auth-proxy

If you've enabled `auth-mode: token`, then we recommend accessing the dashboard with the kubectl [auth-proxy plugin](https://github.com/int128/kauthproxy) which will automatically obtain an access token locally and add it to the HTTP headers when you make requests to the kubetail-server service:

```console
kubectl auth-proxy -n kubetail http://kubetail-server.svc
```

Now your computer will automatically open a new browser tab pointing to the Kubetail dashboard.

## Develop

This repository is organized as a monorepo containing the backend components (a Go-based server, a Go-based agent) and the frontend code (a React-based static website) in their respective top-level directories ([backend](backend), [frontend](frontend)). The website queries the server which proxies requests to the Kubernetes API and to agents running on each node, and also performs a few other custom tasks (e.g. authentication). In production, the frontend website is bundled into the backend server and served as a static website (see [Build](#build)). In development, the backend and frontend are run separately but configured to work together using [Tilt](https://tilt.dev).

To develop Kubetail, first create a Kubernetes dev cluster using a dev cluster tool that [works with Tilt](https://docs.tilt.dev/choosing_clusters#microk8s). To automate the process you can also use [ctlptl](https://github.com/tilt-dev/ctlptl) and one of the configs available in the [`hack/ctlptl`](hack/ctlptl) directory. For example, to create a dev cluster using [minikube](https://minikube.sigs.k8s.io/docs/) you can use this command:

```console
ctlptl apply -f hack/ctlptl/minikube.yaml
```

Once the dev cluster is running and `kubectl` is pointing to it, you can bring up the dev environment using Tilt: 

```console
tilt up
```

After Tilt brings up the backend server you can access it on your localhost on port 4000. To run the frontend development website, cd into to the `frontend` directory and run the `install` and `dev` commands:

```console
cd frontend
pnpm install
pnpm dev
```

Now access the dashboard at [http://localhost:5173](http://localhost:5173). 

## Build

### kubetail-server

To build a docker image for a production deployment of the backend server, run the following command:

```console
docker build -f build/package/Dockerfile.server -t kubetail-server:latest .
```

### kubetail-agent

To build a docker image for a production deployment of the backend agent, run the following command:

```console
docker build -f build/package/Dockerfile.agent -t kubetail-agent:latest .
```

## How to help

Our goal is to build a powerful cloud-native logging platform designed from the ground up for a containerized environment and this project is a work-in-progress. If you're interested in getting involved please send us an email (hello@kubetail.com) or join our Slack channel (kubetail). In particular we're looking for help with the following:

* UI/design
* React frontend development
