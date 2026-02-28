# Kubetail

_Kubetail 是一个面向 Kubernetes 的实时日志仪表板_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

演示: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](README.md) | 简体中文 | [日本語](README.ja.md) | [한국어](README.ko.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Português](README.pt-BR.md) | [Français](README.fr.md)

## 简介

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** 是一个通用的 Kubernetes 日志仪表板，专门针对跨多容器工作负载的实时日志跟踪进行了优化。借助 Kubetail，你可以将某个工作负载中的所有容器日志（例如 Deployment 或 DaemonSet）合并到一条按时间排序的时间线上，并在浏览器或终端中查看。

Kubetail 的主要入口是 `kubetail` CLI 工具。它既可以在你的桌面上启动本地 Web 仪表板，也可以将原始日志直接流式输出到终端。在底层，Kubetail 通过集群的 Kubernetes API 直接从集群中获取日志，因此无需先将日志转发到外部服务即可开箱即用。Kubetail 还会使用 Kubernetes API 跟踪容器生命周期事件，以便在容器启动、停止或被替换时保持日志时间线同步。这使你能够在用户请求跨服务切换到不同临时容器时，依然连续地跟踪日志。

我们的目标是打造 Kubernetes 上最强大、最易用的日志平台，也非常希望你参与进来。如果你发现了 bug 或有建议，请创建 GitHub Issue，或者发送邮件到 hello@kubetail.com。

## 功能

* 简洁易用的界面
* 实时查看日志消息
* 按以下条件过滤日志:
  * 工作负载（例如 Deployment、CronJob、StatefulSet）
  * 绝对或相对时间范围
  * 节点属性（例如可用区、CPU 架构、节点 ID）
  * Grep
* 通过 Kubernetes API 获取日志消息，数据始终由你掌控（默认私有）
* 支持在多个集群之间切换（仅桌面版）
* 可在多种环境运行: Desktop、Cluster、Docker

## 快速开始

### 安装

你可以使用 [Homebrew](https://brew.sh/) 安装 `kubetail`:

```console
brew install kubetail
```

<details>
  <summary>查看另外 15 种安装方式（例如 Krew、Snap、Winget、Ubuntu、Fedora、SUSE、Alpine、Arch、Gentoo、Nix、asdf、Chocolatey、Scoop、MacPorts）</summary>
  
  ```console
  # Krew
  kubectl krew install kubetail

  # Snap
  sudo snap install kubetail

  # Winget
  winget install kubetail
  
  # Chocolatey
  choco install kubetail

  # Scoop
  scoop install kubetail

  # MacPorts
  sudo port install kubetail

  # Ubuntu/Mint (apt)
  sudo add-apt-repository ppa:kubetail/kubetail
  sudo apt update && sudo apt install kubetail-cli

  # Fedora/CentOS/RHEL/Amazonlinux/Mageia (copr)
  dnf copr enable kubetail/kubetail
  dnf install kubetail

  # SUSE (zypper)
  zypper addrepo 'https://download.opensuse.org/repositories/home:/kubetail/$releasever/' kubetail
  zypper refresh && zypper install kubetail-cli

  # Alpine (apk)
  apk add kubetail --repository=https://dl-cdn.alpinelinux.org/alpine/edge/testing

  # Arch Linux (AUR)
  yay -S --noconfirm kubetail-cli

  # Gentoo (GURU)
  ACCEPT_KEYWORDS="~$(portageq envvar ARCH)" emerge dev-util/kubetail

  # Nix (Flake)
  nix profile add github:kubetail-org/kubetail-nix

  # Nix (Classic)
  nix-env -i -f https://github.com/kubetail-org/kubetail-nix/archive/refs/heads/main.tar.gz

  # asdf
  asdf plugin add kubetail https://github.com/kubetail-org/asdf-kubetail.git
  asdf install kubetail latest
  ```
</details>

如果你愿意，也可以直接下载[发布二进制文件](https://github.com/kubetail-org/kubetail/releases/latest)，或者使用我们的安装脚本:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### 用法

下面是一些使用 `kubetail` 的方式:

**1. 启动 Web 仪表板（GUI）**

```console
kubetail serve
```

**2. 在终端中查看日志**

```console
kubetail logs -f deployments/my-app
```

**3. 启用高级功能（安装 Kubetail API）**

```console
kubetail cluster install
```

**4. 初始化本地配置文件**

```console
kubetail config init
```

完整的[命令列表](https://www.kubetail.com/docs/cli#subcommands)请参阅文档。祝你 tail 日志愉快。

## 随处运行

除了在桌面上运行 Kubetail 外，你还可以在以下环境中运行它:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## 文档

完整文档请访问 [https://www.kubetail.com](https://www.kubetail.com/)。

## 路线图与状态

这是 Kubetail 项目的高层计划，按顺序排列如下:

|   | 步骤                                                  | 状态   |
| - | ----------------------------------------------------- | ------ |
| 1 | 实时容器日志                                          | ✅     |
| 2 | 实时搜索与更完善的用户体验                            | 🛠️     |
| 3 | 实时系统日志（例如 systemd、k8s events）              | 🔲     |
| 4 | 基础可定制能力（例如颜色、时间格式）                  | 🔲     |
| 5 | 消息解析与指标                                        | 🔲     |
| 6 | 历史数据（例如日志归档、指标时间序列）                | 🔲     |
| 7 | Kubetail API 与面向开发者的客户端库                   | 🔲     |
| N | 世界和平                                              | 🔲     |

更多细节如下:

**实时容器日志**

用户可以通过 Web 仪表板快速、轻松地查看其集群中当前运行的 Pod 的容器日志。用户可以按工作负载组织查看容器日志，并在临时容器被创建和删除时继续跟踪日志。用户还可以按时间戳缩小查看范围，并按区域、可用区、节点等来源属性过滤日志。

**实时搜索与更完善的用户体验**

用户可以轻松地在桌面或集群中安装 Kubetail。默认情况下，Kubetail 仅使用 Kubernetes API 获取运行中的工作负载和容器日志等基础数据。如果用户需要更高级的功能，可以在集群中安装 Kubetail 自定义服务（即 “Kubetail Cluster API” 和 “Kubetail Cluster Agent”，统称为 “Kubetail API”），从而获得日志搜索、日志文件大小、最后事件时间戳等功能。Kubetail API 的安装、升级和卸载体验都经过精心打磨，用户可以通过 Kubetail Web 仪表板和 CLI 工具，在浏览器和终端中使用同样强大的日志查看能力。

**实时系统日志**

安装了 Kubetail API 的用户可以立即访问节点级日志（例如 systemd）和集群级日志（例如 Kubernetes events），并在一个集成界面中查看这些日志，该界面还会同时展示 CPU 使用率、内存使用量和磁盘空间等其他系统信息。系统日志会实时显示，并与其他日志合并到同一条时间线上。用户可以按时间戳和来源属性过滤系统日志。

**基础可定制能力**

用户将能够在使用 Web 仪表板和 CLI 工具时，通过修改用户设置来完全自定义 Kubetail 体验。用户设置既可以手动编辑配置文件，也可以通过仪表板 UI 进行修改。即使升级过程会新增、移除或修改用户设置，整个体验也会保持平滑一致。用户还可以选择在多个设备之间同步这些设置。

## 开发

### 仓库结构

这个 monorepo 包含以下模块:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([crates/cluster_agent](crates/cluster_agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

它还包含 Kubetail Dashboard 前端的源代码:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### 搭建开发环境

#### 依赖

* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [兼容 Tilt 的集群](https://docs.tilt.dev/choosing_clusters.html)（例如 [minikube](https://minikube.sigs.k8s.io/docs/)、[kind](https://kind.sigs.k8s.io/docs/user/quick-start/)、[docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop)）
* [ctlptl](https://github.com/tilt-dev/ctlptl)（可选）

#### 下一步

1. 创建一个[兼容 Tilt 的](https://docs.tilt.dev/choosing_clusters.html) Kubernetes 开发集群:

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. 启动开发环境:

```console
tilt up
```

3. 启动 Dashboard 服务:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. 在本地运行 Dashboard UI:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

现在可以通过 [http://localhost:5173](http://localhost:5173) 访问仪表板。

<details>
  <summary><h3>为 Rust 优化开发环境（可选）</h3></summary>
  
  默认情况下，当你运行 `tilt up` 时，开发环境会以 "release" 构建来编译 Rust 组件。如果你想更快地迭代，可以让 Tilt 改为在本地使用 "debug" 构建来编译 Rust 代码。

  #### 依赖

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  #### 下一步

  首先，安装你的架构所需的 Rust target:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  接下来，安装 Rust 交叉编译器所需的工具:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  在 macOS 上，将以下内容添加到你的 `~/.cargo/config.toml` 文件中:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  最后，如需使用本地编译器，只需在运行 Tilt 时加上 `KUBETAIL_DEV_RUST_LOCAL` 环境变量:

  ```console
  KUBETAIL_DEV_RUST_LOCAL=true tilt up
  ```
</details>

## 构建

### CLI 工具

要构建 Kubetail CLI 可执行文件（`kubetail`），请运行以下命令:

```console
make
```

构建完成后，你可以在本地 `bin/` 目录中找到该可执行文件。

### Dashboard

要为生产部署构建 Kubetail Dashboard 服务的 Docker 镜像，请运行以下命令:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

要为生产部署构建 Kubetail Cluster API 服务的 Docker 镜像，请运行以下命令:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

要为生产部署构建 Kubetail Cluster Agent 的 Docker 镜像，请运行以下命令:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## 参与贡献

我们正在打造面向 Kubernetes 的最**易用**、最**高性价比**、最**安全**的日志平台，欢迎你的贡献。你可以通过以下方式帮助我们:

* UI/UX 设计
* React 前端开发
* 报告问题并提出新功能建议

欢迎通过 hello@kubetail.com 联系我们，或加入我们的 [Discord 服务器](https://discord.gg/CmsmWAVkvX) 或 [Slack 频道](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)。
