package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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

		cmd.Flags().Set("raw", fmt.Sprintf("%t", true))
		cmd.Flags().Set("hideHeader", fmt.Sprintf("%t", false))
		cmd.Flags().Set("withRegion", fmt.Sprintf("%t", true))

		logsCfg, err := loadLogsConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, logsCfg.hideHeader, true)
		assert.Equal(t, logsCfg.withTs, false)
		assert.Equal(t, logsCfg.withNode, false)
		assert.Equal(t, logsCfg.withRegion, false)
		assert.Equal(t, logsCfg.withOS, false)
		assert.Equal(t, logsCfg.withArch, false)
		assert.Equal(t, logsCfg.withNamespace, false)
		assert.Equal(t, logsCfg.withPod, false)
		assert.Equal(t, logsCfg.withContainer, false)
		assert.Equal(t, logsCfg.withDot, false)
		assert.Equal(t, logsCfg.allContainers, false)

	})

	t.Run("tail is zero in follow mode", func(t *testing.T) {
		cmd := &cobra.Command{}

		addLogsCmdFlags(cmd)
		cmd.Flags().Set("follow", fmt.Sprintf("%t", true))

		logsCfg, err := loadLogsConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, logsCfg.tailVal, int64(0))

	})

	t.Run("wrong timestamp returns an error", func(t *testing.T) {
		cmd := &cobra.Command{}
		addLogsCmdFlags(cmd)

		cmd.Flags().Set("since", "wrong-timestamp")
		_, err := loadLogsConfig(cmd)

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

		logsCfg, err := loadLogsConfig(cmd)
		assert.NoError(t, err)

		assert.Equal(t, logsCfg.sinceTime, expectedSinceTime)
		assert.Equal(t, logsCfg.untilTime, expectedUntilTime)

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
		config          logsConfig
		wantContains    []string
		wantNotContains []string
	}{{
		name: "logs with header, timestamp, message and dot indicator",
		config: logsConfig{
			withTs:  true,
			withDot: true,
		},
		wantContains:    []string{"TIMESTAMP", "MESSAGE", "\u25CB", "2025-03-13T11:42:01.123456789Z", "hello message 1"},
		wantNotContains: []string{},
	}, {
		name: "logs with hideHeader",
		config: logsConfig{
			hideHeader: true,
			withTs:     true,
			withDot:    true,
		},
		wantContains:    []string{"hello message 1"},
		wantNotContains: []string{"TIMESTAMP", "MESSAGE", "\u25CB"},
	}, {
		name: "test show logs without timestamp",
		config: logsConfig{
			withTs:  false,
			withDot: true,
		},
		wantContains:    []string{"hello message 1"},
		wantNotContains: []string{"TIMESTAMP", "2025-03-13T11:42:01.123456789Z"},
	}, {
		name: "show logs with region, zone, os, arch, node",
		config: logsConfig{
			withTs:     true,
			withDot:    true,
			withRegion: true,
			withZone:   true,
			withOS:     true,
			withArch:   true,
			withNode:   true,
		},
		wantContains:    []string{"NODE", "node1", "REGION", "us-east", "OS", "linux", "ARCH", "x64", "ZONE", "us-east-a1"},
		wantNotContains: []string{},
	}, {
		name: "show logs with pod, namespace",
		config: logsConfig{
			withTs:        true,
			withDot:       true,
			withPod:       true,
			withContainer: true,
			withNamespace: true,
		},
		wantContains: []string{"POD", "pod1", "CONTAINER", "container1", "NAMESPACE", "ns1", "hello message 1"},
	}, {
		name: "cursors with tail mode",
		config: logsConfig{
			withTs:      true,
			withDot:     true,
			withCursors: true,
		},
		wantContains: []string{"--- Prev page: --before 2025-03-13T11:42:01.123456789Z ---", "hello message 1"},
	}, {
		name: "cursors with head mode",
		config: logsConfig{
			withTs:      true,
			withDot:     true,
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

		rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		printLogs(rootCtx, cmd, &test.config, mockStream)

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
