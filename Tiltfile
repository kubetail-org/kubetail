load('ext://restart_process', 'docker_build_with_restart')

# kubetail-server
local_resource(
  'kubetail-server-compile',
  'cd backend && CGO_ENABLED=0 GOOS=linux go build -o ../.tilt/server ./cmd/server',
  deps=[
    './backend'
  ]
)

docker_build_with_restart(
  'kubetail-server',
  dockerfile='hack/tilt/Dockerfile.kubetail-server',
  context='.',
  entrypoint="/server/server -c /etc/kubetail/config.yaml",
  only=[
    './.tilt/server',
    './backend/templates'
  ],
  live_update=[
    sync('./.tilt/server', '/server/server'),
    sync('./backend/templates', '/templates')
  ]
)

# --- apply manifests ---

k8s_yaml('hack/tilt/kubetail-server.yaml')
k8s_yaml('hack/tilt/loggen.yaml')
k8s_yaml('hack/tilt/loggen-ansi.yaml')
k8s_yaml('hack/tilt/echoserver.yaml')
k8s_yaml('hack/tilt/cronjob.yaml')
#k8s_yaml('hack/tilt/chaoskube.yaml')

# --- define resources ---

k8s_resource(
  'kubetail-server',
  port_forwards='4000:4000',
  objects=[
    'kubetail-server:serviceaccount',
    'kubetail-server:clusterrole',
    'kubetail-server:clusterrolebinding',
    'kubetail-server:configmap'
  ],
  resource_deps=[]
)

#k8s_resource(
#  'chaoskube',
#  objects=[
#    'chaoskube:serviceaccount',
#    'chaoskube:clusterrole',
#    'chaoskube:clusterrolebinding',
#    'chaoskube:role',
#    'chaoskube:rolebinding'
#  ]
#)

k8s_resource(
  'echoserver',
  port_forwards='8080:8080',
)
