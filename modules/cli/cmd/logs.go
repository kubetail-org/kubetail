// Copyright 2024-2026 The Kubetail Authors
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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/go-playground/validator/v10"
	zlog "github.com/rs/zerolog/log"
	"github.com/sosodev/duration"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/kubetail-org/kubetail/modules/shared/config"
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

const logsHelpTmpl = `
Fetch logs for a specific container or a set of workload containers.

Examples:

	- Sources

		# Tail 'web-abc123' pod in 'default' namespace
		{{.CommandDisplayName}} web-abc123

		# Tail 'web' deployment in the 'default' namespace
		{{.CommandDisplayName}} deployments/web

		# Tail all the deployments in the 'default' namespace
		{{.CommandDisplayName}} deployments/*

		# Tail the 'container1' container in the 'web' deployment
		{{.CommandDisplayName}} deployments/web/container1

		# Tail all the containers in the 'web' deployment
		{{.CommandDisplayName}} deployments/web/*

		# Tail 'web-abc123' pod in 'frontend' namespace
		{{.CommandDisplayName}} frontend:web-abc123

		# Tail 'web' deployment in the 'frontend' namespace
		{{.CommandDisplayName}} frontend:deployments/web

		# Tail multiple sources
		{{.CommandDisplayName}} <source1> <source2>

	- Tail/Head

		# Return last 10 records from the 'nginx' pod (default container)
		{{.CommandDisplayName}} nginx

		# Return last 100 records
		{{.CommandDisplayName}} nginx --tail=100 

		# Return first 10 records
		{{.CommandDisplayName}} nginx --head

		# Return first 100 records
		{{.CommandDisplayName}} nginx --head=100 

		# Stream new records
		{{.CommandDisplayName}} nginx --follow

		# Return last 10 records and stream new ones
		{{.CommandDisplayName}} nginx --tail --follow

		# Return all records
		{{.CommandDisplayName}} nginx --all

		# Return all records and stream new ones
		{{.CommandDisplayName}} nginx --all --follow

	- Time filters

		# Return first 10 records starting from 30 minutes ago
		{{.CommandDisplayName}} nginx --since PT30M

		# Return last 10 records leading up to 30 minutes ago
		{{.CommandDisplayName}} nginx --until PT30M

		# Return all records starting from 30 minutes ago
		{{.CommandDisplayName}} nginx --since PT30M --all

		# Return first 10 records between two exact timestamps
		{{.CommandDisplayName}} nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00

		# Return last 10 records between two exact timestamps
		{{.CommandDisplayName}} nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00 --tail

		# Return all records between two exact timestamps
		{{.CommandDisplayName}} nginx --since 2006-01-02T15:04:05Z07:00 --until 2007-01-02T15:04:05Z07:00 --all

	- Grep filter (requires --force)

		# Return last 10 records from the 'nginx' pod that match "GET /about"
		{{.CommandDisplayName}} nginx --grep "GET /about" --force

		# Return first 10 records
		{{.CommandDisplayName}} nginx --grep "GET /about" --head --force

		# Return last 10 records that match "GET /about" or "GET /contact"
		{{.CommandDisplayName}} nginx --grep "GET /(about|contact)" --force

		# Stream new records that match "GET /about"
		{{.CommandDisplayName}} nginx --grep "GET /about" --follow --force

	- Source filters

		# Tail 'web' deployment pods in 'us-east-1'
		{{.CommandDisplayName}} deployments/web --region=us-east-1

		# Tail 'web' deployment pods in 'us-east-1' or 'us-east-2'
		{{.CommandDisplayName}} deployments/web --region=us-east-1,us-east-2

		# Tail 'web' deployment pods in 'us-east-1' running on 'arm64'
		{{.CommandDisplayName}} deployments/web --region=us-east-1 --arch=arm64

		# Tail 'web' deployment pods in 'us-east-1a' zone
		{{.CommandDisplayName}} deployments/web --zone=us-east-1a

		# Tail 'web' deployment pods in 'us-east-1a' or 'us-east-1b' zone
		{{.CommandDisplayName}} deployments/web --zone=us-east-1a,us-east-1b

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

func getLogsHelp() string {
	tmpl := template.Must(template.New("logs").Parse(logsHelpTmpl))

	var buf bytes.Buffer
	data := struct {
		CommandDisplayName string
	}{
		CommandDisplayName: getCommandDisplayName() + " logs",
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		// Fallback in case of template error
		return "Error generating help text"
	}

	return buf.String()
}

var logsCmd = &cobra.Command{
	Use:   "logs [source1] [source2] ...",
	Short: "Fetch logs for a container or a set of workloads",
	Long:  strings.ReplaceAll(getLogsHelp(), "\t", "  "),
	Args:  cobra.MinimumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		flags := cmd.Flags()
		grep, _ := flags.GetString("grep")
		force, _ := flags.GetBool("force")

		if grep != "" && !force {
			return fmt.Errorf("--force is required when using --grep")
		}

		var cli config.CLI
		cli.Config, _ = flags.GetString("config")

		if cli.Config != "" {
			return validator.New().Struct(cli)
		}

		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Get flags
		flags := cmd.Flags()

		configPath, _ := cmd.Flags().GetString("config")
		inCluster, _ := flags.GetBool(InClusterFlag)

		v := viper.New()

		v.BindPFlag("general.kubeconfig", cmd.Flags().Lookup(KubeconfigFlag))
		v.BindPFlag("commands.logs.kube-context", cmd.Flags().Lookup(KubeContextFlag))
		v.BindPFlag("commands.logs.head", cmd.Flags().Lookup("head"))
		v.BindPFlag("commands.logs.tail", cmd.Flags().Lookup("tail"))

		cliCfg, err := config.NewCLIConfigFromViper(v, configPath)
		if err != nil {
			zlog.Fatal().Caller().Err(err).Send()
		}
		kubeContext := cliCfg.Commands.Logs.KubeContext
		kubeconfigPath := cliCfg.General.KubeconfigPath

		head := flags.Changed("head")
		headVal := cliCfg.Commands.Logs.Head

		tail := flags.Changed("tail")
		tailVal := cliCfg.Commands.Logs.Tail

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

		hideHeader, _ := flags.GetBool("hide-header")
		hideTs, _ := flags.GetBool("hide-ts")
		hideDot, _ := flags.GetBool("hide-dot")

		withTs := !hideTs
		withDot := !hideDot
		allContainers, _ := flags.GetBool("all-containers")

		withNode, _ := flags.GetBool("with-node")
		withRegion, _ := flags.GetBool("with-region")
		withZone, _ := flags.GetBool("with-zone")
		withOS, _ := flags.GetBool("with-os")
		withArch, _ := flags.GetBool("with-arch")
		withNamespace, _ := flags.GetBool("with-namespace")
		withPod, _ := flags.GetBool("with-pod")
		withContainer, _ := flags.GetBool("with-container")
		withCursors, _ := flags.GetBool("with-cursors")

		raw, _ := flags.GetBool("raw")
		if raw {
			hideHeader = true
			withTs = false
			withNode = false
			withRegion = false
			withZone = false
			withOS = false
			withArch = false
			withNamespace = false
			withPod = false
			withContainer = false
			withDot = false
			allContainers = false
		}

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
		env := config.EnvironmentDesktop
		if inCluster {
			env = config.EnvironmentCluster
		}
		cm, err := k8shelpers.NewConnectionManager(env, k8shelpers.WithKubeconfigPath(kubeconfigPath), k8shelpers.WithLazyConnect(true))
		cli.ExitOnError(err)

		// Init stream
		streamOpts := []logs.Option{
			logs.WithKubeContext(kubeContext),
			logs.WithSince(sinceTime),
			logs.WithUntil(untilTime),
			logs.WithFollow(follow),
			logs.WithGrep(grep),
			logs.WithRegions(regionList),
			logs.WithZones(zoneList),
			logs.WithOSes(osList),
			logs.WithArches(archList),
			logs.WithNodes(nodeList),
			logs.WithAllContainers(allContainers),
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

		stream, err := logs.NewStream(rootCtx, cm, args, streamOpts...)
		cli.ExitOnError(err)
		defer stream.Close()

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

			if withDot {
				dot := getDotIndicator(record.Source.ContainerID)
				row = append(row, dot)
			}

			if withNode {
				row = append(row, record.Source.Metadata.Node)
			}
			if withRegion {
				row = append(row, orDefault(record.Source.Metadata.Region, "-"))
			}
			if withZone {
				row = append(row, orDefault(record.Source.Metadata.Zone, "-"))
			}
			if withOS {
				row = append(row, orDefault(record.Source.Metadata.OS, "-"))
			}
			if withArch {
				row = append(row, orDefault(record.Source.Metadata.Arch, "-"))
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

		// Exit if stream encountered error
		cli.ExitOnError(stream.Err())

		// Check if any errors occurred during streaming
		if err := stream.Err(); err != nil {
			fmt.Fprintf(cmd.OutOrStderr(), "\nError: %v\n", err)
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
		err = cm.Shutdown(ctx)
		cli.ExitOnError(err)
	},
}

// Return ANSI color coded dot indicator based on container ID
func getDotIndicator(containerID string) string {
	colors := []string{
		"31m", // red
		"32m", // green
		"33m", // yellow
		"34m", // blue
		"35m", // magenta
		"36m", // cyan
		"91m", // bright red
		"92m", // bright green
		"93m", // bright yellow
		"94m", // bright blue
		"95m", // bright magenta
		"96m", // bright cyan
		"37m", // white
		"90m", // gray
		"97m", // bright white
	}

	// simple djb2 hash
	hash := 5381
	for _, val := range containerID {
		hash = int(val) + ((hash << 5) + hash)
	}

	idx := hash % len(colors)

	if idx < 0 {
		idx = -idx
	}

	dot := fmt.Sprintf("\033[%s%s\033[0m", colors[idx], "\u25CF")

	return dot
}

// Return table writer headers and col widths
func getTableWriterHeaders(flags *pflag.FlagSet, sources []logs.LogSource) ([]string, []int) {
	hideTs, _ := flags.GetBool("hide-ts")
	hideDot, _ := flags.GetBool("hide-dot")

	withTs := !hideTs
	withDot := !hideDot
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
		maxNodeLen = max(maxNodeLen, len(source.Metadata.Node))
		maxRegionLen = max(maxRegionLen, len(source.Metadata.Region))
		maxZoneLen = max(maxZoneLen, len(source.Metadata.Zone))
		maxOSLen = max(maxOSLen, len(source.Metadata.OS))
		maxArchLen = max(maxArchLen, len(source.Metadata.Arch))
		maxNamespaceLen = max(maxArchLen, len(source.Namespace))
		maxPodLen = max(maxArchLen, len(source.PodName))
		maxContainerLen = max(maxArchLen, len(source.ContainerName))
	}

	if withTs {
		headers = append(headers, "TIMESTAMP")
		colWidths = append(colWidths, 30) // Fixed width for timestamp
	}

	if withDot {
		headers = append(headers, "\u25CB")
		colWidths = append(colWidths, 1)
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
	defaultConfigPath, _ := config.DefaultConfigPath()
	cliCfg, err := config.NewCLIConfigFromFile(defaultConfigPath)
	if err != nil {
		cliCfg = config.DefaultCLIConfig()
	}

	flagset := logsCmd.Flags()
	flagset.SortFlags = false

	flagset.String(KubeContextFlag, "", "Specify the kubeconfig context to use")
	flagset.Int64P("head", "h", cliCfg.Commands.Logs.Head, "Return last N records")
	flagset.Lookup("head").NoOptDefVal = strconv.Itoa(int(cliCfg.Commands.Logs.Head))
	flagset.Int64P("tail", "t", cliCfg.Commands.Logs.Tail, "Return last N records")
	flagset.Lookup("tail").NoOptDefVal = strconv.Itoa(int(cliCfg.Commands.Logs.Tail))
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

	flagset.Bool("raw", false, "Output only raw log messages without metadata")
	flagset.Bool("hide-ts", false, "Hide the timestamp of each record")
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
	flagset.Bool("hide-dot", false, "Hide the dot indicator in the records")
	flagset.Bool("all-containers", false, "Show logs from all containers in a Pod")

	//flagset.BoolP("reverse", "r", false, "List records in reverse order")

	flagset.Bool("force", false, "Force command (if necessary)")

	// Define help here to avoid re-defining 'h' shorthand
	flagset.Bool("help", false, "help for logs")
}
