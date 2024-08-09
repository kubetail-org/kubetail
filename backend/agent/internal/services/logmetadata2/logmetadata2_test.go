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
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

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
	suite.Equal(1, 1)
}

// test runner
func TestLogMetadata(t *testing.T) {
	suite.Run(t, new(LogMetadataTestSuite))
}
