load('ext://restart_process', 'docker_build_with_restart')
load('ext://namespace', 'namespace_create')
load('ext://secret', 'secret_create_generic', 'secret_create_tls')

namespace_create('kubetail-system')

# kubetail shared

secret_create_generic(
    name='kubetail-ca',
    namespace='kubetail-system',
    from_file=[
        'ca.crt=./hack/tilt/tls/ca.crt'
    ],
)

# kubetail-dashboard

secret_create_tls(
    name='kubetail-dashboard-tls',
    namespace='kubetail-system',
    cert='./hack/tilt/tls/dashboard.crt',
    key='./hack/tilt/tls/dashboard.key',
)

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

# kubetail-cluster-api

secret_create_tls(
    name='kubetail-cluster-api-tls',
    namespace='kubetail-system',
    cert='./hack/tilt/tls/cluster-api.crt',
    key='./hack/tilt/tls/cluster-api.key',
)

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

secret_create_tls(
    name='kubetail-cluster-agent-tls',
    namespace='kubetail-system',
    cert='./hack/tilt/tls/cluster-agent.crt',
    key='./hack/tilt/tls/cluster-agent.key',
)

build_rust_locally = os.getenv("KUBETAIL_DEV_RUST_LOCAL", default='false').lower() == 'true'
if build_rust_locally:
  local_resource(
    "kubetail-cluster-agent-compile",
    '''
    set -eu

    # --- Determine target architecture ---
    arch=$(uname -m)
    case "$arch" in
      x86_64|amd64) target_arch="x86_64" ;;
      arm64|aarch64) target_arch="aarch64" ;;
      *) echo "Unsupported arch: $arch"; exit 1 ;;
    esac
    target="${target_arch}-unknown-linux-musl"

    cd crates

    # --- Build static binary ---
    cargo build --target "${target}"

    # --- Copy to .tilt directory ---
    out_dir="../.tilt"
    mkdir -p "$out_dir"
    cp "target/${target}/debug/cluster_agent" "$out_dir/cluster-agent"
    ''',
    deps=[
      "./crates",
      "./proto",
    ],
    ignore=[
      './crates/target',
      './crates/*/target'
    ],
  )

  docker_build_with_restart(
    'kubetail-cluster-agent',
    dockerfile='hack/tilt/Dockerfile.kubetail-cluster-agent-local',
    context='.',
    entrypoint="/cluster-agent/cluster-agent -c /etc/kubetail/config.yaml",
    only=[
      './.tilt/cluster-agent',
    ],
    live_update=[
      sync('./.tilt/cluster-agent', '/cluster-agent/cluster-agent')
    ]
  )
else:
  docker_build_with_restart(
    'kubetail-cluster-agent',
    dockerfile='hack/tilt/Dockerfile.kubetail-cluster-agent',
    context='.',
    entrypoint="/cluster-agent/cluster-agent -c /etc/kubetail/config.yaml",
    only=[
      './crates',
      './proto',
    ],
    ignore=[
      './crates/target',
      './crates/*/target'
    ],
    live_update=[
      sync('./.tilt/cluster-agent', '/cluster-agent/cluster-agent'),
    ]
  )

# apply manifests
k8s_yaml('hack/tilt/kubetail.yaml')
k8s_yaml('hack/tilt/loggen.yaml')
k8s_yaml('hack/tilt/loggen-ansi.yaml')
k8s_yaml('hack/tilt/echoserver.yaml')
k8s_yaml('hack/tilt/cronjob.yaml')
k8s_yaml('hack/tilt/chaoskube.yaml')
k8s_yaml('hack/tilt/multi-containers-pod.yaml')
k8s_yaml('hack/tilt/daemonset.yaml')

# define resources
k8s_resource(
  objects=[
    'kubetail-system:namespace',
    'kubetail-ca:secret',
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
    'kubetail-dashboard:configmap',
    'kubetail-dashboard:clusterrole',
    'kubetail-dashboard:clusterrolebinding',
    'kubetail-dashboard:role',
    'kubetail-dashboard:rolebinding',
    'kubetail-dashboard:serviceaccount',
    'kubetail-dashboard-tls:secret',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-cluster-api',
  port_forwards='4501:8080',
  objects=[
    'kubetail-cluster-api:configmap',
    'kubetail-cluster-api:serviceaccount',
    'kubetail-cluster-api:clusterrole',
    'kubetail-cluster-api:clusterrolebinding',
    'kubetail-cluster-api:role',
    'kubetail-cluster-api:rolebinding',
    'kubetail-cluster-api-tls:secret',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-cluster-agent',
  objects=[
    'kubetail-cluster-agent:configmap',
    'kubetail-cluster-agent:serviceaccount',
    'kubetail-cluster-agent:networkpolicy',
    'kubetail-cluster-agent-tls:secret',
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


