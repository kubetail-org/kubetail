# kubetail

<img src="assets/github-logo.svg" height="150px" title="KubeTail">

Kubetail is a private, real-time log viewer for Kubernetes clusters

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/kubetail)](https://artifacthub.io/packages/search?repo=kubetail)
[![slack](https://img.shields.io/badge/Slack-Join%20Our%20Community-364954?logo=slack&labelColor=4D1C51)](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)


Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

## Introduction

Viewing application logs in a containerized environment can be challenging. Typically, an application consists of several services, each deployed across multiple containers which are load balanced to ensure an even consumption of resources. Although viewing individual container logs is easy using tools such as `kubectl` or the Kubernetes Dashboard, simultaneously monitoring logs from all the containers that constitute an application is more difficult. This difficulty is further compounded by the ephemeral nature of containers, which constantly cycle in and out of existence.

Kubetail solves this problem by providing an easy-to-use, web-based interface that allows you to view all the logs for a set of Kubernetes workloads (e.g. Deployment, CronJob, StatefulSet) simultaneously, in real-time. Under the hood, it uses your cluster's Kubernetes API to monitor your workloads and detect when a new workload container gets created or an old one deleted. Kubetail will then add messages from the new container to your viewing stream or update its UI to reflect that an old container will no longer produce messages. This allows you to follow your application logs easily as user requests move from one ephemeral container to another across services. Kubetail can also help you to debug application issues by allowing you to filter your logs by node properties such as availability zone, CPU architecture or node ID. This can be useful to find problems that are specific to a given environment that an application instance is running in.

The kubetail application consists of a Go-based backend server that connects to your Kubernetes API and a React-based static website that queries the backend server and displays results in the browser. Kubtail is typically deployed as a docker container inside your cluster using a manifest file or a helm chart and can be accessed via a web browser using the same methods you use to connect to your Kubernetes Dashboard (e.g. `kubectl proxy`). Since, internally, kubetail uses your Kubernetes API to request logs, your log messages always stay in your possession and kubetail is private by default.

Our goal is to build a powerful cloud-native logging platform designed from the ground up for a containerized environment and this project is a work-in-progress. If you notice a bug or have a suggestion please create a GitHub Issue or send us an email (hello@kubetail.com)!

## Key features

* Small and resource efficient (<30MB of memory, negligible CPU)
* View log messages in real-time
* View logs that are part of a specific workload (e.g. Deployment, CronJob, StatefulSet)
* Detects creation and deletion of workload containers and adds their logs to the viewing stream automatically
* Uses your Kubernetes API so log messages never leave your possession (private by default)
* Filter logs based on time
* Filter logs based on node properties such as availability zone, CPU architecture or node ID
* Color-coded log lines to distinguish between different containers
* A clean, easy-to-use interface

## Install

### Option 1: Manifest file

To allow kubetail to use an internal cluster service account to query your Kubernetes API, use the `-clusterauth` manifest file: 

```sh
kubectl apply -f https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-clusterauth.yaml
```

To require kubetail users to utilize their own Kubernetes authentication token, use the `-tokenauth` manifest file: 

```sh
kubectl apply -f https://github.com/kubetail-org/kubetail/releases/latest/download/kubetail-tokenauth.yaml
```

### Option 2: Helm chart

```sh
# Add the kubetail repository
helm repo add kubetail https://kubetail-org.github.io/helm/

# Deploy a Helm release named "kubetail" using the kubetail chart
helm install kubetail kubetail/kubetail --namespace kubetail --create-namespace
```

To configure the helm chart, please refer to [values.yaml](https://github.com/kubetail-org/helm/kubetail/values.yaml) for valid values and their defaults. You can use a YAML file or specify each parameter using the `--set key=value[,key=value]` argument:
```sh
helm install kubetail kubetail/kubetail \
  --namespace kubetail \
  --create-namespace \
  -f values.yaml \
  --set key1=val1,key2=val2
```

## Access

There are several ways to access the kubetail dashboard once the kubetail application is running in your cluster. For simplicity, we recommend using `kubectl proxy` if your kubetail deployment is using `auth-mode: cluster` and the `kubectl auth-proxy` plugin if it's using `auth-mode: token`.

### Option 1: kubectl proxy

The simplest way to access the dashboard, is using `kubectl proxy`:

```sh
kubectl proxy
```

Now you can access the dashboard at: [http://localhost:8001/api/v1/namespaces/kubetail/services/kubetail:4000/proxy/](http://localhost:8001/api/v1/namespaces/kubetail/services/kubetail:4000/proxy/).

### Option 2: kubectl port-forward

Another way to access the dashboard is using `kubectl port-forward`:

```sh
kubectl port-forward -n kubetail svc/kubetail 4000:4000
```

Now you can access the dashboard at: [http://localhost:4000](http://localhost:4000).

### Option 3: kubectl auth-proxy

If you've enabled `auth-mode: token`, then we recommend accessing the dashboard with the kubectl [auth-proxy plugin](https://github.com/int128/kauthproxy) which will automatically obtain an access token locally and add it to the HTTP headers when you make requests to the kubetail service:

```sh
kubectl auth-proxy -n kubetail http://kubetail.svc
```

Now your computer will automatically open a new browser tab pointing to the kubetail dashboard.

## Configure

### CLI

The kubetail server executable (`server`) supports the following command line configuration options:

| Flag         | Datatype    | Description               | Default   |
| ------------ | ----------- | ------------------------- | --------- |
| -c, --config | string      | Path to config file       | ""        |
| -a, --addr   | string      | Host address to bind to   | ":4000"   |
| --gin-mode   | string      | Gin mode (release, debug) | "release" |

### Config file

Kubetail can be configured using a configuration file written in YAML, JSON, TOML, HCL or envfile format. The application will automatically replace ENV variables written in the format `${NAME}` with their corresponding values. The config file supports the following options (also see [hack/config.yaml](hack/config.yaml)):

| Name                       | Datatype | Description                                          | Default                |
| -------------------------- | -------- | ---------------------------------------------------- | ---------------------- |
| addr                       | string   | Host address to bind to                              | ":4000"                |
| auth-mode                  | string   | Auth mode (token, cluster, local)                    | "token"                |
| gin-mode                   | string   | Gin mode (release, debug)                            | "release"              |
| kube-config                | string   | Kubectl config file path                             | "${HOME}/.kube/config" |
| csrf.enabled               | bool     | Enable CSRF protection                               | true                   |
| csrf.field-name            | string   | CSRF token name in forms                             | "csrf_token"           |
| csrf.secret                | string   | CSRF hash key                                        | ""                     |
| csrf.cookie.name           | string   | CSRF cookie name                                     | "csrf"                 |
| csrf.cookie.path           | string   | CSRF cookie path                                     | "/"                    |
| csrf.cookie.domain         | string   | CSRF cookie domain                                   | ""                     |
| csrf.cookie.max-age        | int      | CSRF cookie max age (in seconds)                     | 43200                  |
| csrf.cookie.secure         | bool     | CSRF cookie secure property                          | false                  |
| csrf.cookie.http-only      | bool     | CSRF cookie HttpOnly property                        | true                   |
| csrf.cookie.same-site      | string   | CSRF cookie SameSite property (strict, lax, none)    | "strict"               |
| logging.enabled            | bool     | Enable logging                                       | true                   |
| logging.level              | string   | Log level                                            | "info"                 |
| logging.format             | string   | Log format (json, pretty)                            | "json"                 |
| logging.access-log-enabled | bool     | Enable access log                                    | true                   |
| session.secret             | string   | Session hash key                                     | ""                     |
| session.cookie.path        | string   | Session cookie path                                  | "/"                    |
| session.cookie.domain      | string   | Session cookie domain                                | ""                     |
| session.cookie.max-age     | int      | Session cookie max age (in seconds)                  | 43200                  |
| session.cookie.secure      | bool     | Session cookie secure property                       | false                  |
| session.cookie.http-only   | bool     | Session cookie HttpOnly property                     | true                   |
| session.cookie.same-site   | string   | Session cookie SameSite property (strict, lax, none) | "strict"               |

## Develop

This repository is organized as a monorepo containing a Go-based backend server and a React-based frontend static website in their respective directories (see [backend](backend) and [frontend](frontend)). The website queries the backend server which proxies requests to a Kubernetes API and also performs a few other custom tasks (e.g. authentication). In production, the frontend website is bundled into the backend server and served as a static website (see [Build](#build)). In development, the backend and frontend are run separately but configured to work together.

To run the backend development server, cd into the `backend` directory and run the `server` command:
```sh
cd backend
go run ./cmd/server -c hack/server.conf
```

Now access the health status at [http://localhost:4000/healthz](http://localhost:4000/healthz). 

To run the frontend development website, cd into to the `frontend` directory and run the `install` and `dev` commands:
```sh
cd frontend
pnpm install
pnpm dev
```

Now access the dashboard at [http://localhost:5173](http://localhost:5173). 

## Build

To build a docker image for a production deployment, run the following command:

```sh
docker build -t kubetail:latest .
```

## How to help

Our goal is to build a powerful cloud-native logging platform designed from the ground up for a containerized environment and this project is a work-in-progress. If you're interested in getting involved please send us an email (hello@kubetail.com) or join our Slack channel (kubetail). In particular we're looking for help with the following:

* UI/design
* React frontend development
