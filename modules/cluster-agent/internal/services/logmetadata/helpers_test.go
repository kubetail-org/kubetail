// Copyright 2024-2025 Andres Morey
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
	"os"
	"path"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestLogfileRegex(t *testing.T) {
	tests := []struct {
		name        string
		setInput    string
		wantMatches []string
	}{
		{
			"no hyphens",
			"pn_ns_cn-123.log",
			[]string{"pn", "ns", "cn", "123"},
		},
		{
			"pod name with hyphen",
			"pn-123_ns_cn-123.log",
			[]string{"pn-123", "ns", "cn", "123"},
		},
		{
			"namespace with hyphen",
			"pn_ns-123_cn-123.log",
			[]string{"pn", "ns-123", "cn", "123"},
		},
		{
			"container name with hyphen",
			"pn_ns_cn-123-123.log",
			[]string{"pn", "ns", "cn-123", "123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := logfileRegex.FindStringSubmatch(tt.setInput)

			// check number of matches
			require.Equal(t, len(tt.wantMatches)+1, len(matches))

			// check matched values
			for i := 0; i < len(tt.wantMatches); i++ {
				require.Equal(t, tt.wantMatches[i], matches[i+1])
			}
		})
	}
}

type ContainerLogsWatcherTestSuite struct {
	suite.Suite
	containerLogsDir string
	podLogsDir       string
}

func (suite *ContainerLogsWatcherTestSuite) SetupTest() {
	// temporary directory for pod logs
	podLogsDir, err := os.MkdirTemp("", "podlogsdir-")
	suite.Require().Nil(err)

	// temporary directory for container log links
	containerLogsDir, err := os.MkdirTemp("", "containerlogsdir-")
	suite.Require().Nil(err)

	// save references
	suite.podLogsDir = podLogsDir
	suite.containerLogsDir = containerLogsDir
}

func (suite *ContainerLogsWatcherTestSuite) TearDownTest() {
	os.RemoveAll(suite.containerLogsDir)
	os.RemoveAll(suite.podLogsDir)
}

// Helper method to create a container log file
func (suite *ContainerLogsWatcherTestSuite) createContainerLogFile(namespace string, podName string, containerName string, containerID string) *os.File {
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

// Helper method to delete a container log file
func (suite *ContainerLogsWatcherTestSuite) removeContainerLogFile(namespace string, podName string, containerName string, containerID string) error {
	pathname := path.Join(suite.containerLogsDir, fmt.Sprintf("%s_%s_%s-%s.log", podName, namespace, containerName, containerID))

	// get target
	target, err := os.Readlink(pathname)
	if err != nil {
		return err
	}

	// delete files
	if err := os.Remove(target); err != nil {
		return err
	}
	if err := os.Remove(pathname); err != nil {
		return err
	}

	return nil
}

func (suite *ContainerLogsWatcherTestSuite) TestClose() {
	// init watcher
	watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, []string{""})
	suite.Require().Nil(err)

	// check that events passes through close event
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := <-watcher.Events
		suite.Require().False(ok)
	}()

	// execute close
	err = watcher.Close()
	suite.Require().Nil(err)

	// wait
	wg.Wait()
}

func (suite *ContainerLogsWatcherTestSuite) TestCreate() {
	tests := []struct {
		name          string
		setNamespaces []string
	}{
		{
			"single namespace",
			[]string{"ns1"},
		},
		{
			"multiple namespaces",
			[]string{"ns1", "ns2"},
		},
		{
			"all namespaces",
			[]string{""},
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			suite.TearDownTest()
			suite.SetupTest()

			// init watcher
			watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, tt.setNamespaces)
			suite.Require().Nil(err)
			defer watcher.Close()

			var wg sync.WaitGroup

			// check that file modification event gets handled
			wg.Add(1)
			go func() {
				defer wg.Done()
				ev, ok := <-watcher.Events
				suite.Require().True(ok)
				suite.True(ev.Op.Has(fsnotify.Create))
			}()

			// create file
			f := suite.createContainerLogFile("ns1", "pn", "cn", "123")
			defer f.Close()

			// wait
			wg.Wait()
		})
	}
}

func (suite *ContainerLogsWatcherTestSuite) TestCreateOutsideNamespace() {
	// init watcher
	watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, []string{"ns1"})
	suite.Require().Nil(err)

	var wg sync.WaitGroup

	// check that file modification event gets handled
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := <-watcher.Events
		suite.Require().False(ok)
	}()

	// create file
	f := suite.createContainerLogFile("ns2", "pn", "cn", "123")
	defer f.Close()

	// close watcher
	watcher.Close()

	// wait
	wg.Wait()
}

func (suite *ContainerLogsWatcherTestSuite) TestModify() {
	tests := []struct {
		name          string
		setNamespaces []string
	}{
		{
			"single namespace",
			[]string{"ns1"},
		},
		{
			"multiple namespaces",
			[]string{"ns1", "ns2"},
		},
		{
			"all namespaces",
			[]string{""},
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			suite.TearDownTest()
			suite.SetupTest()

			// create file
			f := suite.createContainerLogFile("ns1", "pn", "cn", "123")
			f.Write([]byte("123"))
			defer f.Close()

			// init watcher
			watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, tt.setNamespaces)
			suite.Require().Nil(err)
			defer watcher.Close()

			var wg sync.WaitGroup

			// check that file modification event gets handled
			wg.Add(1)
			go func() {
				defer wg.Done()
				ev, ok := <-watcher.Events
				suite.Require().True(ok)
				suite.True(ev.Op.Has(fsnotify.Write))
			}()

			// modify file
			_, err = f.Write([]byte("456"))
			suite.Require().Nil(err)

			// wait
			wg.Wait()
		})
	}
}

func (suite *ContainerLogsWatcherTestSuite) TestModifyOutsideNamespace() {
	// create file
	f := suite.createContainerLogFile("ns1", "pn", "cn", "123")
	f.Write([]byte("123"))
	defer f.Close()

	// init watcher
	watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, []string{"ns2"})
	suite.Require().Nil(err)

	var wg sync.WaitGroup

	// check that file modification event gets ignored
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := <-watcher.Events
		suite.Require().False(ok)
	}()

	// modify file in ns1
	_, err = f.Write([]byte("456"))
	suite.Require().Nil(err)

	// close
	watcher.Close()

	// wait
	wg.Wait()
}

func (suite *ContainerLogsWatcherTestSuite) TestDelete() {
	tests := []struct {
		name          string
		setNamespaces []string
	}{
		{
			"single namespace",
			[]string{"ns1"},
		},
		{
			"multiple namespaces",
			[]string{"ns1", "ns2"},
		},
		{
			"all namespaces",
			[]string{""},
		},
	}

	for _, tt := range tests {
		suite.Run(tt.name, func() {
			suite.TearDownTest()
			suite.SetupTest()

			// create file
			f := suite.createContainerLogFile("ns1", "pn", "cn", "123")
			f.Write([]byte("123"))
			f.Close()

			// init watcher
			watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, tt.setNamespaces)
			suite.Require().Nil(err)

			var wg sync.WaitGroup

			// check that file removal event gets picked up
			wg.Add(1)
			go func() {
				defer wg.Done()

				// handle cases were os emits CHMOD events before REMOVE (e.g. ubuntu)
				var lastEv fsnotify.Event
				for ev := range watcher.Events {
					lastEv = ev
				}

				suite.True(lastEv.Op.Has(fsnotify.Remove))
			}()

			// delete file
			err = suite.removeContainerLogFile("ns1", "pn", "cn", "123")
			suite.Require().Nil(err)

			// wait for event to get processed
			time.Sleep(50 * time.Millisecond)

			watcher.Close()

			// wait
			wg.Wait()
		})
	}
}

func (suite *ContainerLogsWatcherTestSuite) TestDeleteOutsideNamespace() {
	// create file
	f := suite.createContainerLogFile("ns1", "pn", "cn", "123")
	f.Write([]byte("123"))
	f.Close()

	// init watcher
	watcher, err := newContainerLogsWatcher(context.Background(), suite.containerLogsDir, []string{"ns2"})
	suite.Require().Nil(err)

	var wg sync.WaitGroup

	// check that file modification event gets ignored
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, ok := <-watcher.Events
		suite.Require().False(ok)
	}()

	// delete file in ns1
	err = os.Remove(f.Name())
	suite.Require().Nil(err)

	// close
	watcher.Close()

	// wait
	wg.Wait()
}

// test runner
func TestContainerLogsWatcher(t *testing.T) {
	suite.Run(t, new(ContainerLogsWatcherTestSuite))
}
