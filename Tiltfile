load('ext://restart_process', 'docker_build_with_restart')
load('ext://namespace', 'namespace_create')

namespace_create('kubetail-system')

# kubetail-cluster-api

local_resource(
  'kubetail-cluster-api-compile',
  '''
  cd modules

  # Build Go binary  
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

  # Build Go binary  
  export CGO_ENABLED=0
  export GOOS=linux
  go build -o ../.tilt/cluster-agent ./cluster-agent/cmd/main.go
  ''',
  deps=[
    './modules/cluster-agent',
    './modules/shared'
  ]
)

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

  # Build the Go binary
  export CGO_ENABLED=0
  export GOOS=linux
  go build -o ../.tilt/dashboard ./dashboard/cmd/main.go

  # Reset dashboard/website directory
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

# define resources
k8s_resource(
  objects=[
    'kubetail-system:namespace',
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
