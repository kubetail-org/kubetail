#!/bin/sh
# Fails if the sidecar binary links client-go's typed clients or any k8s.io/api package.
# kubernetes/scheme registers every API group, so one import of discovery or the typed
# clientset adds about 16 MB. Test imports are not linked, so tests may use them.
set -eu

cd "$(dirname "$0")/../sidecar"

pattern='^k8s\.io/api/|^k8s\.io/client-go/(discovery|kubernetes)(/|$)'
status=0
for goos in darwin linux windows; do
	found=$(GOOS=$goos go list -deps . | grep -E "$pattern" || true)
	if [ -n "$found" ]; then
		echo "sidecar ($goos) links packages it must not:"
		echo "$found" | sed 's/^/  /'
		status=1
	fi
done
if [ "$status" -ne 0 ]; then
	echo "Read the API with Dynamic or raw requests on kubeconn.Connection instead."
fi
exit $status
