package cmd

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
	stream "github.com/kubetail-org/kubetail/modules/shared/logs/mock"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestLoadLogConfig(t *testing.T) {
	t.Run("raw flag overrides display flags", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)

		flags := cmd.Flags()
		flags.Set("raw", fmt.Sprintf("%t", true))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, cmdCfg.hideHeader, true)
		assert.Empty(t, cmdCfg.columns)
		assert.Equal(t, cmdCfg.allContainers, false)
	})

	t.Run("tail defaults to 10 in follow mode (tail -f semantics)", func(t *testing.T) {
		cmd := &cobra.Command{}

		addLogsCmdFlags(cmd)
		cmd.Flags().Set("follow", fmt.Sprintf("%t", true))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, int64(10), cmdCfg.tailVal)
	})

	t.Run("--since without head/tail/all resolves to head mode", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		cmd.Flags().Set("since", "PT30M")

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)
		assert.Equal(t, logsStreamModeHead, cmdCfg.resolvedMode)
	})

	t.Run("--tail=0 propagates as tailVal=0", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		cmd.Flags().Set("tail", "0")

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, int64(0), cmdCfg.tailVal)
	})

	t.Run("wrong timestamp returns an error", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)

		cmd.Flags().Set("since", "wrong-timestamp")
		_, err := loadLogsCmdConfig(cmd)

		assert.Error(t, err)
	})

	t.Run("add and remove nanosecond when --after and --before are used", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)

		t1 := time.Date(2025, 3, 13, 11, 42, 1, 123456789, time.UTC)
		expectedSinceTime := time.Date(2025, 3, 13, 11, 42, 1, 123456790, time.UTC)
		expectedUntilTime := time.Date(2025, 3, 13, 11, 42, 1, 123456788, time.UTC)

		cmd.Flags().Set("after", t1.Format(time.RFC3339Nano))
		cmd.Flags().Set("before", t1.Format(time.RFC3339Nano))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, cmdCfg.sinceTime, expectedSinceTime)
		assert.Equal(t, cmdCfg.untilTime, expectedUntilTime)
	})

	t.Run("--columns replaces default columns", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		err := cmd.Flags().Set("columns", "pod,container")
		assert.NoError(t, err)

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, []string{"pod", "container"}, cmdCfg.columns)
	})

	t.Run("--add-columns and --remove-columns update current set", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		assert.NoError(t, cmd.Flags().Set("add-columns", "pod,namespace"))
		assert.NoError(t, cmd.Flags().Set("remove-columns", "dot"))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, []string{"timestamp", "pod", "namespace"}, cmdCfg.columns)
	})

	t.Run("remove-columns removes from explicit columns", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		assert.NoError(t, cmd.Flags().Set("columns", "pod,node,timestamp"))
		assert.NoError(t, cmd.Flags().Set("remove-columns", "node"))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, []string{"pod", "timestamp"}, cmdCfg.columns)
	})

	t.Run("unknown column values are accepted", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)
		assert.NoError(t, cmd.Flags().Set("columns", "pod,invalid"))
		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)
		assert.Equal(t, []string{"pod", "invalid"}, cmdCfg.columns)
	})
}

func TestBuildClusterAPIStreamConfig(t *testing.T) {
	t.Run("passes through base fields", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{
			kubecontext:  "ctx-1",
			grep:         "GET /about",
			follow:       true,
			resolvedMode: logsStreamModeTail,
			tailVal:      10,
		}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"deployments/web", "deployments/api"})

		assert.Equal(t, "ctx-1", got.KubeContext)
		assert.Equal(t, []string{"deployments/web", "deployments/api"}, got.Sources)
		assert.Equal(t, "GET /about", got.Grep)
		assert.True(t, got.Follow)
	})

	t.Run("formats since/until as RFC3339Nano", func(t *testing.T) {
		since := time.Date(2024, 1, 2, 15, 4, 5, 123456789, time.UTC)
		until := time.Date(2024, 1, 2, 16, 0, 0, 0, time.UTC)
		cmdCfg := &logsCmdConfig{
			sinceTime:    since,
			untilTime:    until,
			resolvedMode: logsStreamModeTail,
			tailVal:      10,
		}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Equal(t, since.Format(time.RFC3339Nano), got.Since)
		assert.Equal(t, until.Format(time.RFC3339Nano), got.Until)
	})

	t.Run("omits since/until when zero", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeTail, tailVal: 10}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Empty(t, got.Since)
		assert.Empty(t, got.Until)
	})

	t.Run("head mode sets HEAD with limit and no pagination", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeHead, headVal: 25}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Equal(t, "HEAD", got.Mode)
		assert.Equal(t, 25, got.Limit)
	})

	t.Run("all mode sets HEAD with paginate and no limit", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeAll}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Equal(t, "HEAD", got.Mode)
		assert.Zero(t, got.Limit, "--all must not impose a client-side limit cap")
		assert.True(t, got.Paginate, "--all must walk every page via NextCursor")
	})

	t.Run("head mode does not paginate", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeHead, headVal: 25}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.False(t, got.Paginate, "--head=N must stop after one page")
	})

	t.Run("tail mode sets TAIL with limit", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeTail, tailVal: 50}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Equal(t, "TAIL", got.Mode)
		assert.Equal(t, 50, got.Limit)
	})

	t.Run("tail mode with tailVal=0 skips bootstrap", func(t *testing.T) {
		// --tail=0 is the "follow only, no backlog" path — leaving Mode empty
		// tells Stream.Start to skip the bootstrap fetch entirely.
		cmdCfg := &logsCmdConfig{resolvedMode: logsStreamModeTail, tailVal: 0, follow: true}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Empty(t, got.Mode, "tail=0 must leave Mode empty to skip bootstrap")
		assert.Zero(t, got.Limit)
		assert.True(t, got.Follow)
	})

	t.Run("passes through source filters", func(t *testing.T) {
		cmdCfg := &logsCmdConfig{
			resolvedMode: logsStreamModeTail,
			tailVal:      10,
			regionList:   []string{"us-east-1", "us-east-2"},
			zoneList:     []string{"us-east-1a"},
			osList:       []string{"linux"},
			archList:     []string{"amd64", "arm64"},
			nodeList:     []string{"node-1"},
		}
		got := buildClusterAPIStreamConfig(cmdCfg, []string{"x"})

		assert.Equal(t, []string{"us-east-1", "us-east-2"}, got.Regions)
		assert.Equal(t, []string{"us-east-1a"}, got.Zones)
		assert.Equal(t, []string{"linux"}, got.OSes)
		assert.Equal(t, []string{"amd64", "arm64"}, got.Arches)
		assert.Equal(t, []string{"node-1"}, got.Nodes)
	})
}

func TestPrintLogs(t *testing.T) {
	s1 := logs.LogSource{
		PodName:       "pod1",
		Namespace:     "ns1",
		ContainerName: "container1",
		ContainerID:   "cont1",
		Metadata: logs.LogSourceMetadata{
			Region: "us-east",
			Zone:   "us-east-a1",
			OS:     "linux",
			Arch:   "x64",
			Node:   "node1",
		},
	}

	t1 := time.Date(2025, 3, 13, 11, 42, 1, 123456789, time.UTC)
	t2 := time.Date(2025, 3, 13, 11, 45, 2, 123456789, time.UTC)

	logRecords := []logs.LogRecord{
		{Source: s1, Timestamp: t1, Message: "hello message 1"},
		{Source: s1, Timestamp: t2, Message: "hello message 2"},
	}

	tests := []struct {
		name            string
		cmdCfg          logsCmdConfig
		wantContains    []string
		wantNotContains []string
	}{{
		name: "logs with header, timestamp, message and dot indicator",
		cmdCfg: logsCmdConfig{
			columns: []string{"timestamp", "dot"},
		},
		wantContains:    []string{"TIMESTAMP", "MESSAGE", "\u25CB", "2025-03-13T11:42:01.123456789Z", "hello message 1"},
		wantNotContains: []string{},
	}, {
		name: "logs with hideHeader",
		cmdCfg: logsCmdConfig{
			hideHeader: true,
			columns:    []string{"timestamp", "dot"},
		},
		wantContains:    []string{"hello message 1"},
		wantNotContains: []string{"TIMESTAMP", "MESSAGE", "\u25CB"},
	}, {
		name: "test show logs without timestamp",
		cmdCfg: logsCmdConfig{
			columns: []string{"dot"},
		},
		wantContains:    []string{"hello message 1"},
		wantNotContains: []string{"TIMESTAMP", "2025-03-13T11:42:01.123456789Z"},
	}, {
		name: "show logs with region, zone, os, arch, node",
		cmdCfg: logsCmdConfig{
			columns: []string{"timestamp", "dot", "region", "zone", "os", "arch", "node"},
		},
		wantContains:    []string{"NODE", "node1", "REGION", "us-east", "OS", "linux", "ARCH", "x64", "ZONE", "us-east-a1"},
		wantNotContains: []string{},
	}, {
		name: "show logs with pod, namespace",
		cmdCfg: logsCmdConfig{
			columns: []string{"timestamp", "dot", "pod", "container", "namespace"},
		},
		wantContains: []string{"POD", "pod1", "CONTAINER", "container1", "NAMESPACE", "ns1", "hello message 1"},
	}, {
		name: "cursors with tail mode",
		cmdCfg: logsCmdConfig{
			columns:      []string{"timestamp", "dot"},
			withCursors:  true,
			resolvedMode: logsStreamModeTail,
		},
		wantContains: []string{"--- Prev page: --before 2025-03-13T11:42:01.123456789Z ---", "hello message 1"},
	}, {
		name: "cursors with head mode",
		cmdCfg: logsCmdConfig{
			columns:      []string{"timestamp", "dot"},
			withCursors:  true,
			resolvedMode: logsStreamModeHead,
		},
		wantContains: []string{"--- Next page: --after 2025-03-13T11:45:02.123456789Z ---", "hello message 1"},
	}}

	for _, test := range tests {
		cmd := &cobra.Command{}

		stdout := &bytes.Buffer{}
		cmd.SetOut(stdout)

		mockStream := &stream.MockStream{}
		outCh := make(chan logs.LogRecord, len(logRecords))

		for _, record := range logRecords {
			outCh <- record
		}
		close(outCh)

		mockStream.On("Records").Return((<-chan logs.LogRecord)(outCh))
		mockStream.On("Sources").Return([]logs.LogSource{s1})
		mockStream.On("Err").Return(nil)

		printLogs(context.Background(), cmd, &test.cmdCfg, mockStream)

		for _, want := range test.wantContains {
			assert.Contains(t, stdout.String(), want)
		}

		for _, want := range test.wantNotContains {
			assert.NotContains(t, stdout.String(), want)
		}

		// checking if all the mock calls ran properly
		mockStream.AssertExpectations(t)
	}
}
