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

package logmetadata

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestList(t *testing.T) {
	s, err := NewLogMetadataService("node-name")
	assert.Nil(t, err)

	grpcServer := grpc.NewServer()
	agentpb.RegisterLogMetadataServiceServer(grpcServer, s)

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()
	defer grpcServer.Stop()

	// init conn
	conn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
	)
	assert.Nil(t, err)
	defer conn.Close()

	// init client
	client := agentpb.NewLogMetadataServiceClient(conn)

	// make request
	req := &agentpb.LogMetadataListRequest{}
	resp, err := client.List(context.Background(), req)
	fmt.Println(err)
	fmt.Println(resp)
}

func TestWatch(t *testing.T) {
	assert.Equal(t, 1, 1)
}
