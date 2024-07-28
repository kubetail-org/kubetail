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
	"net"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
)

type LogMetadataTestSuite struct {
	suite.Suite
	grpcServer *grpc.Server
	grpcConn   *grpc.ClientConn
	logsDir    string
}

func (suite *LogMetadataTestSuite) SetupSuite() {
	// Create a temporary directory in the default location for temporary files.
	logsDir, err := os.MkdirTemp("", "logmetadata")
	suite.Require().Nil(err)

	// init service
	s, err := NewLogMetadataService("node-name", logsDir)
	suite.Require().Nil(err)

	// init server
	grpcServer := grpc.NewServer()
	agentpb.RegisterLogMetadataServiceServer(grpcServer, s)

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := grpcServer.Serve(lis); err != nil {
			panic(err)
		}
	}()

	// init conn
	dialerFunc := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}
	grpcConn, err := grpc.NewClient(
		"passthrough://bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialerFunc),
	)
	suite.Require().Nil(err)

	// save references
	suite.logsDir = logsDir
	suite.grpcServer = grpcServer
	suite.grpcConn = grpcConn
}

func (suite *LogMetadataTestSuite) TearDownSuite() {
	defer os.RemoveAll(suite.logsDir)
	suite.grpcConn.Close()
	suite.grpcServer.Stop()
}

func (suite *LogMetadataTestSuite) TestList() {
	// create temp files
	f1, err := os.Create(path.Join(suite.logsDir, "podname_default_containername-id123.log"))
	suite.Require().Nil(err)
	f1.Write([]byte("12345"))

	// init client
	client := agentpb.NewLogMetadataServiceClient(suite.grpcConn)

	// make request
	req := &agentpb.LogMetadataListRequest{
		Namespaces: []string{"default"},
	}
	resp, err := client.List(context.Background(), req)
	suite.Require().Nil(err)
	suite.Equal(1, len(resp.Items))

	item := resp.Items[0]
	suite.Equal("id123", item.Id)
	suite.Equal("node-name", item.Spec.NodeName)
	suite.Equal("default", item.Spec.Namespace)
	suite.Equal("podname", item.Spec.PodName)
	suite.Equal("containername", item.Spec.ContainerName)
	suite.Equal("id123", item.Spec.ContainerId)
	suite.Equal(int64(5), item.FileInfo.Size)
}

func (suite *LogMetadataTestSuite) TestWatch() {

}

// test runner
func TestLogMetadata(t *testing.T) {
	suite.Run(t, new(LogMetadataTestSuite))
}
