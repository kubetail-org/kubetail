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
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/kubetail-org/kubetail/backend/common/config"
)

type LogMetadataTestSuite struct {
	suite.Suite
	grpcServer       *grpc.Server
	grpcConn         *grpc.ClientConn
	podLogsDir       string
	containerLogsDir string
}

func (suite *LogMetadataTestSuite) SetupSuite() {
	// disable logging
	config.ConfigureLogger(config.LoggerOptions{Enabled: false})

	// temporary directory for pod logs
	podLogsDir, err := os.MkdirTemp("", "logmetadata-podlogsdir-")
	suite.Require().Nil(err)

	// temporary directory for container log links
	containerLogsDir, err := os.MkdirTemp("", "logmetadata-containerlogsdir-")
	suite.Require().Nil(err)

	// init test server

	s, err := NewLogMetadataService("node-name", containerLogsDir)
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
	suite.podLogsDir = podLogsDir
	suite.containerLogsDir = containerLogsDir
	suite.grpcServer = grpcServer
	suite.grpcConn = grpcConn
}

func (suite *LogMetadataTestSuite) TearDownSuite() {
	defer os.RemoveAll(suite.containerLogsDir)
	defer os.RemoveAll(suite.podLogsDir)
	suite.grpcConn.Close()
	suite.grpcServer.Stop()
}

func (suite *LogMetadataTestSuite) SetupTest() {
	suite.clearDir(suite.containerLogsDir)
	suite.clearDir(suite.podLogsDir)
}

func (suite *LogMetadataTestSuite) clearDir(dirPath string) {
	// list files
	files, err := os.ReadDir(dirPath)
	suite.Require().Nil(err)

	// delete each one
	for _, file := range files {
		filePath := filepath.Join(dirPath, file.Name())
		err = os.Remove(filePath)
		suite.Require().Nil(err)
	}
}

func (suite *LogMetadataTestSuite) createContainerLogFile(namespace string, podName string, containerName string, containerID string) *os.File {
	// create pod log file
	f, err := os.CreateTemp(suite.podLogsDir, "*.log")
	suite.Require().Nil(err)

	// add soft link to container logs dir
	target := f.Name()
	link := path.Join(suite.containerLogsDir, fmt.Sprintf("%s_%s_%s-%s.log", podName, namespace, containerName, containerID))
	err = os.Symlink(target, link)
	suite.Require().Nil(err)

	return f
}

/*
func (suite *LogMetadataTestSuite) TestList() {
	// create files
	f1 := suite.createContainerLogFile("ns1", "pn1", "cn", "123")
	f1.Write([]byte("12345"))

	f2 := suite.createContainerLogFile("ns2", "pn2", "cn", "abc")
	defer f2.Close()
	f2.Write([]byte("abc"))

	// init client
	client := agentpb.NewLogMetadataServiceClient(suite.grpcConn)

	// make request 1
	req1 := &agentpb.LogMetadataListRequest{
		Namespaces: []string{"ns1"},
	}
	resp1, err := client.List(context.Background(), req1)
	suite.Require().Nil(err)
	suite.Equal(1, len(resp1.Items))

	item1 := resp1.Items[0]
	suite.Equal("123", item1.Id)
	suite.Equal("node-name", item1.Spec.NodeName)
	suite.Equal("ns1", item1.Spec.Namespace)
	suite.Equal("pn1", item1.Spec.PodName)
	suite.Equal("cn", item1.Spec.ContainerName)
	suite.Equal("123", item1.Spec.ContainerId)
	suite.Equal(int64(5), item1.FileInfo.Size)

	// make request 2
	req2 := &agentpb.LogMetadataListRequest{
		Namespaces: []string{"ns2"},
	}
	resp2, err := client.List(context.Background(), req2)
	suite.Require().Nil(err)
	suite.Equal(1, len(resp1.Items))

	item2 := resp2.Items[0]
	suite.Equal("abc", item2.Id)
	suite.Equal("node-name", item2.Spec.NodeName)
	suite.Equal("ns2", item2.Spec.Namespace)
	suite.Equal("pn2", item2.Spec.PodName)
	suite.Equal("cn", item2.Spec.ContainerName)
	suite.Equal("abc", item2.Spec.ContainerId)
	suite.Equal(int64(3), item2.FileInfo.Size)
}
*/

func (suite *LogMetadataTestSuite) TestWatchAdded() {
	// create file after watch starts
	testEventBus.SubscribeOnceAsync("watch:started", func() {
		// create file
		f := suite.createContainerLogFile("ns1", "pn1", "cn", "123")
		defer f.Close()
	})

	// init client
	client := agentpb.NewLogMetadataServiceClient(suite.grpcConn)

	// init request
	req := &agentpb.LogMetadataWatchRequest{
		Namespaces: []string{"ns1"},
	}

	// start watch
	stream, err := client.Watch(context.Background(), req)
	suite.Require().Nil(err)

	//time.Sleep(2 * time.Second)
	res, err := stream.Recv()
	suite.Require().Nil(err)

	suite.Equal("ADDED", res.Type)
	suite.Equal("123", res.Object.Id)
	suite.Equal("node-name", res.Object.Spec.NodeName)
	suite.Equal("ns1", res.Object.Spec.Namespace)
	suite.Equal("pn1", res.Object.Spec.PodName)
	suite.Equal("cn", res.Object.Spec.ContainerName)
	suite.Equal("123", res.Object.Spec.ContainerId)
	fmt.Println("XXXX")
}

/*
func (suite *LogMetadataTestSuite) TestWatchModified() {
	// create file
	f := suite.createContainerLogFile("ns1", "pn1", "cn", "123")
	defer f.Close()

	// modify file after watch starts
	testEventBus.SubscribeOnceAsync("watch:started", func() {
		f.Write([]byte("123"))
	})

	// init client
	client := agentpb.NewLogMetadataServiceClient(suite.grpcConn)

	// init request
	req := &agentpb.LogMetadataWatchRequest{
		Namespaces: []string{"ns1"},
	}

	// start watch
	stream, err := client.Watch(context.Background(), req)
	suite.Require().Nil(err)

	res, err := stream.Recv()
	suite.Require().Nil(err)

	suite.Equal("MODIFIED", res.Type)
	suite.Equal("123", res.Object.Id)
	suite.Equal("node-name", res.Object.Spec.NodeName)
	suite.Equal("ns1", res.Object.Spec.Namespace)
	suite.Equal("pn1", res.Object.Spec.PodName)
	suite.Equal("cn", res.Object.Spec.ContainerName)
	suite.Equal("123", res.Object.Spec.ContainerId)
}
*/

func (suite *LogMetadataTestSuite) TestWatchDeleted() {

}

// test runner
func TestLogMetadata(t *testing.T) {
	suite.Run(t, new(LogMetadataTestSuite))
}
