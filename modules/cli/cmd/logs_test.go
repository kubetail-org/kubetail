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

func defaultLogFlags() logFlags {
	return logFlags{
		kubecontext:    "",
		inCluster:      false,
		kubeconfigPath: "",

		head:    false,
		headVal: 10,
		tail:    false,
		tailVal: 10,
		all:     false,
		follow:  false,

		grep:       "",
		regionList: []string{},
		zoneList:   []string{},
		osList:     []string{},
		archList:   []string{},
		nodeList:   []string{},

		hideHeader: false,
		hideTs:     false,
		hideDot:    false,

		withTs:        true,
		withDot:       true,
		allContainers: false,

		withNode:      false,
		withRegion:    false,
		withZone:      false,
		withOS:        false,
		withArch:      false,
		withNamespace: false,
		withPod:       false,
		withContainer: false,
		withCursors:   false,

		raw: false,
	}
}

func TestLoadLogConfig(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(cmd *cobra.Command)
		expected logFlags
	}{
		{

			name: "Test default flags",
			setup: func(cmd *cobra.Command) {
			},
			expected: defaultLogFlags(),
		},
		{

			name: "Test connection flags",
			setup: func(cmd *cobra.Command) {
				// adding this to test the flags added in the root command
				cmd.Flags().String(KubeconfigFlag, "", "Path to kubeconfig file")
				cmd.Flags().Bool(InClusterFlag, false, "Use in-cluster Kubernetes configuration")

				cmd.Flags().Set(KubeContextFlag, "cluster1")
				cmd.Flags().Set(KubeconfigFlag, "~/.kube/config")
				cmd.Flags().Set(InClusterFlag, fmt.Sprintf("%t", true))
			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.kubecontext = "cluster1"
				f.kubeconfigPath = "~/.kube/config"
				f.inCluster = true
				return f
			}(),
		},
		{
			name: "Test stream mode flags",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("head", fmt.Sprintf("%v", 20))
				cmd.Flags().Set("tail", fmt.Sprintf("%v", 30))
				cmd.Flags().Set("all", fmt.Sprintf("%t", true))
				cmd.Flags().Set("follow", fmt.Sprintf("%t", true))
			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.headVal = 20
				f.tailVal = 30
				f.head = true
				f.tail = true

				f.all = true
				f.follow = true

				return f
			}(),
		},
		{
			name: "Test resource filter flags",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("grep", "GET /about")
				cmd.Flags().Set("region", "us-east,ap-south-1")
				cmd.Flags().Set("zone", "us-east-1a,ap-south-a1")
				cmd.Flags().Set("os", "linux")
				cmd.Flags().Set("arch", "x86,arm64")
				cmd.Flags().Set("node", "minikube")
			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.grep = "GET /about"
				f.regionList = []string{"us-east", "ap-south-1"}
				f.zoneList = []string{"us-east-1a", "ap-south-a1"}
				f.osList = []string{"linux"}
				f.archList = []string{"x86", "arm64"}
				f.nodeList = []string{"minikube"}
				return f
			}(),
		}, {
			name: "Test visibility flags",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("hide-header", fmt.Sprintf("%t", true))
				cmd.Flags().Set("hide-ts", fmt.Sprintf("%t", true))
				cmd.Flags().Set("hide-dot", fmt.Sprintf("%t", true))
			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.hideDot = true
				f.hideHeader = true
				f.hideTs = true

				f.withTs = false
				f.withDot = false

				return f
			}(),
		},
		{
			name: "Test raw flag",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("raw", fmt.Sprintf("%t", true))
				cmd.Flags().Set("all-containers", fmt.Sprintf("%t", true))
			},
			expected: func() logFlags {
				f := defaultLogFlags()

				f.hideHeader = true
				f.withTs = false
				f.withNode = false
				f.withRegion = false
				f.withZone = false
				f.withOS = false
				f.withArch = false
				f.withNamespace = false
				f.withPod = false
				f.withContainer = false
				f.withDot = false
				f.allContainers = false

				f.raw = true

				return f
			}(),
		},
		{
			name: "Test metadata flags",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("with-node", fmt.Sprintf("%t", true))
				cmd.Flags().Set("with-zone", fmt.Sprintf("%t", true))
				cmd.Flags().Set("with-os", fmt.Sprintf("%t", true))
				cmd.Flags().Set("with-arch", fmt.Sprintf("%t", true))
				cmd.Flags().Set("with-region", fmt.Sprintf("%t", true))
				cmd.Flags().Set("with-namespace", fmt.Sprintf("%t", false))
				cmd.Flags().Set("with-pod", fmt.Sprintf("%t", false))
				cmd.Flags().Set("with-container", fmt.Sprintf("%t", false))
				cmd.Flags().Set("with-cursors", fmt.Sprintf("%t", false))
				cmd.Flags().Set("all-containers", fmt.Sprintf("%t", true))

			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.withNode = true
				f.withZone = true
				f.withOS = true
				f.withArch = true
				f.withRegion = true
				f.withNamespace = false
				f.withPod = false
				f.withContainer = false
				f.withCursors = false

				f.allContainers = true

				return f
			}(),
		},
		{
			name: "Test follow without tail sets tailVal to 0",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Set("follow", fmt.Sprintf("%t", true))
			},
			expected: func() logFlags {
				f := defaultLogFlags()
				f.follow = true
				f.tailVal = 0
				return f
			}(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			addLogsCmdFlags(cmd)

			test.setup(cmd)
			flags, streamOpts := loadLogConfig(cmd)

			assert.NotEmpty(t, streamOpts)
			assert.Len(t, streamOpts, 12)
			assert.Equal(t, &test.expected, flags)
		})
	}
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
		flags           logFlags
		wantContains    []string
		wantNotContains []string
	}{{
		name: "logs with header, timestamp, message and dot indicator",
		flags: func() logFlags {
			f := defaultLogFlags()
			return f
		}(),
		wantContains:    []string{"TIMESTAMP", "MESSAGE", "\u25CB", "2025-03-13T11:42:01.123456789Z", "hello message 1"},
		wantNotContains: []string{},
	}, {
		name: "logs with hideHeader",
		flags: func() logFlags {
			f := defaultLogFlags()

			f.hideHeader = true
			return f
		}(),
		wantContains:    []string{"hello message 1"},
		wantNotContains: []string{"TIMESTAMP", "MESSAGE", "\u25CB"},
	},
		{
			name: "test show logs without timestamp ",
			flags: func() logFlags {
				f := defaultLogFlags()
				f.withTs = false
				return f
			}(),
			wantContains:    []string{"hello message 1"},
			wantNotContains: []string{"TIMESTAMP", "2025-03-13T11:42:01.123456789Z"},
		},
		{
			name: "show logs with  region, zone, os, arch, node",
			flags: func() logFlags {
				f := defaultLogFlags()
				f.withRegion = true
				f.withZone = true
				f.withOS = true
				f.withArch = true
				f.withNode = true
				return f
			}(),
			wantContains:    []string{"NODE", "node1", "REGION", "us-east", "OS", "linux", "ARCH", "x64", "ZONE", "us-east-a1"},
			wantNotContains: []string{},
		},
		{
			name: "show logs with pod, namespace",
			flags: func() logFlags {
				f := defaultLogFlags()
				f.withPod = true
				f.withContainer = true
				f.withNamespace = true
				return f
			}(),
			wantContains: []string{"POD", "pod1", "CONTAINER", "container1", "NAMESPACE", "ns1", "hello message 1"},
		},
		{
			name: "cursors with tail mode",
			flags: func() logFlags {
				f := defaultLogFlags()
				f.withCursors = true
				return f
			}(),
			wantContains: []string{"--- Prev page: --before 2025-03-13T11:42:01.123456789Z ---", "hello message 1"},
		},
		{
			name: "cursors with head mode",
			flags: func() logFlags {
				f := defaultLogFlags()
				f.withCursors = true
				f.head = true
				return f
			}(),
			wantContains: []string{"--- Next page: --after 2025-03-13T11:45:02.123456789Z ---", "hello message 1"},
		},
	}

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

		printLogs(rootCtx, cmd, &test.flags, mockStream)

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
