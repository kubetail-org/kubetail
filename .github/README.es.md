# Kubetail

_Kubetail es un panel de registro en tiempo real para Kubernetes_

<a href="https://youtu.be/q9rV9gHQb4Q">
  <img width="350" alt="demo-thumbnail" src="https://github.com/user-attachments/assets/3b528e7e-5f8a-4bfd-86a1-0b70691b8a4c">
</a>

Demo: [https://www.kubetail.com/demo](https://www.kubetail.com/demo)

<a href="https://discord.gg/CmsmWAVkvX"><img src="https://img.shields.io/discord/1212031524216770650?logo=Discord&style=flat-square&logoColor=FFFFFF&labelColor=5B65F0&label=Discord&color=64B73A"></a>
[![Slack](https://img.shields.io/badge/Slack-kubetail-364954?logo=slack&labelColor=4D1C51)](https://kubernetes.slack.com/archives/C08SHG1GR37)
[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](https://github.com/kubetail-org/.github/blob/main/CODE_OF_CONDUCT.md)
[![Contributor Resources](https://img.shields.io/badge/Contributor%20Resources-purple?style=flat-square)](https://github.com/kubetail-org)

[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Deutsch](README.de.md) | Español | [Português](README.pt-BR.md) | [Français](README.fr.md)

## Introducción

<img src="https://github.com/user-attachments/assets/3713a774-1b3a-41f9-8e9d-9331bbf8acac" width="300" title="Kubetail">

<br>
<br>

**Kubetail** es un panel de registro de propósito general para Kubernetes, optimizado para seguir logs en tiempo real a través de cargas de trabajo con múltiples contenedores. Con Kubetail, puedes ver los logs de todos los contenedores de una carga de trabajo (por ejemplo, un Deployment o un DaemonSet) combinados en una única línea temporal cronológica, disponible en el navegador o en la terminal.

El punto de entrada principal de Kubetail es la herramienta CLI `kubetail`, que puede abrir un panel web local en tu escritorio o transmitir logs sin procesar directamente a tu terminal. Por debajo, Kubetail usa la API de Kubernetes de tu clúster para obtener los logs directamente, por lo que funciona desde el primer momento sin necesidad de reenviar tus logs a un servicio externo. Kubetail también usa la API de Kubernetes para seguir eventos del ciclo de vida de los contenedores y mantener sincronizada la línea temporal de logs cuando los contenedores arrancan, se detienen o son reemplazados. Esto facilita seguir los logs sin interrupciones cuando las solicitudes de usuario pasan de un contenedor efímero a otro entre servicios.

Nuestro objetivo es construir la plataforma de logging para Kubernetes más potente y fácil de usar, y nos encantaría contar con tu ayuda. Si detectas un bug o tienes una sugerencia, crea un GitHub Issue o escríbenos a hello@kubetail.com.

## Características

* Interfaz limpia y fácil de usar
* Ver mensajes de log en tiempo real
* Filtrar logs por:
  * Carga de trabajo (por ejemplo, Deployment, CronJob, StatefulSet)
  * Rango de tiempo absoluto o relativo
  * Propiedades del nodo (por ejemplo, zona de disponibilidad, arquitectura de CPU, ID del nodo)
  * Grep
* Usa tu API de Kubernetes para recuperar mensajes de log, así que los datos nunca salen de tu control (privado por defecto)
* Cambia entre varios clústeres (solo escritorio)
* Ejecútalo en cualquier lugar: Desktop, Cluster, Docker

## Inicio rápido

### Instalación

Para instalar `kubetail`, puedes usar [Homebrew](https://brew.sh/):

```console
brew install kubetail
```

<details>
  <summary>Ver otras 15 opciones (por ejemplo, Krew, Snap, Winget, Ubuntu, Fedora, SUSE, Alpine, Arch, Gentoo, Nix, asdf, Chocolatey, Scoop, MacPorts)</summary>
  
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

Si lo prefieres, también puedes descargarlo desde los [binarios de las versiones](https://github.com/kubetail-org/kubetail/releases/latest) o usar nuestro script de instalación:

```console
curl -sS https://www.kubetail.com/install.sh | bash
```

### Uso

Estas son algunas formas de usar `kubetail`:

**1. Inicia el panel web (GUI)**

```console
kubetail serve
```

**2. Ve los logs en tu terminal**

```console
kubetail logs -f deployments/my-app
```

**3. Habilita funciones avanzadas (instalando la API de Kubetail)**

```console
kubetail cluster install
```

**4. Inicializa un archivo de configuración local**

```console
kubetail config init
```

Consulta la documentación para ver la lista completa de [commands](https://www.kubetail.com/docs/cli#subcommands). Disfruta siguiendo tus logs.

## Ejecútalo en cualquier lugar

Además de ejecutar Kubetail en tu escritorio, también puedes usarlo en estos entornos:

* [Cluster](https://www.kubetail.com/docs/getting-started/cluster/install)
* [Docker](https://www.kubetail.com/docs/getting-started/docker)
* [Minikube](https://www.kubetail.com/docs/getting-started/cluster/install#minikube)

## Documentación

Visita la documentación completa en [https://www.kubetail.com](https://www.kubetail.com/).

## Hoja de ruta y estado

Este es nuestro plan de alto nivel para el proyecto Kubetail, en orden:

|   | Paso                                                  | Estado |
| - | ----------------------------------------------------- | ------ |
| 1 | Logs de contenedores en tiempo real                   | ✅     |
| 2 | Búsqueda en tiempo real y experiencia de usuario pulida | 🛠️   |
| 3 | Logs del sistema en tiempo real (por ejemplo, systemd, k8s events) | 🔲 |
| 4 | Personalización básica (por ejemplo, colores, formatos de hora) | 🔲 |
| 5 | Análisis de mensajes y métricas                       | 🔲     |
| 6 | Datos históricos (por ejemplo, archivos de logs, series temporales de métricas) | 🔲 |
| 7 | API de Kubetail y bibliotecas cliente para desarrolladores | 🔲  |
| N | Paz mundial                                           | 🔲     |

Y aquí tienes algunos detalles adicionales:

**Logs de contenedores en tiempo real**

Los usuarios pueden ver rápida y fácilmente los logs de contenedores de los pods que se están ejecutando en sus clústeres mediante un panel web. Pueden ver los logs organizados por cargas de trabajo y seguirlos mientras se crean y eliminan contenedores efímeros. También pueden reducir la ventana de visualización por marca de tiempo y filtrar los logs por propiedades de origen como región, zona y nodo.

**Búsqueda en tiempo real y experiencia de usuario pulida**

Los usuarios pueden instalar Kubetail fácilmente en sus escritorios y en sus clústeres. De forma predeterminada, Kubetail usa solo la API de Kubernetes para obtener datos básicos como las cargas de trabajo en ejecución y los logs de contenedores. Si un usuario necesita funcionalidad más avanzada, puede instalar servicios personalizados de Kubetail en su clúster (es decir, "Kubetail Cluster API" y "Kubetail Cluster Agent", conocidos en conjunto como la "Kubetail API") y obtener acceso a funciones como búsqueda de logs, tamaños de archivos de log y marcas de tiempo del último evento. Toda la experiencia de instalar, actualizar y desinstalar la Kubetail API está muy cuidada, y los usuarios pueden ver sus logs con herramientas igual de potentes en el navegador y en la terminal mediante el panel web y la CLI de Kubetail.

**Logs del sistema en tiempo real**

Los usuarios que instalen la Kubetail API obtendrán acceso inmediato a logs a nivel de nodo (por ejemplo, systemd) y a nivel de clúster (por ejemplo, Kubernetes events), y podrán verlos en una interfaz integrada que muestra sus logs de contenedores en contexto con otra información del sistema como uso de CPU, uso de memoria y espacio en disco. Los logs del sistema se pueden ver en tiempo real, en la misma línea temporal combinada que el resto de logs. Los usuarios pueden filtrar los logs del sistema por marca de tiempo y propiedades de origen.

**Personalización básica**

Los usuarios podrán personalizar por completo su experiencia con Kubetail al usar el panel web y la CLI modificando sus ajustes de usuario. Estos ajustes se podrán cambiar manualmente mediante un archivo de configuración o desde la interfaz del panel. La experiencia estará muy pulida y funcionará sin problemas incluso cuando las actualizaciones añadan, eliminen o modifiquen ajustes. Los usuarios también tendrán la opción de sincronizar sus ajustes entre varios dispositivos.

## Desarrollo

### Estructura del repositorio

Este monorepo tiene una carpeta por componente:

* [apps/web](../apps/web): la Dashboard UI, una aplicación React de una sola página
* [dashboard](../dashboard): el backend del Dashboard, que sirve la UI y una API GraphQL sobre la API de Kubernetes
* [cli](../cli): la CLI `kubetail`
* [cluster/api](../cluster/api): la Cluster API, un servidor GraphQL dentro del clúster que lee los logs a través de los Cluster Agents
* [cluster/agent](../cluster/agent): un workspace de Rust con el Cluster Agent ([cluster_agent](../cluster/agent/cluster_agent)), que se ejecuta en cada nodo, y el lector de archivos de log que enlaza ([rgkl](../cluster/agent/rgkl))
* [apps/desktop](../apps/desktop) y [apps/mobile](../apps/mobile): marcadores de posición para las apps de escritorio y móvil

Y estas carpetas compartidas:

* [libs/go-common](../libs/go-common): los paquetes Go que importan los demás componentes Go
* [libs/proto](../libs/proto): el contrato gRPC entre la Cluster API y el Cluster Agent
* [libs/graphql](../libs/graphql): el esquema de cada servidor GraphQL, en `libs/graphql/<server>/`
* [go.work](../go.work): el workspace de Go de `cli`, `dashboard`, `cluster/api` y `libs/go-common`
* [config/default](../config/default): el archivo de configuración predeterminado y documentado de cada componente
* [e2e](../e2e): pruebas end-to-end en Python, ejecutadas contra un clúster kind
* [tools](../tools): las herramientas Go que ejecuta CI, en un módulo propio
* [docs/adr](../docs/adr): Architecture Decision Records, que explican por qué el diseño es como es

[CONTRIBUTING.md](../CONTRIBUTING.md#quick-reference) indica en qué carpeta empezar.

### Configurar el entorno de desarrollo

#### Dependencias

* [Go](https://go.dev/) 1.26
* [Node.js](https://nodejs.org/) 26 y [pnpm](https://pnpm.io/)
* [Tilt](https://tilt.dev/)
* [Clúster compatible con Tilt](https://docs.tilt.dev/choosing_clusters.html) (por ejemplo, [minikube](https://minikube.sigs.k8s.io/docs/), [kind](https://kind.sigs.k8s.io/docs/user/quick-start/), [docker-desktop](https://docs.tilt.dev/choosing_clusters.html#docker-for-desktop))
* [ctlptl](https://github.com/tilt-dev/ctlptl) (opcional)

CI fija las versiones exactas de Go y Node.js en [.github/actions/setup-environment/action.yml](actions/setup-environment/action.yml). `make` necesita Node.js y pnpm además de Go, ya que compila la UI antes que la CLI.

#### Siguientes pasos

1. Crea un clúster de desarrollo de Kubernetes [compatible con Tilt](https://docs.tilt.dev/choosing_clusters.html):

```console
# minikube
ctlptl apply -f hack/ctlptl/minikube.yaml

# kind
ctlptl apply -f hack/ctlptl/kind.yaml

# docker-desktop
ctlptl apply -f hack/ctlptl/docker-desktop.yaml
```

2. Inicia el entorno de desarrollo:

```console
tilt up
```

3. Inicia el servidor del Dashboard:

```console
cd dashboard
go run cmd/main.go -c hack/config.yaml
```

El servidor escucha en el puerto 4000. Sirve la UI solo después de que `make build-dashboard-ui` haya copiado una compilación en `dashboard/website/`; hasta entonces, cada ruta de página responde 404 y accedes a la UI a través del servidor de desarrollo del siguiente paso.

4. Ejecuta la Dashboard UI localmente:

```console
cd apps/web
pnpm install
pnpm dev
```

Ahora puedes acceder al dashboard en [http://localhost:5173](http://localhost:5173). El servidor de desarrollo reenvía las peticiones de la API al servidor del Dashboard en el puerto 4000.

<details>
  <summary><h3>Desarrollo Rust (opcional)</h3></summary>
  
  De forma predeterminada, el entorno de desarrollo usa una imagen pre-construida para el cluster-agent basado en Rust. Si estás trabajando en el código Rust, puedes usar la variable de entorno `KUBETAIL_RUST_DEV_MODE` para activar un modo de desarrollo que compila el código Rust y sincroniza el binario en el clúster. Hay dos modos disponibles:

  | Modo     | Descripción                                                                                                          |
  | -------- | -------------------------------------------------------------------------------------------------------------------- |
  | `docker` | Compila el código Rust dentro de un contenedor Docker. No requiere toolchain Rust local.                             |
  | `local`  | Compila el código Rust en tu máquina usando compilación cruzada. Recompilaciones más rápidas pero requiere configuración local. |

  #### Modo Docker

  Para usar el modo Docker, ejecuta Tilt con la variable de entorno `KUBETAIL_RUST_DEV_MODE` establecida en `docker`:

  ```console
  KUBETAIL_RUST_DEV_MODE=docker tilt up
  ```

  #### Modo local

  El modo local requiere dependencias y configuración adicionales pero ofrece recompilaciones más rápidas.

  ##### Dependencias

  * [rustup](https://rustup.rs)
  * [protobuf](https://protobuf.dev/installation/)

  ##### Configuración

  Primero, instala el target de Rust necesario para tu arquitectura:

  ```console
  # x86_64
  rustup target add x86_64-unknown-linux-musl

  # aarch64
  rustup target add aarch64-unknown-linux-musl
  ```

  Después, instala las herramientas que necesita el compilador cruzado de Rust:

  ```console
  # macOS (Homebrew)
  brew install FiloSottile/musl-cross/musl-cross

  # Linux (Ubuntu)
  apt-get install musl-tools
  ```

  En macOS, añade esto a tu archivo `~/.cargo/config.toml`:

  ```
  [target.x86_64-unknown-linux-musl]
  linker = "x86_64-linux-musl-gcc"

  [target.aarch64-unknown-linux-musl]
  linker = "aarch64-linux-musl-gcc"
  ```

  Por último, ejecuta Tilt con la variable de entorno `KUBETAIL_RUST_DEV_MODE` establecida en `local`:

  ```console
  KUBETAIL_RUST_DEV_MODE=local tilt up
  ```
</details>

### Comprobaciones

[CONTRIBUTING.md](../CONTRIBUTING.md) describe cómo probar un cambio y abrir un pull request. El README de cada componente tiene una sección *Checks* (por ejemplo [dashboard/README.md](../dashboard/README.md#checks)): qué ejecutar mientras trabajas y qué ejecutar antes de abrir un pull request. La CI ejecuta todas las comprobaciones en todas las plataformas.

Un cambio en un lugar puede romper otro componente. Ejecuta también:

| Un cambio en | Ejecuta también |
|---|---|
| `libs/go-common/` | `make test-go` (todo el workspace) |
| `libs/graphql/*/schema.graphqls` | `go generate ./...` en ese servidor, `pnpm graphql-codegen` en `apps/web/`, `make vet-ts` |
| `libs/proto/` | `make check-generated`, las comprobaciones de Go y Rust |
| un `Dockerfile` o una versión de toolchain | `make lint-toolchains lint-digests` |
| `.github/` u otro YAML | `make lint-yaml` |
| `scripts/` | `make lint-sh test-sh` |
| `tools/` | `cd tools && gofmt -l . && go vet ./... && go test ./...` |
| Markdown | `make lint-md` |
| `config/`, `hack/` | La CI no ejecuta ningún job de código para estos: indica en el pull request las comprobaciones que ejecutaste |

`make help` lista todos los targets.

## Build

### Herramienta CLI

Para compilar el ejecutable de la herramienta CLI de Kubetail (`kubetail`), ejecuta el siguiente comando:

```console
make
```

Cuando termine el proceso de compilación, encontrarás el ejecutable en el directorio local `bin/`.

### Dashboard

Para compilar una imagen Docker para un despliegue en producción del servidor Kubetail Dashboard, ejecuta el siguiente comando:

```console
docker build -f dashboard/Dockerfile -t kubetail-dashboard:latest .
```

### Cluster API

Para compilar una imagen Docker para un despliegue en producción del servidor Kubetail Cluster API, ejecuta el siguiente comando:

```console
docker build -f cluster/api/Dockerfile -t kubetail-cluster-api:latest .
```

### Cluster Agent

Para compilar una imagen Docker para un despliegue en producción de Kubetail Cluster Agent, ejecuta el siguiente comando:

```console
docker build -f cluster/agent/Dockerfile -t kubetail-cluster-agent:latest .
```

## Participa

Estamos construyendo la plataforma de logging para Kubernetes más **fácil de usar**, **rentable** y **segura**, y nos encantaría contar con tus contribuciones. Así es como puedes ayudar:

* Diseño UI/UX
* Desarrollo frontend con React
* Reportar problemas y sugerir funcionalidades

Escríbenos a hello@kubetail.com o únete a nuestro [servidor de Discord](https://discord.gg/CmsmWAVkvX) o [canal de Slack](https://join.slack.com/t/kubetail/shared_invite/zt-2cq01cbm8-e1kbLT3EmcLPpHSeoFYm1w).
