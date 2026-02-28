# Kubetail

_Kubetail ist ein Echtzeit-Logging-Dashboard für Kubernetes_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | Deutsch | [Español](README.es.md) | [Português](README.pt-BR.md) | [Français](README.fr.md)

## Einführung

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** ist ein allgemeines Logging-Dashboard für Kubernetes, das für das Echtzeit-Tailing von Logs über Multi-Container-Workloads hinweg optimiert ist. Mit Kubetail kannst du Logs aller Container in einem Workload (z. B. Deployment oder DaemonSet) in einer einzigen chronologischen Timeline zusammengeführt im Browser oder Terminal ansehen.

Der primäre Einstiegspunkt für Kubetail ist das CLI-Tool `kubetail`, das lokal ein Web-Dashboard auf deinem Desktop starten oder rohe Logs direkt in dein Terminal streamen kann. Im Hintergrund verwendet Kubetail die Kubernetes-API deines Clusters, um Logs direkt aus dem Cluster abzurufen. Dadurch funktioniert es sofort, ohne dass Logs an einen externen Dienst weitergeleitet werden müssen. Kubetail nutzt die Kubernetes-API außerdem, um Container-Lifecycle-Events zu verfolgen und die Log-Timeline zu synchronisieren, wenn Container starten, stoppen oder ersetzt werden. So lassen sich Logs nahtlos weiterverfolgen, auch wenn Benutzeranfragen über Services hinweg von einem kurzlebigen Container zum nächsten wechseln.

Unser Ziel ist es, die leistungsfähigste und benutzerfreundlichste Logging-Plattform für Kubernetes zu bauen, und wir freuen uns über jede Unterstützung. Wenn dir ein Bug auffällt oder du eine Idee hast, erstelle bitte ein GitHub Issue oder schreib uns an hello@kubetail.com.

## Funktionen

* Saubere, leicht bedienbare Oberfläche
* Log-Nachrichten in Echtzeit anzeigen
* Logs filtern nach:
  * Workload (z. B. Deployment, CronJob, StatefulSet)
  * Absolutem oder relativem Zeitbereich
  * Node-Eigenschaften (z. B. Availability Zone, CPU-Architektur, Node-ID)
  * Grep
* Verwendet deine Kubernetes-API zum Abrufen von Logs, sodass die Daten deinen Besitz nie verlassen (standardmäßig privat)
* Zwischen mehreren Clustern wechseln (nur Desktop)
* Überall ausführen: Desktop, Cluster, Docker

## Schnellstart

### Installation

Du kannst `kubetail` mit [Homebrew](https://brew.sh/) installieren:

```console
brew install kubetail
```

<details>
  <summary>15 weitere Optionen anzeigen (z. B. Krew, Snap, Winget, Ubuntu, Fedora, SUSE, Alpine, Arch, Gentoo, Nix, asdf, Chocolatey, Scoop, MacPorts)</summary>
  
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

Alternativ kannst du es auch aus den [Release-Binaries](https://github.com/kubetail-org/kubetail/releases/latest) herunterladen oder unser Installationsskript verwenden:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### Verwendung

Hier sind einige Möglichkeiten, `kubetail` zu verwenden:

**1. Starte das Web-Dashboard (GUI)**

```console
kubetail serve
```

**2. Zeige Logs im Terminal an**

```console
kubetail logs -f deployments/my-app
```

**3. Aktiviere erweiterte Funktionen (durch Installation der Kubetail API)**

```console
kubetail cluster install
```

**4. Initialisiere eine lokale Konfigurationsdatei**

```console
kubetail config init
```

In der Dokumentation findest du die vollständige Liste der [commands](https://www.kubetail.com/docs/cli#subcommands). Viel Spaß beim Tailing deiner Logs.

## Überall ausführen

Zusätzlich zum Betrieb auf dem Desktop kannst du Kubetail auch in diesen Umgebungen ausführen:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## Dokumentation

Die vollständige Dokumentation findest du unter [https://www.kubetail.com](https://www.kubetail.com/).

## Roadmap und Status

Das ist unser grober Plan für das Kubetail-Projekt, in dieser Reihenfolge:

|   | Schritt                                               | Status |
| - | ----------------------------------------------------- | ------ |
| 1 | Echtzeit-Container-Logs                               | ✅     |
| 2 | Echtzeit-Suche und ausgereifte User Experience        | 🛠️     |
| 3 | Echtzeit-Systemlogs (z. B. systemd, k8s events)       | 🔲     |
| 4 | Grundlegende Anpassbarkeit (z. B. Farben, Zeitformate)| 🔲     |
| 5 | Nachrichtenanalyse und Metriken                       | 🔲     |
| 6 | Historische Daten (z. B. Log-Archive, Metrik-Zeitreihen) | 🔲  |
| 7 | Kubetail API und entwicklerorientierte Client-Bibliotheken | 🔲 |
| N | Weltfrieden                                           | 🔲     |

Hier sind noch einige zusätzliche Details:

**Echtzeit-Container-Logs**

Benutzer können die Container-Logs der aktuell im Cluster laufenden Pods schnell und einfach über ein Web-Dashboard anzeigen. Die Logs lassen sich nach Workloads organisiert betrachten, und Log-Nachrichten können weiterverfolgt werden, während kurzlebige Container erstellt und wieder gelöscht werden. Außerdem lässt sich das Sichtfenster per Zeitstempel eingrenzen, und Logs können nach Quell-Eigenschaften wie Region, Zone und Node gefiltert werden.

**Echtzeit-Suche und ausgereifte User Experience**

Benutzer können Kubetail einfach auf ihren Desktops und in ihren Clustern installieren. Standardmäßig verwendet Kubetail ausschließlich die Kubernetes-API, um grundlegende Daten wie laufende Workloads und Container-Logs abzurufen. Wenn ein Benutzer erweiterte Funktionen benötigt, kann er benutzerdefinierte Kubetail-Dienste im Cluster installieren (also "Kubetail Cluster API" und "Kubetail Cluster Agent", zusammen als "Kubetail API" bezeichnet) und so Funktionen wie Log-Suche, Log-Dateigrößen und Zeitstempel des letzten Events erhalten. Die gesamte Erfahrung rund um Installation, Upgrade und Deinstallation der Kubetail API ist sehr ausgereift, und Benutzer können ihre Logs sowohl im Browser als auch im Terminal mit gleich leistungsfähigen Werkzeugen über das Kubetail Web-Dashboard und das CLI-Tool anzeigen.

**Echtzeit-Systemlogs**

Benutzer, die die Kubetail API installieren, erhalten sofort Zugriff auf ihre Logs auf Node-Ebene (z. B. systemd) und Cluster-Ebene (z. B. Kubernetes events) und sehen diese in einer integrierten Oberfläche, die Container-Logs im Kontext weiterer Systeminformationen wie CPU-Auslastung, Speichernutzung und Plattenplatz anzeigt. Systemlogs sind in Echtzeit sichtbar, in derselben zusammengeführten Timeline wie andere Logs. Benutzer können Systemlogs nach Zeitstempel und Quell-Eigenschaften filtern.

**Grundlegende Anpassbarkeit**

Benutzer können ihre Kubetail-Erfahrung bei der Nutzung des Web-Dashboards und CLI-Tools vollständig über ihre Benutzereinstellungen anpassen. Diese Einstellungen können entweder per Konfigurationsdatei von Hand oder über die Dashboard-UI geändert werden. Die Erfahrung bleibt auch bei Upgrades sehr ausgereift und funktioniert nahtlos, selbst wenn dabei Einstellungen hinzugefügt, entfernt oder geändert werden. Benutzer haben außerdem die Möglichkeit, ihre Einstellungen über mehrere Geräte hinweg zu synchronisieren.

## Entwicklung

### Repository-Struktur

Dieses Monorepo enthält die folgenden Module:

* Kubetail CLI ([modules/cli](modules/cli))
* Kubetail Cluster API ([modules/cluster-api](modules/cluster-api))
* Kubetail Cluster Agent ([crates/cluster_agent](crates/cluster_agent))
* Kubetail Dashboard ([modules/dashboard](modules/dashboard))

Es enthält außerdem den Quellcode für das Frontend des Kubetail Dashboards:

* Dashboard UI ([dashboard-ui](dashboard-ui))

### Entwicklungsumgebung einrichten

#### Abhängigkeiten

* [Go](https://go.dev/)
* [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Tilt-kompatibler Cluster](https://docs.tilt.dev/choosing_clusters.html) (z. B. [minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/), [docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop))
* [ctlptl](https://github.com/tilt-dev/ctlptl) (optional)

#### Nächste Schritte

1. Erstelle einen [Tilt-kompatiblen](https://docs.tilt.dev/choosing_clusters.html) Kubernetes-Dev-Cluster:

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. Starte die Entwicklungsumgebung:

```console
tilt up
```

3. Starte den Dashboard-Server:

```console
cd modules/dashboard
go run cmd/main.go -c hack/config.yaml
```

4. Starte die Dashboard-UI lokal:

```console
cd dashboard-ui
pnpm install
pnpm dev
```

Anschließend kannst du das Dashboard unter [http://localhost:5173](http://localhost:5173) aufrufen.

<details>
  <summary><h3>Entwicklungsumgebung für Rust optimieren (optional)</h3></summary>
  
  Standardmäßig kompiliert die Entwicklungsumgebung beim Ausführen von `tilt up` die Rust-Komponenten als "release"-Builds. Wenn du schneller iterieren möchtest, kannst du Tilt stattdessen so konfigurieren, dass der Rust-Code lokal als "debug"-Build kompiliert wird.

  #### Abhängigkeiten

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  #### Nächste Schritte

  Installiere zuerst das für deine Architektur benötigte Rust-Target:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  Installiere als Nächstes die Werkzeuge, die der Rust-Cross-Compiler benötigt:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  Unter macOS füge Folgendes zu deiner Datei `~/.cargo/config.toml` hinzu:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  Um schließlich den lokalen Compiler zu verwenden, starte Tilt einfach mit der Umgebungsvariable `KUBETAIL_DEV_RUST_LOCAL`:

  ```console
  KUBETAIL_DEV_RUST_LOCAL=true tilt up
  ```
</details>

## Build

### CLI-Tool

Um das ausführbare Kubetail CLI-Tool (`kubetail`) zu bauen, führe den folgenden Befehl aus:

```console
make
```

Nach Abschluss des Builds findest du die ausführbare Datei im lokalen Verzeichnis `bin/`.

### Dashboard

Um ein Docker-Image für ein Produktions-Deployment des Kubetail Dashboard-Servers zu bauen, führe den folgenden Befehl aus:

```console
docker build -f build/package/Dockerfile.dashboard -t kubetail-dashboard:latest .
```

### Cluster API

Um ein Docker-Image für ein Produktions-Deployment des Kubetail Cluster API-Servers zu bauen, führe den folgenden Befehl aus:

```console
docker build -f build/package/Dockerfile.cluster-api -t kubetail-cluster-api:latest .
```

### Cluster Agent

Um ein Docker-Image für ein Produktions-Deployment des Kubetail Cluster Agents zu bauen, führe den folgenden Befehl aus:

```console
docker build -f build/package/Dockerfile.cluster-agent -t kubetail-cluster-agent:latest .
```

## Mitmachen

Wir bauen die **benutzerfreundlichste**, **kosteneffizienteste** und **sicherste** Logging-Plattform für Kubernetes und freuen uns über deine Beiträge. So kannst du helfen:

* UI/UX-Design
* React-Frontend-Entwicklung
* Issues melden und Features vorschlagen

Du erreichst uns unter hello@kubetail.com oder in unserem [Discord-Server](https://discord.gg/CmsmWAVkvX) bzw. [Slack-Kanal](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
