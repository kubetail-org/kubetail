load('ext://restart_process', 'docker_build_with_restart')
load('ext://namespace', 'namespace_create')

namespace_create('kubetail-system')

# kubetail-cluster-api

local_resource(
  'kubetail-cluster-api-compile',
  '''
  cd modules

  # --- Build Go binary ---
  export CGO_ENABLED=0
  export GOOS=linux
  go build -o ../.tilt/cluster-api ./cluster-api/cmd/main.go
  ''',
  deps=[
    './modules/cluster-api',
    './modules/shared'
  ]
)

docker_build_with_restart(
  'kubetail-cluster-api',
  dockerfile='hack/tilt/Dockerfile.kubetail-cluster-api',
  context='.',
  entrypoint="/cluster-api/cluster-api -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/cluster-api',
  ],
  live_update=[
    sync('./.tilt/cluster-api', '/cluster-api/cluster-api'),
  ]
)

# kubetail-cluster-agent

local_resource(
  'kubetail-cluster-agent-compile',
  '''
  cd modules

  # --- Build Go binary   ---
  export CGO_ENABLED=0
  export GOOS=linux
  go build -o ../.tilt/cluster-agent ./cluster-agent/cmd/main.go
  ''',
  deps=[
    './modules/cluster-agent',
    './modules/shared'
  ]
)

build_rust_locally = os.getenv("KUBETAIL_DEV_RUST_LOCAL", default='false').lower() == 'true'
if build_rust_locally:
  local_resource(
    "kubetail-rgkl-compile",
    '''
    set -eu

    cd crates/rgkl

    # --- Determine target architecture ---
    arch=$(uname -m)
    case "$arch" in
      x86_64|amd64) target_arch="x86_64" ;;
      arm64|aarch64) target_arch="aarch64" ;;
      *) echo "Unsupported arch: $arch"; exit 1 ;;
    esac
    target="${target_arch}-unknown-linux-musl"

    # --- Build static binary ---
    cargo build --target "${target}"

    # --- Copy to .tilt directory ---
    out_dir="../../.tilt"
    mkdir -p "$out_dir"
    cp "target/${target}/debug/rgkl" "$out_dir"
    ''',
    deps=[
      "./crates/rgkl/src",
      "./crates/rgkl/Cargo.toml",
      "./crates/rgkl/Cargo.lock",
      "./proto",
    ],
  )

  docker_build_with_restart(
    'kubetail-cluster-agent',
    dockerfile='hack/tilt/Dockerfile.kubetail-cluster-agent-local',
    context='.',
    entrypoint="/cluster-agent/cluster-agent -c /etc/kubetail/config.yaml",
    only=[
      './.tilt/cluster-agent',
      './.tilt/rgkl',
    ],
    live_update=[
      sync('./.tilt/cluster-agent', '/cluster-agent/cluster-agent'),
      sync('./.tilt/rgkl', '/cluster-agent/rgkl')
    ]
  )
else:
  docker_build_with_restart(
    'kubetail-cluster-agent',
    dockerfile='hack/tilt/Dockerfile.kubetail-cluster-agent',
    context='.',
    entrypoint="/cluster-agent/cluster-agent -c /etc/kubetail/config.yaml",
    only=[
      './crates/rgkl',
      './proto',
      './.tilt/cluster-agent'
    ],
    ignore=[
      './crates/rgkl/target'
    ],
    live_update=[
      sync('./.tilt/cluster-agent', '/cluster-agent/cluster-agent'),
    ]
  )

# kubetail-dashboard

local_resource(
  'kubetail-dashboard-compile',
  '''
  # Check if the dashboard-ui/dist directory exists
  if [ -d dashboard-ui/dist ]; then
    rm -rf modules/dashboard/website &&
    cp -r dashboard-ui/dist modules/dashboard/website
  fi

  cd modules

  # --- Build the Go binary ---
  export CGO_ENABLED=0
  export GOOS=linux
  go build -o ../.tilt/dashboard ./dashboard/cmd/main.go

  # --- Reset dashboard/website directory ---
  if [ ! -f dashboard/website/.gitkeep ]; then
    rm -rf dashboard/website &&
    git checkout dashboard/website
  fi
  ''',
  deps=[
    './dashboard-ui/dist',
    './modules/dashboard',
    './modules/shared'
  ],
  ignore=[
    './modules/dashboard/website'
  ]
)

docker_build_with_restart(
  'kubetail-dashboard',
  dockerfile='hack/tilt/Dockerfile.kubetail-dashboard',
  context='.',
  entrypoint="/dashboard/dashboard -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/dashboard',
  ],
  live_update=[
    sync('./.tilt/dashboard', '/dashboard/dashboard'),
  ]
)

# apply manifests
k8s_yaml('hack/tilt/kubetail.yaml')
k8s_yaml('hack/tilt/loggen.yaml')
k8s_yaml('hack/tilt/loggen-ansi.yaml')
k8s_yaml('hack/tilt/echoserver.yaml')
k8s_yaml('hack/tilt/cronjob.yaml')
k8s_yaml('hack/tilt/chaoskube.yaml')
k8s_yaml('hack/tilt/clickstack.yaml')

# define resources
k8s_resource(
  objects=[
    'kubetail-system:namespace',
    'clickstack:namespace',
    'kubetail:configmap',
    'kubetail-testuser:serviceaccount',
    'kubetail-testuser:role',
    'kubetail-testuser:clusterrole',
    'kubetail-testuser:rolebinding',
    'kubetail-testuser:clusterrolebinding',
  ],
  new_name='kubetail-shared',
)

k8s_resource(
  'kubetail-dashboard',
  port_forwards='4500:8080',
  objects=[
    'kubetail-dashboard:clusterrole',
    'kubetail-dashboard:clusterrolebinding',
    'kubetail-dashboard:role',
    'kubetail-dashboard:rolebinding',
    'kubetail-dashboard:serviceaccount',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-cluster-api',
  port_forwards='4501:8080',
  objects=[
    'kubetail-cluster-api:serviceaccount',
    'kubetail-cluster-api:clusterrole',
    'kubetail-cluster-api:clusterrolebinding',
    'kubetail-cluster-api:role',
    'kubetail-cluster-api:rolebinding',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-cluster-agent',
  objects=[
    'kubetail-cluster-agent:serviceaccount',
    'kubetail-cluster-agent:networkpolicy',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  objects=[
    'kubetail-cli:serviceaccount',
    'kubetail-cli:clusterrole',
    'kubetail-cli:clusterrolebinding',
  ],
  new_name='kubetail-cli',
)

k8s_resource(
  'chaoskube',
  objects=[
    'chaoskube:serviceaccount',
    'chaoskube:clusterrole',
    'chaoskube:clusterrolebinding',
    'chaoskube:role',
    'chaoskube:rolebinding'
  ]
)

k8s_resource(
  'echoserver',
  port_forwards='4502:8080',
)

k8s_resource(
  'clickstack',
  port_forwards=['4503:8080'],
  resource_deps=['kubetail-shared'],
)
