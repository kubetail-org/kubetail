load('ext://restart_process', 'docker_build_with_restart')

# kubetail-agent
local_resource(
  'kubetail-agent-compile',
  'cd modules && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/agent ./agent/cmd/main.go',
  deps=[
    './modules/agent',
    './modules/common'
  ]
)

docker_build_with_restart(
  'kubetail-agent',
  dockerfile='hack/tilt/Dockerfile.kubetail-agent',
  context='.',
  entrypoint="/agent/agent -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/agent',
  ],
  live_update=[
    sync('./.tilt/agent', '/agent/agent'),
  ]
)

# kubetail-server
local_resource(
  'kubetail-server-compile',
  'cd modules && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/server ./server/cmd/main.go',
  deps=[
    './modules/server',
    './modules/common'
  ]
)

docker_build_with_restart(
  'kubetail-server',
  dockerfile='hack/tilt/Dockerfile.kubetail-server',
  context='.',
  entrypoint="/server/server -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/server',
  ],
  live_update=[
    sync('./.tilt/server', '/server/server'),
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
  'kubetail-server',
  port_forwards='7500:4000',
  objects=[
    'kubetail-server:clusterrole',
    'kubetail-server:clusterrolebinding',
    'kubetail-server:role',
    'kubetail-server:rolebinding',
    'kubetail-server:serviceaccount',
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-agent',
  objects=[
    'kubetail-agent:serviceaccount',
    'kubetail-agent:clusterrole',
    'kubetail-agent:clusterrolebinding',
    'kubetail-agent:networkpolicy',
  ],
  resource_deps=['kubetail-shared'],
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
  port_forwards='8080:8080',
)
