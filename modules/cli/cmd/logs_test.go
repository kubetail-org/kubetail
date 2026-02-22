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

	t.Run("tail is zero in follow mode", func(t *testing.T) {
		cmd := &cobra.Command{}

		addLogsCmdFlags(cmd)
		cmd.Flags().Set("follow", fmt.Sprintf("%t", true))

		cmdCfg, err := loadLogsCmdConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, cmdCfg.tailVal, int64(0))
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
			columns:     []string{"timestamp", "dot"},
			withCursors: true,
		},
		wantContains: []string{"--- Prev page: --before 2025-03-13T11:42:01.123456789Z ---", "hello message 1"},
	}, {
		name: "cursors with head mode",
		cmdCfg: logsCmdConfig{
			columns:     []string{"timestamp", "dot"},
			withCursors: true,
			head:        true,
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
