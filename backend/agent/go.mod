module github.com/kubetail-org/kubetail/backend/agent

go 1.22

replace github.com/kubetail-org/kubetail/backend/common => ../common

require (
	github.com/fsnotify/fsnotify v1.7.0
	github.com/kubetail-org/kubetail/backend/common v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.34.1
)

require (
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240318140521-94a12d6c2237 // indirect
)
