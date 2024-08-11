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

package logmetadata2

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/kubetail-org/kubetail/backend/common/agentpb"
	"github.com/kubetail-org/kubetail/backend/common/config"
)

type LogMetadataTestSuite struct {
	suite.Suite
	podLogsDir       string
	containerLogsDir string
	testServer       *TestServer
}

// Setup suite
func (suite *LogMetadataTestSuite) SetupSuite() {
	// disable logging
	config.ConfigureLogger(config.LoggerOptions{Enabled: false})

	// temporary directory for pod logs
	podLogsDir, err := os.MkdirTemp("", "logmetadata-podlogsdir-")
	suite.Require().Nil(err)

	// temporary directory for container log links
	containerLogsDir, err := os.MkdirTemp("", "logmetadata-containerlogsdir-")
	suite.Require().Nil(err)

	// test config
	cfg := config.DefaultConfig()
	cfg.AuthMode = config.AuthModeCluster
	cfg.Agent.ContainerLogsDir = containerLogsDir

	// init test server
	testServer, err := NewTestServer(cfg)
	suite.Require().Nil(err)

	// save references
	suite.podLogsDir = podLogsDir
	suite.containerLogsDir = containerLogsDir
	suite.testServer = testServer
}

// Teardown suite
func (suite *LogMetadataTestSuite) TearDownSuite() {
	defer os.RemoveAll(suite.containerLogsDir)
	defer os.RemoveAll(suite.podLogsDir)
	suite.testServer.Stop()
}

// Setup test
func (suite *LogMetadataTestSuite) SetupTest() {
	// clear log file dirs
	for _, dirpath := range []string{suite.containerLogsDir, suite.podLogsDir} {
		// list files
		files, err := os.ReadDir(dirpath)
		suite.Require().Nil(err)

		// delete each one
		for _, file := range files {
			filePath := filepath.Join(dirpath, file.Name())
			err = os.Remove(filePath)
			suite.Require().Nil(err)
		}
	}

	// reset clientset
	suite.testServer.ResetClientset()
}

// Helper method to create a container log file
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

func (suite *LogMetadataTestSuite) TestList() {
	// add file to namespace ns1
	f0 := suite.createContainerLogFile("ns1", "pn1", "cn", "000")
	f0.Close()

	// add file to namespace ns1
	f1 := suite.createContainerLogFile("ns1", "pn2", "cn", "111")
	f1.Write([]byte("123"))
	f1.Close()

	// add file to namespace ns2
	f2 := suite.createContainerLogFile("ns2", "pn", "cn", "222")
	f2.Write([]byte("123456"))
	f2.Close()

	// add file to namespace ns2
	f3 := suite.createContainerLogFile("ns3", "pn", "cn", "333")
	f3.Write([]byte("123456789"))
	f3.Close()

	suite.Run("single namespace", func() {
		// allow access
		suite.testServer.AllowSSAR([]string{"ns1"}, []string{"list"})

		client := suite.testServer.NewTestClient()
		resp, err := client.List(context.Background(), &agentpb.LogMetadataListRequest{Namespaces: []string{"ns1"}})
		suite.Require().Nil(err)

		// check number of items
		suite.Require().Equal(2, len(resp.Items))

		// check item0
		item0 := resp.Items[0]
		suite.Equal("000", item0.Id)
		suite.Equal("ns1", item0.Spec.Namespace)
		suite.Equal("pn1", item0.Spec.PodName)
		suite.Equal("cn", item0.Spec.ContainerName)
		suite.Equal("000", item0.Spec.ContainerId)

		// check item1
		item1 := resp.Items[1]
		suite.Equal("111", item1.Id)
		suite.Equal("ns1", item1.Spec.Namespace)
		suite.Equal("pn2", item1.Spec.PodName)
		suite.Equal("cn", item1.Spec.ContainerName)
		suite.Equal("111", item1.Spec.ContainerId)
	})

	suite.Run("multiple namespaces", func() {
		// allow access
		suite.testServer.AllowSSAR([]string{"ns1", "ns2"}, []string{"list"})

		client := suite.testServer.NewTestClient()
		resp, err := client.List(context.Background(), &agentpb.LogMetadataListRequest{Namespaces: []string{"ns1", "ns2"}})
		suite.Require().Nil(err)

		// check number of items
		suite.Require().Equal(3, len(resp.Items))

		// check item2
		item2 := resp.Items[2]
		suite.Equal("222", item2.Id)
		suite.Equal("ns2", item2.Spec.Namespace)
		suite.Equal("pn", item2.Spec.PodName)
		suite.Equal("cn", item2.Spec.ContainerName)
		suite.Equal("222", item2.Spec.ContainerId)
	})

	suite.Run("all namespaces", func() {
		// allow access
		suite.testServer.AllowSSAR([]string{""}, []string{"list"})

		client := suite.testServer.NewTestClient()
		resp, err := client.List(context.Background(), &agentpb.LogMetadataListRequest{Namespaces: []string{""}})
		suite.Require().Nil(err)

		// check number of items
		suite.Require().Equal(4, len(resp.Items))

		// check item3
		item3 := resp.Items[3]
		suite.Equal("333", item3.Id)
		suite.Equal("ns3", item3.Spec.Namespace)
		suite.Equal("pn", item3.Spec.PodName)
		suite.Equal("cn", item3.Spec.ContainerName)
		suite.Equal("333", item3.Spec.ContainerId)
	})
}

// test runner
func TestLogMetadata(t *testing.T) {
	suite.Run(t, new(LogMetadataTestSuite))
}
