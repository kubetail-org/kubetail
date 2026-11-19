# Kubetail

_Kubetail は Kubernetes 向けのリアルタイムロギングダッシュボードです_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

デモ: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](https://github.com/kubetail-org/.github/blob/main/CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](../README.md) | [简体中文](README.zh-CN.md) | 日本語 | [한국어](README.ko.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Português](README.pt-BR.md) | [Français](README.fr.md)

## 概要

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** は Kubernetes 向けの汎用ロギングダッシュボードで、複数コンテナのワークロードにまたがるログをリアルタイムで追跡する用途に最適化されています。Kubetail を使うと、ワークロード内のすべてのコンテナ（Deployment や DaemonSet など）のログを 1 つの時系列タイムラインに統合して、ブラウザまたはターミナルで確認できます。

Kubetail の主な入り口は `kubetail` CLI ツールです。ローカルの Web ダッシュボードをデスクトップ上で起動したり、生のログをそのままターミナルへストリーミングしたりできます。内部では、Kubetail はクラスターの Kubernetes API を使ってログを直接取得するため、外部サービスへログを転送しなくてもそのまま利用できます。また、Kubernetes API を使ってコンテナのライフサイクルイベントも追跡し、コンテナの起動、停止、置き換えに合わせてログのタイムラインを同期します。そのため、ユーザーリクエストがサービス間で一時的なコンテナをまたいで移動しても、途切れなくログを追跡できます。

私たちの目標は、Kubernetes 向けで最も強力かつ使いやすいロギングプラットフォームを作ることです。ぜひ力を貸してください。バグを見つけた場合や提案がある場合は、GitHub Issue を作成するか、<hello@kubetail.com> までメールしてください。

## 特徴

* すっきりして使いやすいインターフェース
* ログメッセージをリアルタイムで表示
* 次の条件でログをフィルタリング:
  * ワークロード（例: Deployment、CronJob、StatefulSet）
  * 絶対時間または相対時間の範囲
  * ノードの属性（例: アベイラビリティゾーン、CPU アーキテクチャ、ノード ID）
  * Grep
* Kubernetes API を使ってログメッセージを取得するため、データが手元から離れない（デフォルトでプライベート）
* 複数クラスターを切り替え可能（デスクトップ版のみ）
* どこでも実行可能: Desktop、Cluster、Docker

## クイックスタート

### インストール

`kubetail` をインストールするには、[Homebrew](https://brew.sh/) を使用できます:

```console
brew install kubetail
```

<details>
  <summary>Krew、Snap、Winget、Ubuntu、Fedora、SUSE、Alpine、Arch、Gentoo、Nix、asdf、Chocolatey、Scoop、MacPorts など、ほか 15 個の方法を見る</summary>
  
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

必要であれば、[リリースバイナリ](https://github.com/kubetail-org/kubetail/releases/latest)からダウンロードするか、インストールスクリプトを使うこともできます:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### 使い方

`kubetail` の利用例をいくつか紹介します:

**1. Web ダッシュボードを起動する（GUI）**

```console
kubetail serve
```

**2. ターミナルでログを表示する**

```console
kubetail logs -f deployments/my-app
```

**3. 高度な機能を有効にする（Kubetail API をインストール）**

```console
kubetail cluster install
```

**4. ローカル設定ファイルを初期化する**

```console
kubetail config init
```

[commands](https://www.kubetail.com/docs/cli#subcommands) の一覧はドキュメントを参照してください。ログの tail を楽しんでください。

## どこでも実行可能

Kubetail はデスクトップだけでなく、次の環境でも実行できます:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## ドキュメント

完全なドキュメントは [https://www.kubetail.com](https://www.kubetail.com/) で確認できます。

## ロードマップとステータス

これは Kubetail プロジェクトの大まかな計画です。順番に並んでいます:

|   | ステップ                                              | 状態   |
| - | ----------------------------------------------------- | ------ |
| 1 | リアルタイムのコンテナログ                            | ✅     |
| 2 | リアルタイム検索と洗練されたユーザー体験              | 🛠️     |
| 3 | リアルタイムのシステムログ（例: systemd、k8s events） | 🔲     |
| 4 | 基本的なカスタマイズ性（例: 色、時刻形式）            | 🔲     |
| 5 | メッセージ解析とメトリクス                            | 🔲     |
| 6 | 履歴データ（例: ログアーカイブ、メトリクス時系列）    | 🔲     |
| 7 | Kubetail API と開発者向けクライアントライブラリ       | 🔲     |
| N | 世界平和                                              | 🔲     |

追加の詳細は次のとおりです:

**リアルタイムのコンテナログ**

ユーザーはクラスター内で現在稼働している Pod のコンテナログを、Web ダッシュボードから迅速かつ簡単に表示できます。ワークロードごとに整理されたログを確認し、一時コンテナが作成・削除されてもログを追跡できます。さらに、タイムスタンプで表示範囲を絞り込み、リージョン、ゾーン、ノードなどのソース属性でもログをフィルタリングできます。

**リアルタイム検索と洗練されたユーザー体験**

ユーザーは Kubetail をデスクトップやクラスターに簡単にインストールできます。デフォルトでは、Kubetail は Kubernetes API のみを使用して、実行中のワークロードやコンテナログなどの基本データを取得します。より高度な機能が必要な場合は、クラスター内に Kubetail 独自のサービス（「Kubetail Cluster API」と「Kubetail Cluster Agent」。総称して「Kubetail API」）をインストールすることで、ログ検索、ログファイルサイズ、最終イベント時刻などの機能を利用できます。Kubetail API のインストール、アップグレード、アンインストールの体験は非常に洗練されており、ユーザーは Kubetail の Web ダッシュボードと CLI ツールを通じて、ブラウザとターミナルの両方で強力なログ閲覧機能を利用できます。

**リアルタイムのシステムログ**

Kubetail API をインストールしたユーザーは、ノードレベルのログ（例: systemd）やクラスター全体のログ（例: Kubernetes events）にすぐアクセスでき、CPU 使用率、メモリ使用量、ディスク容量などのシステム情報とあわせて表示される統合インターフェースで確認できます。システムログはリアルタイムで表示され、ほかのログと同じ統合タイムライン上に並びます。ユーザーはタイムスタンプやソース属性でシステムログをフィルタリングできます。

**基本的なカスタマイズ性**

ユーザーは Web ダッシュボードや CLI ツール利用時の Kubetail 体験を、ユーザー設定の変更によって完全にカスタマイズできるようになります。設定は config ファイルを手で編集するか、ダッシュボード UI から変更できます。設定の追加、削除、変更を伴うアップグレードでも、体験は非常に洗練され、シームレスに動作します。さらに、複数デバイス間で設定を同期する選択肢も提供されます。

## 開発

### リポジトリ構成

このモノレポはコンポーネントごとに 1 つのフォルダーを持ちます:

* [apps/web](../apps/web): Dashboard UI（React のシングルページアプリ）
* [dashboard](../dashboard): Dashboard のバックエンド。UI と、Kubernetes API の上の GraphQL API を提供します
* [cli](../cli): `kubetail` CLI
* [cluster/api](../cluster/api): Cluster API。クラスター内で動く GraphQL サーバーで、Cluster Agent を通してログを読みます
* [cluster/agent](../cluster/agent): Rust ワークスペース。各ノードで動く Cluster Agent（[cluster_agent](../cluster/agent/cluster_agent)）と、それがリンクするログファイルリーダー（[rgkl](../cluster/agent/rgkl)）を含みます
* [apps/desktop](../apps/desktop) と [apps/mobile](../apps/mobile): デスクトップアプリとモバイルアプリのプレースホルダー

共有フォルダーは次のとおりです:

* [libs/go-common](../libs/go-common): 他の Go コンポーネントがインポートする Go パッケージ
* [libs/proto](../libs/proto): Cluster API と Cluster Agent の間の gRPC 契約
* [libs/graphql](../libs/graphql): 各 GraphQL サーバーのスキーマ（`libs/graphql/<server>/`）
* [go.work](../go.work): `cli`、`dashboard`、`cluster/api`、`libs/go-common` の Go ワークスペース
* [config/default](../config/default): 各コンポーネントの、ドキュメント付きのデフォルト設定ファイル
* [e2e](../e2e): kind クラスターに対して実行する Python の E2E テスト
* [tools](../tools): CI が実行する Go ツール（独立したモジュール）
* [docs/adr](../docs/adr): Architecture Decision Records。設計がなぜ今の形なのかを記録します

どのフォルダーから始めるかは [CONTRIBUTING.md](../CONTRIBUTING.md#quick-reference) にあります。

### 開発環境のセットアップ

#### 依存関係

* [Go](https://go.dev/) 1.26
* [Node.js](https://nodejs.org/) 26 と [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Tilt と互換性のあるクラスター](https://docs.tilt.dev/choosing_clusters.html)（例: [minikube](https://minikube.sigs.k8s.io/docs/)、[kind](https://kind.sigs.k8s.io/docs/user/quick-start/)、[docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop)）
* [ctlptl](https://github.com/tilt-dev/ctlptl)（任意）

CI は Go と Node.js の正確なバージョンを [.github/actions/setup-environment/action.yml](actions/setup-environment/action.yml) で固定しています。`make` は CLI の前に UI をビルドするため、Go に加えて Node.js と pnpm が必要です。

#### 次の手順

1. [Tilt 対応](https://docs.tilt.dev/choosing_clusters.html)の Kubernetes 開発クラスターを作成します:

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. 開発環境を起動します:

```console
tilt up
```

3. Dashboard サーバーを起動します:

```console
cd dashboard
go run cmd/main.go -c hack/config.yaml
```

サーバーはポート 4000 で待ち受けます。UI を配信するのは `make build-dashboard-ui` がビルドを `dashboard/website/` にコピーした後だけです。それまではすべてのページルートが 404 を返すため、次の手順の開発サーバーから UI にアクセスします。

4. Dashboard UI をローカルで起動します:

```console
cd apps/web
pnpm install
pnpm dev
```

次に、[http://localhost:5173](http://localhost:5173) でダッシュボードへアクセスできます。開発サーバーは API リクエストをポート 4000 の Dashboard サーバーへプロキシします。

<details>
  <summary><h3>Rust 開発（任意）</h3></summary>
  
  デフォルトでは、開発環境は Rust ベースの cluster-agent にビルド済みイメージを使用します。Rust コードを開発している場合は、環境変数 `KUBETAIL_RUST_DEV_MODE` を使用して、Rust コードをコンパイルしバイナリをクラスタに同期する開発モードを有効にできます。2つのモードがあります:

  | モード   | 説明                                                                                                      |
  | -------- | --------------------------------------------------------------------------------------------------------- |
  | `docker` | Docker コンテナ内で Rust コードをコンパイルします。ローカルの Rust ツールチェーンは不要です。              |
  | `local`  | クロスコンパイルを使用してマシン上で Rust コードをコンパイルします。リビルドが高速ですがローカル設定が必要です。 |

  #### Docker モード

  Docker モードを使用するには、環境変数 `KUBETAIL_RUST_DEV_MODE` を `docker` に設定して Tilt を起動します:

  ```console
  KUBETAIL_RUST_DEV_MODE=docker tilt up
  ```

  #### ローカルモード

  ローカルモードは追加の依存関係と設定が必要ですが、より高速なリビルドが可能です。

  ##### 依存関係

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  ##### セットアップ

  まず、使用しているアーキテクチャに必要な Rust ターゲットをインストールします:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  次に、Rust クロスコンパイラに必要なツールをインストールします:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  macOS では、`~/.cargo/config.toml` に次を追加します:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  環境変数 `KUBETAIL_RUST_DEV_MODE` を `local` に設定して Tilt を起動します:

  ```console
  KUBETAIL_RUST_DEV_MODE=local tilt up
  ```
</details>

### チェック

変更のテスト方法とプルリクエストの作り方は [CONTRIBUTING.md](../CONTRIBUTING.md) にあります。プルリクエストを作る前に、ブランチで変更した部分のチェックを実行します:

```console
make check-changed
```

`make help` はすべてのターゲットを一覧表示します。各コンポーネントの `AGENTS.md`（例: [dashboard/AGENTS.md](../dashboard/AGENTS.md)）に、その規約とチェックがあります。

## ビルド

### CLI ツール

Kubetail CLI ツールの実行ファイル（`kubetail`）をビルドするには、次のコマンドを実行します:

```console
make
```

ビルドが完了すると、実行ファイルはローカルの `bin/` ディレクトリに生成されます。

### ダッシュボード

本番デプロイ用の Kubetail Dashboard サーバーの Docker イメージをビルドするには、次のコマンドを実行します:

```console
docker build -f dashboard/Dockerfile -t kubetail-dashboard:latest .
```

### Cluster API

本番デプロイ用の Kubetail Cluster API サーバーの Docker イメージをビルドするには、次のコマンドを実行します:

```console
docker build -f cluster/api/Dockerfile -t kubetail-cluster-api:latest .
```

### Cluster Agent

本番デプロイ用の Kubetail Cluster Agent の Docker イメージをビルドするには、次のコマンドを実行します:

```console
docker build -f cluster/agent/Dockerfile -t kubetail-cluster-agent:latest .
```

## 参加する

私たちは Kubernetes 向けで最も **使いやすく**、**コスト効率が高く**、**安全な** ロギングプラットフォームを構築しています。ぜひ貢献してください。参加方法は次のとおりです:

* UI/UX デザイン
* React フロントエンド開発
* Issue の報告や機能提案

hello@kubetail.com までご連絡いただくか、[Discord サーバー](https://discord.gg/CmsmWAVkvX) または [Slack チャンネル](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w) に参加してください。
