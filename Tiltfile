load('ext://restart_process', 'docker_build_with_restart')

# kubetail-agent
local_resource(
  'kubetail-agent-compile',
  'cd backend && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/agent ./agent/cmd/main.go',
  deps=[
    './backend/agent',
    './backend/common/agentpb'
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
  'cd backend && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/server ./server/cmd/main.go',
  deps=[
    './backend/server',
    './backend/common/agentpb'
  ]
)

docker_build_with_restart(
  'kubetail-server',
  dockerfile='hack/tilt/Dockerfile.kubetail-server',
  context='.',
  entrypoint="/server/server -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/server',
    './backend/server/templates'
  ],
  live_update=[
    sync('./.tilt/server', '/server/server'),
    sync('./backend/server/templates', '/server/templates')
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
    'kubetail:configmap'
  ],
  new_name="kubetail-shared"
)

k8s_resource(
  'kubetail-server',
  port_forwards='4000:4000',
  objects=[
    'kubetail-server:serviceaccount',
    'kubetail-server:clusterrole',
    'kubetail-server:clusterrolebinding'
  ],
  resource_deps=['kubetail-shared'],
)

k8s_resource(
  'kubetail-agent',
  objects=[
    'kubetail-agent:serviceaccount',
    'kubetail-agent:clusterrole',
    'kubetail-agent:clusterrolebinding',
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
