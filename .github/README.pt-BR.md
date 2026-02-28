# Kubetail

_Kubetail e um painel de logs em tempo real para Kubernetes_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Deutsch](README.de.md) | [Español](README.es.md) | Português | [Français](README.fr.md)

## Introducao

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** e um painel de logs de uso geral para Kubernetes, otimizado para acompanhar logs em tempo real em workloads com varios containers. Com o Kubetail, voce pode visualizar os logs de todos os containers de um workload (por exemplo, Deployment ou DaemonSet) combinados em uma unica linha do tempo cronologica, entregue no navegador ou no terminal.

O principal ponto de entrada do Kubetail e a ferramenta de CLI `kubetail`, que pode abrir um dashboard web local no seu desktop ou transmitir logs brutos diretamente para o terminal. Nos bastidores, o Kubetail usa a API Kubernetes do seu cluster para buscar logs diretamente do cluster, entao ele funciona imediatamente sem precisar encaminhar seus logs para um servico externo. O Kubetail tambem usa a API Kubernetes para acompanhar eventos do ciclo de vida dos containers e manter a linha do tempo dos logs sincronizada conforme os containers iniciam, param ou sao substituidos. Isso facilita acompanhar logs sem interrupcao quando requisicoes de usuarios passam de um container efemero para outro entre servicos.

Nosso objetivo e construir a plataforma de logging para Kubernetes mais poderosa e facil de usar, e gostariamos muito da sua ajuda. Se voce encontrar um bug ou tiver alguma sugestao, abra uma GitHub Issue ou envie um email para hello@kubetail.com.

## Recursos

* Interface limpa e facil de usar
* Visualize mensagens de log em tempo real
* Filtre logs por:
  * Workload (por exemplo, Deployment, CronJob, StatefulSet)
  * Intervalo de tempo absoluto ou relativo
  * Propriedades do node (por exemplo, zona de disponibilidade, arquitetura de CPU, ID do node)
  * Grep
* Usa sua API Kubernetes para recuperar mensagens de log, entao os dados nunca saem do seu controle (privado por padrao)
* Alterne entre varios clusters (somente desktop)
* Execute em qualquer lugar: Desktop, Cluster, Docker

## Inicio rapido

### Instalacao

Para instalar o `kubetail`, voce pode usar [Homebrew](https://brew.sh/):

```console
brew install kubetail
```

<details>
  <summary>Veja outras 15 opcoes (por exemplo, Krew, Snap, Winget, Ubuntu, Fedora, SUSE, Alpine, Arch, Gentoo, Nix, asdf, Chocolatey, Scoop, MacPorts)</summary>
  
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

Se preferir, voce tambem pode baixar os [binarios de release](https://github.com/kubetail-org/kubetail/releases/latest) ou usar nosso script de instalacao:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### Uso

Aqui estao algumas formas de usar o `kubetail`:

**1. Inicie o dashboard web (GUI)**

```console
kubetail serve
```

**2. Veja logs no terminal**

```console
kubetail logs -f deployments/my-app
```

**3. Habilite recursos avancados (instalando a API do Kubetail)**

```console
kubetail cluster install
```

**4. Inicialize um arquivo de configuracao local**

```console
kubetail config init
```

Consulte a documentacao para ver a lista completa de [commands](https://www.kubetail.com/docs/cli#subcommands). Bom tail dos seus logs.

## Execute em qualquer lugar

Além de executar o Kubetail no seu desktop, voce tambem pode executa-lo nestes ambientes:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## Documentacao

Visite nossa documentacao completa em [https://www.kubetail.com](https://www.kubetail.com/).

## Roadmap e status

Este e o nosso plano de alto nivel para o projeto Kubetail, em ordem:

|   | Etapa                                                 | Status |
| - | ----------------------------------------------------- | ------ |
| 1 | Logs de containers em tempo real                      | ✅     |
| 2 | Busca em tempo real e experiencia de usuario refinada | 🛠️     |
| 3 | Logs de sistema em tempo real (por exemplo, systemd, k8s events) | 🔲 |
| 4 | Personalizacao basica (por exemplo, cores, formatos de hora) | 🔲 |
| 5 | Analise de mensagens e metricas                       | 🔲     |
| 6 | Dados historicos (por exemplo, arquivos de logs, series temporais de metricas) | 🔲 |
| 7 | API do Kubetail e bibliotecas cliente para desenvolvedores | 🔲 |
| N | Paz mundial                                           | 🔲     |

Aqui estao alguns detalhes adicionais:

**Logs de containers em tempo real**

Os usuarios podem visualizar de forma rapida e facil os logs dos containers dos pods que estao em execucao no cluster usando um dashboard web. Eles podem ver os logs organizados por workload e acompanhar as mensagens conforme containers efemeros sao criados e removidos. Tambem podem restringir a janela de visualizacao por timestamp e filtrar logs por propriedades de origem como regiao, zona e node.

**Busca em tempo real e experiencia de usuario refinada**

Os usuarios podem instalar o Kubetail facilmente em seus desktops e clusters. Por padrao, o Kubetail usa apenas a API Kubernetes para buscar dados basicos, como workloads em execucao e logs de containers. Se um usuario quiser funcionalidades mais avancadas, pode instalar servicos personalizados do Kubetail no cluster (isto e, "Kubetail Cluster API" e "Kubetail Cluster Agent", conhecidos coletivamente como "Kubetail API") e obter acesso a recursos como busca de logs, tamanhos de arquivos de log e timestamps do ultimo evento. Toda a experiencia de instalar, atualizar e desinstalar a Kubetail API e bem polida, e os usuarios conseguem ver seus logs com ferramentas igualmente poderosas no navegador e no terminal usando o dashboard web e a CLI do Kubetail.

**Logs de sistema em tempo real**

Os usuarios que instalarem a Kubetail API terao acesso imediato aos logs em nivel de node (por exemplo, systemd) e em nivel de cluster (por exemplo, Kubernetes events), visualizados em uma interface integrada que mostra os logs de containers em contexto com outras informacoes do sistema, como uso de CPU, uso de memoria e espaco em disco. Os logs de sistema podem ser vistos em tempo real, na mesma linha do tempo combinada dos outros logs. Os usuarios podem filtrar logs de sistema por timestamp e propriedades de origem.

**Personalizacao basica**

Os usuarios poderao personalizar completamente sua experiencia com o Kubetail ao usar o dashboard web e a CLI, modificando suas configuracoes de usuario. Essas configuracoes poderao ser alteradas manualmente em um arquivo de configuracao ou pela UI do dashboard. A experiencia sera bem refinada e funcionara de forma fluida em upgrades que adicionem, removam ou modifiquem configuracoes de usuario. Os usuarios tambem terao a opcao de sincronizar suas configuracoes entre varios dispositivos.

## Desenvolvimento

### Estrutura do repositorio

Este monorepo contem os seguintes modulos:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([crates/cluster_agent](crates/cluster_agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

Ele tambem contem o codigo-fonte do frontend do Kubetail Dashboard:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### Configurando o ambiente de desenvolvimento

#### Dependencias

* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Cluster compativel com Tilt](https://docs.tilt.dev/choosing_clusters.html) (por exemplo, [minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/), [docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop))
* [ctlptl](https://github.com/tilt-dev/ctlptl) (opcional)

#### Proximos passos

1. Crie um cluster de desenvolvimento Kubernetes [compativel com Tilt](https://docs.tilt.dev/choosing_clusters.html):

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. Inicie o ambiente de desenvolvimento:

```console
tilt up
```

3. Inicie o servidor do Dashboard:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. Execute a Dashboard UI localmente:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

Agora acesse o dashboard em [http://localhost:5173](http://localhost:5173).

<details>
  <summary><h3>Otimize o ambiente de desenvolvimento para Rust (opcional)</h3></summary>
  
  Por padrao, o ambiente de desenvolvimento compila builds "release" dos componentes Rust quando voce executa `tilt up`. Se quiser iterar mais rapidamente, voce pode fazer com que o Tilt compile o codigo Rust localmente usando builds "debug".

  #### Dependencias

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  #### Proximos passos

  Primeiro, instale o target Rust necessario para sua arquitetura:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  Em seguida, instale as ferramentas necessarias para o compilador cruzado do Rust:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  No macOS, adicione isto ao seu arquivo `~/.cargo/config.toml`:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  Por fim, para usar o compilador local, execute o Tilt com a variavel de ambiente `KUBETAIL_DEV_RUST_LOCAL`:

  ```console
  KUBETAIL_DEV_RUST_LOCAL=true tilt up
  ```
</details>

## Build

### Ferramenta CLI

Para compilar o executavel da ferramenta CLI do Kubetail (`kubetail`), execute o seguinte comando:

```console
make
```

Quando o processo de build terminar, voce encontrara o executavel no diretorio local `bin/`.

### Dashboard

Para compilar uma imagem Docker para um deploy de producao do servidor Kubetail Dashboard, execute o seguinte comando:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

Para compilar uma imagem Docker para um deploy de producao do servidor Kubetail Cluster API, execute o seguinte comando:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

Para compilar uma imagem Docker para um deploy de producao do Kubetail Cluster Agent, execute o seguinte comando:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## Envolva-se

Estamos construindo a plataforma de logging para Kubernetes mais **amigavel ao usuario**, **economica** e **segura**, e adoraríamos receber suas contribuicoes. Veja como voce pode ajudar:

* Design de UI/UX
* Desenvolvimento frontend com React
* Relatar problemas e sugerir recursos

Entre em contato pelo email hello@kubetail.com ou participe do nosso [servidor no Discord](https://discord.gg/CmsmWAVkvX) ou [canal no Slack](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
