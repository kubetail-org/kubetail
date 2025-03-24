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

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sosodev/duration"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"

	"github.com/kubetail-org/kubetail/modules/cli/internal/cli"
	"github.com/kubetail-org/kubetail/modules/cli/internal/tablewriter"
)

type logsStreamMode int

const (
	logsStreamModeUnknown logsStreamMode = iota
	logsStreamModeHead
	logsStreamModeTail
	logsStreamModeAll
)

const logsHelp = `
Fetch logs for a specific container or a set of workload containers.

Examples:

	- Sources

		# Tail 'web-abc123' pod in 'default' namespace
		kubetail logs web-abc123

    # Tail 'web' deployment in the 'default' namespace
		kubetail logs deployments/web

		# Tail all the deployments in the 'default' namespace
		kubetail logs deployments/*

		# Tail the 'container1' container in the 'web' deployment
		kubetail deployments/web/container1

		# Tail all the containers in the 'web' deployment
		kubtail logs deployments/web/*

		# Tail 'web-abc123' pod in 'frontend' namespace
		kubetail logs frontend:web-abc123

		# Tail 'web' deployment in the 'frontend' namespace
		kubetail logs frontend:deployments/web

		# Tail multiple sources
		kubetail logs <source1> <source2>

	- Tail/Head

		# Return last 10 records from the 'nginx' pod (default container)
		kubetail logs nginx

		# Return last 100 records
		kubetail logs nginx --tail=100 

		# Return first 10 records
		kubetail logs nginx --head

		# Return first 100 records
		kubetail logs nginx --head=100 

		# Stream new records
		kubetail logs nginx --follow

		# Return last 10 records and stream new ones
		kubetail logs nginx --tail --follow

		# Return last 10 records in reverse order (last-to-first)
		kubetail logs nginx --reverse

		# Return all records
		kubetail logs nginx --all

		# Return all records and stream new ones
		kubetail logs nginx --all --follow

		# Return all records in reverse order (last-to-first)
		kubetail logs nginx --all --reverse

	- Time filters

		# Return all records starting from 30 minutes ago
		kubetail logs nginx --since PT30M 

		# Return first 10 records starting from 30 minutes ago
		kubetail logs nginx --since PT30M --head

    # Return last 10 records leading up to 30 minutes ago
    kubetail logs nginx --until PT30M

    # Return all records between two exact timestamps
    kubetail logs nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00

		# Return first 10 records between two exact timestamps
    kubetail logs nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00 --head

		# Return last 10 records between two exact timestamps
    kubetail logs nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00 --tail

	- Grep filter (requires Kubetail Cluster API)

		# Return last 10 records from the 'nginx' pod that match "GET /about"
		kubetail logs nginx --grep "GET /about"

		# Return first 10 records
		kubetail logs nginx --grep "GET /about" --head

		# Return last 10 records that match "GET /about" or "GET /contact"
		kubetail logs ngingx --grep "GET /(about|contact)"

		# Stream new records that match "GET /about"
		kubetail logs nginx --grep "GET /about" --follow

	- Source filters

		# Tail 'web' deployment pods in 'us-east-1'
		kubetail logs deployments/web --region=us-east-1

		# Tail 'web' deployment pods in 'us-east-1' or 'us-east-2'
		kubetail logs deployments/web --region=us-east-1,us-east-2

		# Tail 'web' deployment pods in 'us-east-1' running on 'arm64'
		kubetail logs deployments/web --region=us-east-1 --arch=arm64

		# Tail 'web' deployment pods in 'us-east-1a' zone
		kubetail logs deployments/web --zone=us-east-1a

		# Tail 'web' deployment pods in 'us-east-1a' or 'us-east-1b' zone
		kubetail logs deployments/web --zone=us-east-1a,us-east-1b

Notes:

	- The 'since' and 'until' flags accept the following:

		* ISO 8601 timestamp (e.g., "2006-01-02T15:04:05Z07:00")
		* ISO 8601 duration (e.g., "PT5M")

	- The 'after' and 'before' flags accept the following:

	  * ISO 8601 timestamp (e.g., "2006-01-02T15:04:05Z07:00")

	- Using 'head'/'tail'/'all' flags together is not allowed

	- Default behavior is "tail" unless 'since' is specified

	- Using 'grep' requires 'force' because the command may unexpectedly download
	  more log records than expected

`

var logsCmd = &cobra.Command{
	Use:   "logs [source1] [source2] ...",
	Short: "Fetch logs for a container or a set of workloads",
	Long:  strings.ReplaceAll(logsHelp, "\t", "  "),
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		grep, _ := flags.GetString("grep")
		force, _ := flags.GetBool("force")

		if grep != "" && !force {
			return fmt.Errorf("--force is required when using --grep")
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		flags := cmd.Flags()

		kubeContext, _ := flags.GetString("kube-context")

		head := flags.Changed("head")
		headVal, _ := flags.GetInt64("head")

		tail := flags.Changed("tail")
		tailVal, _ := flags.GetInt64("tail")
		all, _ := flags.GetBool("all")
		follow, _ := flags.GetBool("follow")

		since, _ := flags.GetString("since")
		until, _ := flags.GetString("until")
		after, _ := flags.GetString("after")
		before, _ := flags.GetString("before")

		grep, _ := flags.GetString("grep")
		regionList, _ := flags.GetStringSlice("region")
		zoneList, _ := flags.GetStringSlice("zone")
		osList, _ := flags.GetStringSlice("os")
		archList, _ := flags.GetStringSlice("arch")
		nodeList, _ := flags.GetStringSlice("node")

		withTs, _ := flags.GetBool("with-ts")
		withNode, _ := flags.GetBool("with-node")
		withRegion, _ := flags.GetBool("with-region")
		withZone, _ := flags.GetBool("with-zone")
		withOS, _ := flags.GetBool("with-os")
		withArch, _ := flags.GetBool("with-arch")
		withNamespace, _ := flags.GetBool("with-namespace")
		withPod, _ := flags.GetBool("with-pod")
		withContainer, _ := flags.GetBool("with-container")
		withCursors, _ := flags.GetBool("with-cursors")

		hideHeader, _ := flags.GetBool("hide-header")

		// Stream mode
		streamMode := logsStreamModeUnknown
		if head {
			streamMode = logsStreamModeHead
		} else if tail {
			streamMode = logsStreamModeTail
		} else if all {
			streamMode = logsStreamModeAll
		} else if since != "" {
			streamMode = logsStreamModeHead
		} else {
			streamMode = logsStreamModeTail
		}

		// Default tail num to 0 if follow is true
		if follow && !tail {
			tailVal = 0
		}

		// Parse `since`
		sinceTime, err := parseTimeArg(since)
		cli.ExitOnError(err)

		// Parse `until`
		untilTime, err := parseTimeArg(until)
		cli.ExitOnError(err)

		// Parse `after`
		afterTime, err := parseTimeArg(after)
		cli.ExitOnError(err)

		// Parse `before`
		beforeTime, err := parseTimeArg(before)
		cli.ExitOnError(err)

		// Handle after/before
		if !afterTime.IsZero() {
			sinceTime = afterTime.Add(1 * time.Nanosecond)
		}

		if !beforeTime.IsZero() {
			untilTime = beforeTime.Add(-1 * time.Nanosecond)
		}

		// Init connection manager
		cm, err := k8shelpers.NewDesktopConnectionManager()
		cli.ExitOnError(err)

		kubeContextPtr := ptr.To(kubeContext)

		// Init clientset
		clientset, err := cm.GetOrCreateClientset(kubeContextPtr)
		cli.ExitOnError(err)

		// Init stream
		streamOpts := []logs.StreamOption{
			logs.WithDefaultNamespace(cm.GetDefaultNamespace(kubeContextPtr)),
			logs.WithSince(sinceTime),
			logs.WithUntil(untilTime),
			logs.WithFollow(follow),
			logs.WithGrep(grep),
			logs.WithRegions(regionList),
			logs.WithZones(zoneList),
			logs.WithOSes(osList),
			logs.WithArches(archList),
			logs.WithNodes(nodeList),
		}

		switch streamMode {
		case logsStreamModeHead:
			streamOpts = append(streamOpts, logs.WithHead(headVal))
		case logsStreamModeTail:
			streamOpts = append(streamOpts, logs.WithTail(tailVal))
		case logsStreamModeAll:
			streamOpts = append(streamOpts, logs.WithAll())
		default:
			cli.ExitOnError(fmt.Errorf("invalid stream mode: %d", streamMode))
		}

		// Initalize context that stops on SIGTERM
		rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop() // clean up resources

		stream, err := logs.NewStream(rootCtx, clientset, args, streamOpts...)
		cli.ExitOnError(err)

		// Start stream
		err = stream.Start(rootCtx)
		cli.ExitOnError(err)

		// Write records to stdout
		writer := bufio.NewWriter(cmd.OutOrStdout())

		headers, colWidths := getTableWriterHeaders(flags, stream.Sources())
		tw := tablewriter.NewTableWriter(writer, colWidths)

		// Print header
		showHeader := withTs || withNode || withRegion || withZone || withOS || withArch || withNamespace || withPod || withContainer
		if showHeader && !hideHeader {
			tw.PrintHeader(headers)
			writer.Flush()
		}

		// Write rows
		var firstRecord, lastRecord *logs.LogRecord
		for record := range stream.Records() {
			if firstRecord == nil {
				firstRecord = &record
			}
			lastRecord = &record

			// Prepare row data
			row := []string{}
			if withTs {
				row = append(row, record.Timestamp.Format(time.RFC3339Nano))
			}
			if withNode {
				row = append(row, record.Source.NodeName)
			}
			if withRegion {
				row = append(row, orDefault(record.Source.Metadata.Region, "-"))
			}
			if withZone {
				row = append(row, orDefault(record.Source.Metadata.Zone, "-"))
			}
			if withOS {
				row = append(row, orDefault(record.Source.Metadata.OperatingSystem, "-"))
			}
			if withArch {
				row = append(row, orDefault(record.Source.Metadata.Architecture, "-"))
			}
			if withNamespace {
				row = append(row, orDefault(record.Source.Namespace, "-"))
			}
			if withPod {
				row = append(row, orDefault(record.Source.PodName, "-"))
			}
			if withContainer {
				row = append(row, orDefault(record.Source.ContainerName, "-"))
			}
			row = append(row, record.Message)

			// Add row to table
			tw.WriteRow(row)
			writer.Flush()
		}

		// Exit early if user issued SIGTERM
		if rootCtx.Err() != nil {
			return
		}

		// Output paging cursors if requested
		if withCursors && !follow && !all {
			if head && lastRecord != nil {
				// For head mode, the last record's timestamp is used as the "after" cursor for the next page
				fmt.Fprintf(cmd.OutOrStderr(), "\n--- Next page: --after %s ---\n", lastRecord.Timestamp.Format(time.RFC3339Nano))
			} else if firstRecord != nil {
				// For tail mode, the first record's timestamp would be used as the "before" cursor
				fmt.Fprintf(cmd.OutOrStderr(), "\n--- Prev page: --before %s ---\n", firstRecord.Timestamp.Format(time.RFC3339Nano))
			}
		}

		// Graceful close
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = stream.Close(ctx)
		cli.ExitOnError(err)
	},
}

// Return table writer headers and col widths
func getTableWriterHeaders(flags *pflag.FlagSet, sources []logs.LogSource) ([]string, []int) {
	withTs, _ := flags.GetBool("with-ts")
	withNode, _ := flags.GetBool("with-node")
	withRegion, _ := flags.GetBool("with-region")
	withZone, _ := flags.GetBool("with-zone")
	withOS, _ := flags.GetBool("with-os")
	withArch, _ := flags.GetBool("with-arch")
	withNamespace, _ := flags.GetBool("with-namespace")
	withPod, _ := flags.GetBool("with-pod")
	withContainer, _ := flags.GetBool("with-container")

	headers := []string{}
	colWidths := []int{}

	// Calculate max lengths from sources
	maxNodeLen := len("NODE")
	maxRegionLen := len("REGION")
	maxZoneLen := len("ZONE")
	maxOSLen := len("OS")
	maxArchLen := len("ARCH")
	maxNamespaceLen := len("NAMESPACE")
	maxPodLen := len("POD")
	maxContainerLen := len("CONTAINER")

	// Find maximum length for each attribute across all sources
	for _, source := range sources {
		maxNodeLen = max(maxNodeLen, len(source.NodeName))
		maxRegionLen = max(maxRegionLen, len(source.Metadata.Region))
		maxZoneLen = max(maxZoneLen, len(source.Metadata.Zone))
		maxOSLen = max(maxOSLen, len(source.Metadata.OperatingSystem))
		maxArchLen = max(maxArchLen, len(source.Metadata.Architecture))
		maxNamespaceLen = max(maxArchLen, len(source.Namespace))
		maxPodLen = max(maxArchLen, len(source.PodName))
		maxContainerLen = max(maxArchLen, len(source.ContainerName))
	}

	if withTs {
		headers = append(headers, "TIMESTAMP")
		colWidths = append(colWidths, 30) // Fixed width for timestamp
	}
	if withNode {
		headers = append(headers, "NODE")
		colWidths = append(colWidths, maxNodeLen)
	}
	if withRegion {
		headers = append(headers, "REGION")
		colWidths = append(colWidths, maxRegionLen)
	}
	if withZone {
		headers = append(headers, "ZONE")
		colWidths = append(colWidths, maxZoneLen)
	}
	if withOS {
		headers = append(headers, "OS")
		colWidths = append(colWidths, maxOSLen)
	}
	if withArch {
		headers = append(headers, "ARCH")
		colWidths = append(colWidths, maxArchLen)
	}
	if withNamespace {
		headers = append(headers, "NAMESPACE")
		colWidths = append(colWidths, maxNamespaceLen)
	}
	if withPod {
		headers = append(headers, "POD")
		colWidths = append(colWidths, maxPodLen)
	}
	if withContainer {
		headers = append(headers, "CONTAINER")
		colWidths = append(colWidths, maxContainerLen)
	}
	headers = append(headers, "MESSAGE")

	return headers, colWidths
}

// Parse an input either as an ISO timestamp or an ISO duration string
func parseTimeArg(arg string) (time.Time, error) {
	var zero time.Time

	arg = strings.TrimSpace(arg)
	if arg == "" {
		return zero, nil
	} else if timeAgo, err := duration.Parse(arg); err == nil {
		// Parsed as ISO duration
		return time.Now().Add(-1 * timeAgo.ToTimeDuration()), nil
	} else if ts, err := time.Parse(time.RFC3339Nano, arg); err == nil {
		// Parsed as ISO timestamp
		return ts, nil
	}

	return zero, fmt.Errorf("unable to parse arg %s", arg)
}

// Return value or default
func orDefault[T comparable](val T, defaultVal T) T {
	var zero T
	if val == zero {
		return defaultVal
	}
	return val
}

func init() {
	rootCmd.AddCommand(logsCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// serveCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// serveCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	flagset := logsCmd.Flags()
	flagset.SortFlags = false

	flagset.String("kube-context", "", "Specify the kubeconfig context to use")

	flagset.Int64P("head", "h", 10, "Return first N records")
	flagset.Lookup("head").NoOptDefVal = "10"
	flagset.Int64P("tail", "t", 10, "Return last N records")
	flagset.Lookup("tail").NoOptDefVal = "10"
	flagset.Bool("all", false, "Return all records")
	logsCmd.MarkFlagsMutuallyExclusive("head", "tail", "all")

	flagset.BoolP("follow", "f", false, "Stream new records")

	flagset.String("since", "", "Include records from the specified point (inclusive)")
	flagset.String("until", "", "Include records up to the specified point (inclusive)")
	flagset.String("after", "", "Include records strictly after the specified point")
	flagset.String("before", "", "Include records strictly before the specified point")
	logsCmd.MarkFlagsMutuallyExclusive("since", "after")
	logsCmd.MarkFlagsMutuallyExclusive("until", "before")

	flagset.StringP("grep", "g", "", "Filter records by a regular expression")

	flagset.StringSlice("region", []string{}, "Filter source pods by region")
	flagset.StringSlice("zone", []string{}, "Filter source pods by zone")
	flagset.StringSlice("os", []string{}, "Filter source pods by operating system")
	flagset.StringSlice("arch", []string{}, "Filter source pods by CPU architecture")
	flagset.StringSlice("node", []string{}, "Filter source pods by node name")

	flagset.Bool("with-ts", false, "Show the timestamp of each record")
	flagset.Bool("with-node", false, "Show the source node of each record")
	flagset.Bool("with-region", false, "Show the source region of each record")
	flagset.Bool("with-zone", false, "Show the source zone of each record")
	flagset.Bool("with-os", false, "Show the source operating system of each record")
	flagset.Bool("with-arch", false, "Show the source architecture of each record")
	flagset.Bool("with-namespace", false, "Show the source namespace of each record")
	flagset.Bool("with-pod", false, "Show the source pod of each record")
	flagset.Bool("with-container", false, "Show the source container of each record")
	flagset.Bool("with-cursors", false, "Show paging cursors")

	flagset.Bool("hide-header", false, "Hide table header")

	//flagset.BoolP("reverse", "r", false, "List records in reverse order")

	flagset.Bool("force", false, "Force command (if necessary)")

	// Define help here to avoid re-defining 'h' shorthand
	flagset.Bool("help", false, "help for logs")
}
