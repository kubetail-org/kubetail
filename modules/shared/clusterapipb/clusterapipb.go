package clusterapipb

//go:generate protoc --proto_path=../../../proto --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ../../../proto/cluster_api.proto
