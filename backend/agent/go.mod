module github.com/kubetail-org/kubetail/backend/agent

go 1.22.3

replace github.com/kubetail-org/kubetail/backend/common => ../common

require (
	github.com/fsnotify/fsnotify v1.7.0
	github.com/kubetail-org/kubetail/backend/common v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.64.0
	google.golang.org/protobuf v1.34.1
)

require (
	github.com/golang/protobuf v1.5.4 // indirect
	golang.org/x/net v0.22.0 // indirect
	golang.org/x/sys v0.21.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	google.golang.org/genproto v0.0.0-20200825200019-8632dd797987 // indirect
)
