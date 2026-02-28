# Kubetail

_Kubetail은 Kubernetes를 위한 실시간 로깅 대시보드입니다_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

데모: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | 한국어 | [Deutsch](README.de.md) | [Español](README.es.md) | [Português](README.pt-BR.md) | [Français](README.fr.md)

## 소개

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail**은 Kubernetes용 범용 로깅 대시보드로, 여러 컨테이너 워크로드에 걸친 로그를 실시간으로 tailing 하도록 최적화되어 있습니다. Kubetail을 사용하면 하나의 워크로드(예: Deployment 또는 DaemonSet)에 속한 모든 컨테이너의 로그를 하나의 시간순 타임라인으로 합쳐 브라우저나 터미널에서 볼 수 있습니다.

Kubetail의 주요 진입점은 `kubetail` CLI 도구입니다. 이 도구로 데스크톱에서 로컬 웹 대시보드를 실행하거나 원시 로그를 터미널로 직접 스트리밍할 수 있습니다. 내부적으로 Kubetail은 클러스터의 Kubernetes API를 사용해 로그를 클러스터에서 직접 가져오기 때문에, 로그를 외부 서비스로 전달하지 않아도 바로 사용할 수 있습니다. 또한 컨테이너 수명 주기 이벤트를 Kubernetes API로 추적해 컨테이너가 시작, 종료, 교체될 때 로그 타임라인을 동기화합니다. 덕분에 사용자 요청이 서비스 사이에서 임시 컨테이너를 오가더라도 로그를 끊김 없이 따라갈 수 있습니다.

저희의 목표는 Kubernetes를 위한 가장 강력하고 사용하기 쉬운 로깅 플랫폼을 만드는 것입니다. 버그를 발견했거나 제안이 있다면 GitHub Issue를 생성하거나 hello@kubetail.com 으로 메일을 보내 주세요.

## 기능

* 깔끔하고 사용하기 쉬운 인터페이스
* 실시간 로그 메시지 보기
* 다음 기준으로 로그 필터링:
  * 워크로드(예: Deployment, CronJob, StatefulSet)
  * 절대 또는 상대 시간 범위
  * 노드 속성(예: 가용 영역, CPU 아키텍처, 노드 ID)
  * Grep
* Kubernetes API로 로그 메시지를 가져오므로 데이터가 외부로 나가지 않음(기본적으로 비공개)
* 여러 클러스터 간 전환 지원(데스크톱 전용)
* 어디서나 실행 가능: Desktop, Cluster, Docker

## 빠른 시작

### 설치

`kubetail`은 [Homebrew](https://brew.sh/)로 설치할 수 있습니다:

```console
brew install kubetail
```

<details>
  <summary>Krew, Snap, Winget, Ubuntu, Fedora, SUSE, Alpine, Arch, Gentoo, Nix, asdf, Chocolatey, Scoop, MacPorts 등 15가지 다른 방법 보기</summary>
  
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

원한다면 [릴리스 바이너리](https://github.com/kubetail-org/kubetail/releases/latest)를 다운로드하거나 설치 스크립트를 사용할 수도 있습니다:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### 사용법

`kubetail`을 사용하는 몇 가지 방법은 다음과 같습니다:

**1. 웹 대시보드 시작하기 (GUI)**

```console
kubetail serve
```

**2. 터미널에서 로그 보기**

```console
kubetail logs -f deployments/my-app
```

**3. 고급 기능 활성화하기 (Kubetail API 설치)**

```console
kubetail cluster install
```

**4. 로컬 설정 파일 초기화하기**

```console
kubetail config init
```

전체 [commands](https://www.kubetail.com/docs/cli#subcommands) 목록은 문서를 확인하세요. 즐겁게 로그를 tail 하세요.

## 어디서나 실행

Kubetail은 데스크톱뿐 아니라 다음 환경에서도 실행할 수 있습니다:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## 문서

전체 문서는 [https://www.kubetail.com](https://www.kubetail.com/) 에서 확인할 수 있습니다.

## 로드맵 및 상태

다음은 Kubetail 프로젝트의 상위 수준 계획이며, 순서대로 나열되어 있습니다:

|   | 단계                                                  | 상태   |
| - | ----------------------------------------------------- | ------ |
| 1 | 실시간 컨테이너 로그                                  | ✅     |
| 2 | 실시간 검색과 세련된 사용자 경험                      | 🛠️     |
| 3 | 실시간 시스템 로그(예: systemd, k8s events)           | 🔲     |
| 4 | 기본적인 사용자 정의 기능(예: 색상, 시간 형식)        | 🔲     |
| 5 | 메시지 파싱 및 메트릭                                 | 🔲     |
| 6 | 이력 데이터(예: 로그 아카이브, 메트릭 시계열)         | 🔲     |
| 7 | Kubetail API 및 개발자용 클라이언트 라이브러리        | 🔲     |
| N | 세계 평화                                             | 🔲     |

추가 세부 사항은 다음과 같습니다:

**실시간 컨테이너 로그**

사용자는 클러스터에서 현재 실행 중인 Pod의 컨테이너 로그를 웹 대시보드에서 빠르고 쉽게 볼 수 있습니다. 워크로드별로 정리된 컨테이너 로그를 보고, 임시 컨테이너가 생성되고 삭제되는 동안에도 로그를 계속 따라갈 수 있습니다. 또한 타임스탬프로 조회 범위를 좁히고 리전, 존, 노드 같은 소스 속성으로 로그를 필터링할 수 있습니다.

**실시간 검색과 세련된 사용자 경험**

사용자는 Kubetail을 데스크톱과 클러스터에 쉽게 설치할 수 있습니다. 기본적으로 Kubetail은 Kubernetes API만 사용해 실행 중인 워크로드와 컨테이너 로그 같은 기본 데이터를 가져옵니다. 더 고급 기능이 필요하면 클러스터에 Kubetail 전용 서비스("Kubetail Cluster API"와 "Kubetail Cluster Agent", 통칭 "Kubetail API")를 설치하여 로그 검색, 로그 파일 크기, 마지막 이벤트 시각 등의 기능을 사용할 수 있습니다. Kubetail API의 설치, 업그레이드, 제거 경험은 매우 다듬어져 있으며, 사용자는 Kubetail 웹 대시보드와 CLI 도구를 통해 브라우저와 터미널 양쪽에서 강력한 로그 보기 기능을 사용할 수 있습니다.

**실시간 시스템 로그**

Kubetail API를 설치한 사용자는 노드 수준 로그(예: systemd)와 클러스터 수준 로그(예: Kubernetes events)에 즉시 접근할 수 있으며, CPU 사용량, 메모리 사용량, 디스크 공간 같은 다른 시스템 정보와 함께 보여 주는 통합 인터페이스에서 이를 확인할 수 있습니다. 시스템 로그는 다른 로그와 동일한 통합 타임라인 안에서 실시간으로 표시됩니다. 사용자는 타임스탬프와 소스 속성으로 시스템 로그를 필터링할 수 있습니다.

**기본적인 사용자 정의 기능**

사용자는 웹 대시보드와 CLI 도구를 사용할 때 사용자 설정을 변경해 Kubetail 경험을 완전히 사용자 정의할 수 있게 됩니다. 사용자 설정은 구성 파일을 직접 편집하거나 대시보드 UI를 통해 수정할 수 있습니다. 설정이 추가, 제거, 변경되는 업그레이드에서도 경험은 매끄럽게 유지됩니다. 또한 여러 기기 간에 설정을 동기화하는 옵션도 제공됩니다.

## 개발

### 저장소 구조

이 모노레포에는 다음 모듈이 포함되어 있습니다:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([crates/cluster_agent](crates/cluster_agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

또한 Kubetail Dashboard 프런트엔드의 소스 코드도 포함되어 있습니다:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### 개발 환경 설정

#### 의존성

* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Tilt와 호환되는 클러스터](https://docs.tilt.dev/choosing_clusters.html) (예: [minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/), [docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop))
* [ctlptl](https://github.com/tilt-dev/ctlptl) (선택 사항)

#### 다음 단계

1. [Tilt와 호환되는](https://docs.tilt.dev/choosing_clusters.html) Kubernetes 개발 클러스터를 생성합니다:

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. 개발 환경을 시작합니다:

```console
tilt up
```

3. Dashboard 서버를 시작합니다:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. Dashboard UI를 로컬에서 실행합니다:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

이제 [http://localhost:5173](http://localhost:5173) 에서 대시보드에 접속할 수 있습니다.

<details>
  <summary><h3>Rust용 개발 환경 최적화(선택 사항)</h3></summary>
  
  기본적으로 `tilt up`을 실행하면 개발 환경은 Rust 컴포넌트를 "release" 빌드로 컴파일합니다. 더 빠르게 반복 작업하고 싶다면 대신 Tilt가 "debug" 빌드로 Rust 코드를 로컬에서 컴파일하도록 할 수 있습니다.

  #### 의존성

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  #### 다음 단계

  먼저 아키텍처에 필요한 Rust 타깃을 설치합니다:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  다음으로 Rust 크로스 컴파일러에 필요한 도구를 설치합니다:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  macOS에서는 `~/.cargo/config.toml` 파일에 다음을 추가합니다:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  마지막으로 로컬 컴파일러를 사용하려면 `KUBETAIL_DEV_RUST_LOCAL` 환경 변수를 붙여 Tilt를 실행합니다:

  ```console
  KUBETAIL_DEV_RUST_LOCAL=true tilt up
  ```
</details>

## 빌드

### CLI 도구

Kubetail CLI 실행 파일(`kubetail`)을 빌드하려면 다음 명령을 실행합니다:

```console
make
```

빌드가 끝나면 실행 파일은 로컬 `bin/` 디렉터리에서 찾을 수 있습니다.

### Dashboard

프로덕션 배포용 Kubetail Dashboard 서버의 Docker 이미지를 빌드하려면 다음 명령을 실행합니다:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

프로덕션 배포용 Kubetail Cluster API 서버의 Docker 이미지를 빌드하려면 다음 명령을 실행합니다:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

프로덕션 배포용 Kubetail Cluster Agent의 Docker 이미지를 빌드하려면 다음 명령을 실행합니다:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## 참여하기

저희는 Kubernetes를 위한 가장 **사용자 친화적**이고, **비용 효율적**이며, **안전한** 로깅 플랫폼을 만들고 있습니다. 다음과 같은 방식으로 기여할 수 있습니다:

* UI/UX 디자인
* React 프런트엔드 개발
* 이슈 제보 및 기능 제안

hello@kubetail.com 으로 연락하거나 [Discord 서버](https://discord.gg/CmsmWAVkvX) 또는 [Slack 채널](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w)에 참여해 주세요.
