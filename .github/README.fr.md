# Kubetail

_Kubetail est un tableau de bord de logs en temps reel pour Kubernetes_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Português](README.pt-BR.md) | Français

## Introduction

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** est un tableau de bord de logs generaliste pour Kubernetes, optimise pour suivre en temps reel les logs de workloads multi-conteneurs. Avec Kubetail, vous pouvez consulter les logs de tous les conteneurs d'un workload (par exemple un Deployment ou un DaemonSet) fusionnes dans une seule timeline chronologique, accessible depuis votre navigateur ou votre terminal.

Le point d'entree principal de Kubetail est l'outil CLI `kubetail`, qui peut lancer un tableau de bord web local sur votre poste ou diffuser directement des logs bruts dans votre terminal. En interne, Kubetail utilise l'API Kubernetes de votre cluster pour recuperer les logs directement depuis le cluster, ce qui lui permet de fonctionner immediatement sans devoir envoyer vos logs vers un service externe. Kubetail utilise aussi l'API Kubernetes pour suivre les evenements du cycle de vie des conteneurs et garder la timeline synchronisee lorsque des conteneurs demarrent, s'arretent ou sont remplaces. Cela permet de suivre les logs sans interruption lorsque des requetes utilisateur passent d'un conteneur ephemere a un autre entre plusieurs services.

Notre objectif est de construire la plateforme de logging Kubernetes la plus puissante et la plus simple a utiliser, et nous aimerions beaucoup votre aide. Si vous remarquez un bug ou avez une suggestion, merci de creer une GitHub Issue ou de nous envoyer un email a hello@kubetail.com.

## Fonctionnalites

* Interface claire et facile a utiliser
* Affichage des messages de log en temps reel
* Filtrage des logs par:
  * Workload (par exemple Deployment, CronJob, StatefulSet)
  * Plage de temps absolue ou relative
  * Proprietes du node (par exemple zone de disponibilite, architecture CPU, ID du node)
  * Grep
* Utilise votre API Kubernetes pour recuperer les logs, les donnees ne quittent donc jamais votre controle (prive par defaut)
* Bascule entre plusieurs clusters (desktop uniquement)
* Execution partout: Desktop, Cluster, Docker

## Demarrage rapide

### Installation

Pour installer `kubetail`, vous pouvez utiliser [Homebrew](https://brew.sh/):

```console
brew install kubetail
```

<details>
  <summary>Voir 15 autres options (par exemple Krew, Snap, Winget, Ubuntu, Fedora, SUSE, Alpine, Arch, Gentoo, Nix, asdf, Chocolatey, Scoop, MacPorts)</summary>
  
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

Si vous preferez, vous pouvez aussi le telecharger depuis les [binaires de release](https://github.com/kubetail-org/kubetail/releases/latest) ou utiliser notre script d'installation:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### Utilisation

Voici quelques facons d'utiliser `kubetail`:

**1. Demarrez le tableau de bord web (GUI)**

```console
kubetail serve
```

**2. Affichez les logs dans votre terminal**

```console
kubetail logs -f deployments/my-app
```

**3. Activez les fonctionnalites avancees (en installant l'API Kubetail)**

```console
kubetail cluster install
```

**4. Initialisez un fichier de configuration local**

```console
kubetail config init
```

Consultez la documentation pour la liste complete des [commands](https://www.kubetail.com/docs/cli#subcommands). Bon tail de vos logs.

## Execution partout

En plus d'executer Kubetail sur votre poste, vous pouvez aussi l'executer dans ces environnements:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## Documentation

Consultez notre documentation complete sur [https://www.kubetail.com](https://www.kubetail.com/).

## Feuille de route et statut

Voici notre plan de haut niveau pour le projet Kubetail, dans l'ordre:

|   | Etape                                                 | Statut |
| - | ----------------------------------------------------- | ------ |
| 1 | Logs de conteneurs en temps reel                      | ✅     |
| 2 | Recherche en temps reel et experience utilisateur aboutie | 🛠️ |
| 3 | Logs systeme en temps reel (par exemple systemd, k8s events) | 🔲 |
| 4 | Personnalisation de base (par exemple couleurs, formats horaires) | 🔲 |
| 5 | Analyse des messages et metriques                     | 🔲     |
| 6 | Donnees historiques (par exemple archives de logs, series temporelles de metriques) | 🔲 |
| 7 | API Kubetail et bibliotheques clientes pour les developpeurs | 🔲 |
| N | Paix dans le monde                                    | 🔲     |

Voici quelques details supplementaires:

**Logs de conteneurs en temps reel**

Les utilisateurs peuvent consulter rapidement et facilement les logs des conteneurs des pods actuellement en cours d'execution dans leurs clusters via un tableau de bord web. Ils peuvent voir les logs organises par workload et suivre les messages lorsqu'un conteneur ephemere est cree puis supprime. Ils peuvent aussi restreindre la fenetre d'affichage par horodatage et filtrer les logs selon des proprietes de source comme la region, la zone ou le node.

**Recherche en temps reel et experience utilisateur aboutie**

Les utilisateurs peuvent installer Kubetail facilement sur leur poste et dans leurs clusters. Par defaut, Kubetail utilise uniquement l'API Kubernetes pour recuperer des donnees de base comme les workloads en cours d'execution et les logs de conteneurs. Si un utilisateur souhaite des fonctionnalites plus avancees, il peut installer des services Kubetail specifiques dans son cluster (c'est-a-dire "Kubetail Cluster API" et "Kubetail Cluster Agent", appeles collectivement la "Kubetail API") et acceder a des fonctionnalites comme la recherche dans les logs, la taille des fichiers de logs et les horodatages du dernier evenement. L'experience complete d'installation, de mise a niveau et de desinstallation de la Kubetail API est tres soignee, et les utilisateurs peuvent consulter leurs logs avec des outils tout aussi puissants dans le navigateur et dans le terminal via le tableau de bord web Kubetail et l'outil CLI.

**Logs systeme en temps reel**

Les utilisateurs qui installent la Kubetail API obtiennent un acces immediat a leurs logs au niveau du node (par exemple systemd) et du cluster (par exemple Kubernetes events), visibles dans une interface integree qui replace les logs de conteneurs dans le contexte d'autres informations systeme comme l'utilisation CPU, l'utilisation memoire et l'espace disque. Les logs systeme sont visibles en temps reel, dans la meme timeline fusionnee que les autres logs. Les utilisateurs peuvent filtrer les logs systeme par horodatage et par proprietes de source.

**Personnalisation de base**

Les utilisateurs pourront personnaliser completement leur experience Kubetail via leurs parametres utilisateur lorsqu'ils utilisent le tableau de bord web et l'outil CLI. Ces parametres pourront etre modifies a la main dans un fichier de configuration ou via l'interface du dashboard. L'experience sera tres soignee et fonctionnera de facon fluide lors des mises a niveau qui ajoutent, suppriment ou modifient des parametres utilisateur. Les utilisateurs auront aussi la possibilite de synchroniser leurs parametres entre plusieurs appareils.

## Developpement

### Structure du depot

Ce monorepo contient les modules suivants:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([crates/cluster_agent](crates/cluster_agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

Il contient aussi le code source du frontend du Kubetail Dashboard:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### Mise en place de l'environnement de developpement

#### Dependances

* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Cluster compatible avec Tilt](https://docs.tilt.dev/choosing_clusters.html) (par exemple [minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/), [docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop))
* [ctlptl](https://github.com/tilt-dev/ctlptl) (optionnel)

#### Etapes suivantes

1. Creez un cluster de developpement Kubernetes [compatible avec Tilt](https://docs.tilt.dev/choosing_clusters.html):

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. Demarrez l'environnement de developpement:

```console
tilt up
```

3. Demarrez le serveur Dashboard:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. Lancez la Dashboard UI localement:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

Vous pouvez maintenant acceder au dashboard sur [http://localhost:5173](http://localhost:5173).

<details>
  <summary><h3>Optimiser l'environnement de developpement pour Rust (optionnel)</h3></summary>
  
  Par defaut, l'environnement de developpement compile des builds "release" des composants Rust lorsque vous executez `tilt up`. Si vous voulez iterer plus vite, vous pouvez faire compiler le code Rust localement par Tilt en builds "debug".

  #### Dependances

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  #### Etapes suivantes

  Installez d'abord la cible Rust necessaire a votre architecture:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  Ensuite, installez les outils necessaires au compilateur croise Rust:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  Sur macOS, ajoutez ceci a votre fichier `~/.cargo/config.toml`:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  Enfin, pour utiliser le compilateur local, lancez Tilt avec la variable d'environnement `KUBETAIL_DEV_RUST_LOCAL`:

  ```console
  KUBETAIL_DEV_RUST_LOCAL=true tilt up
  ```
</details>

## Build

### Outil CLI

Pour compiler l'executable de l'outil CLI Kubetail (`kubetail`), executez la commande suivante:

```console
make
```

Une fois la compilation terminee, vous trouverez l'executable dans le repertoire local `bin/`.

### Dashboard

Pour construire une image Docker pour un deploiement de production du serveur Kubetail Dashboard, executez la commande suivante:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

Pour construire une image Docker pour un deploiement de production du serveur Kubetail Cluster API, executez la commande suivante:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

Pour construire une image Docker pour un deploiement de production du Kubetail Cluster Agent, executez la commande suivante:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## Participer

Nous construisons la plateforme de logging pour Kubernetes la plus **simple a utiliser**, la plus **economique** et la plus **secure**, et nous aimerions beaucoup vos contributions. Voici comment vous pouvez aider:

* Design UI/UX
* Developpement frontend React
* Signaler des problemes et proposer des fonctionnalites

Contactez-nous a hello@kubetail.com, ou rejoignez notre [serveur Discord](https://discord.gg/CmsmWAVkvX) ou notre [canal Slack](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
