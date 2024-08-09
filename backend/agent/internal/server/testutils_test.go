// Copyright 2024 Andres Morey
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/common/testpb"
)

type TestService struct {
	testpb.UnimplementedTestServiceServer
}

func (s *TestService) Echo(ctx context.Context, req *testpb.EchoRequest) (*testpb.EchoResponse, error) {
	return &testpb.EchoResponse{Message: req.GetMessage()}, nil
}

// Test client
type TestClient struct {
	testpb.TestServiceClient
	grpcConn *grpc.ClientConn
}

// Close underlying grpc connection
func (tc *TestClient) Close() error {
	return tc.grpcConn.Close()
}

// Test Server
type TestServer struct {
	*grpc.Server
	lis *bufconn.Listener
}

// Initialize new TestClient instance
func (ts *TestServer) NewTestClient(opts ...grpc.DialOption) (*TestClient, error) {
	// init conn
	dialerFunc := func(ctx context.Context, _ string) (net.Conn, error) {
		return ts.lis.DialContext(ctx)
	}

	opts = append(
		opts,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialerFunc),
	)

	grpcConn, err := grpc.NewClient("passthrough://bufnet", opts...)
	if err != nil {
		return nil, err
	}

	// init client
	client := testpb.NewTestServiceClient(grpcConn)

	// return test client
	return &TestClient{TestServiceClient: client, grpcConn: grpcConn}, nil
}

// Initialize new TestServer instance
func NewTestServer(cfg *config.Config) *TestServer {
	// init service
	svc := &TestService{}

	// init server
	server := NewServer(cfg)
	testpb.RegisterTestServiceServer(server, svc)

	// init listener
	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := server.Serve(lis); err != nil {
			panic(err)
		}
	}()

	return &TestServer{Server: server, lis: lis}
}
