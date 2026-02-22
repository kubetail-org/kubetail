// Copyright 2024 The Kubetail Authors
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
	"slices"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sosodev/duration"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"

	"github.com/kubetail-org/kubetail/modules/cli/internal/cli"
	"github.com/kubetail-org/kubetail/modules/cli/internal/tablewriter"
	"github.com/kubetail-org/kubetail/modules/cli/pkg/config"
)

var headFlag config.OptionalInt64
var tailFlag config.OptionalInt64

type logsStreamMode int

const (
	logsStreamModeUnknown logsStreamMode = iota
	logsStreamModeHead
	logsStreamModeTail
	logsStreamModeAll
)

type logsCmdConfig struct {
	streamOpts []logs.Option

	kubecontext    string
	inCluster      bool
	kubeconfigPath string

	sinceTime time.Time
	untilTime time.Time

	head    bool
	headVal int64
	tail    bool
	tailVal int64
	all     bool
	follow  bool

	grep       string
	regionList []string
	zoneList   []string
	osList     []string
	archList   []string
	nodeList   []string
	columns    []string

	hideHeader    bool
	allContainers bool
	withCursors   bool

	raw bool
}

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

func loadLogsCmdConfig(cmd *cobra.Command) (*logsCmdConfig, error) {
	// Get flags
	flags := cmd.Flags()

	configPath, _ := flags.GetString("config")
	inCluster, _ := flags.GetBool(InClusterFlag)

	v := viper.New()

	v.BindPFlag("general.kubeconfig", flags.Lookup(KubeconfigFlag))
	v.BindPFlag("commands.logs.kube-context", flags.Lookup(KubeContextFlag))
	if flags.Changed("columns") {
		columns, _ := flags.GetStringSlice("columns")
		v.Set("commands.logs.columns", columns)
	}

	if headFlag.IsValueProvided {
		v.Set("commands.logs.head", headFlag.Value)
	}
	if tailFlag.IsValueProvided {
		v.Set("commands.logs.tail", tailFlag.Value)
	}

	cfg, err := config.NewConfig(configPath, v)
	if err != nil {
		return nil, err
	}

	kubeContext := cfg.Commands.Logs.KubeContext
	kubeconfigPath := cfg.General.KubeconfigPath

	head := flags.Changed("head")
	headVal := cfg.Commands.Logs.Head

	tail := flags.Changed("tail")
	tailVal := cfg.Commands.Logs.Tail

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
	allContainers, _ := flags.GetBool("all-containers")

	withCursors, _ := flags.GetBool("with-cursors")

	columns := resolveLogsColumns(cfg)
	addColumns, _ := flags.GetStringSlice("add-columns")
	removeColumns, _ := flags.GetStringSlice("remove-columns")
	columns = applyColumnAddRemove(columns, addColumns, removeColumns)

	raw, _ := flags.GetBool("raw")
	if raw {
		hideHeader = true
		columns = []string{}
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
	if err != nil {
		return nil, err
	}

	// Parse `until`
	untilTime, err := parseTimeArg(until)
	if err != nil {
		return nil, err
	}

	// Parse `after`
	afterTime, err := parseTimeArg(after)
	if err != nil {
		return nil, err
	}

	// Parse `before`
	beforeTime, err := parseTimeArg(before)
	if err != nil {
		return nil, err
	}

	// Handle after/before
	if !afterTime.IsZero() {
		sinceTime = afterTime.Add(1 * time.Nanosecond)
	}

	if !beforeTime.IsZero() {
		untilTime = beforeTime.Add(-1 * time.Nanosecond)
	}

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
		return nil, fmt.Errorf("invalid stream mode: %d", streamMode)
	}

	cmdCfg := &logsCmdConfig{
		streamOpts:     streamOpts,
		kubecontext:    kubeContext,
		inCluster:      inCluster,
		kubeconfigPath: kubeconfigPath,

		sinceTime: sinceTime,
		untilTime: untilTime,

		head:    head,
		headVal: headVal,
		tail:    tail,
		tailVal: tailVal,
		all:     all,
		follow:  follow,

		grep:       grep,
		regionList: regionList,
		zoneList:   zoneList,
		osList:     osList,
		archList:   archList,
		nodeList:   nodeList,
		columns:    columns,

		hideHeader:    hideHeader,
		allContainers: allContainers,
		withCursors:   withCursors,

		raw: raw,
	}

	return cmdCfg, nil
}

func resolveLogsColumns(cfg *config.Config) []string {
	return normalizeColumns(cfg.Commands.Logs.Columns)
}

func applyColumnAddRemove(current []string, addColumns []string, removeColumns []string) []string {
	addList := normalizeColumns(addColumns)
	removeList := normalizeColumns(removeColumns)
	updated := append([]string{}, current...)
	for _, col := range addList {
		if !slices.Contains(updated, col) {
			updated = append(updated, col)
		}
	}
	for _, col := range removeList {
		updated = slices.DeleteFunc(updated, func(item string) bool {
			return item == col
		})
	}

	return updated
}

func normalizeColumns(columns []string) []string {
	out := []string{}
	seen := map[string]struct{}{}

	for _, col := range columns {
		item := strings.TrimSpace(strings.ToLower(col))
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}

	return out
}

func printLogs(rootCtx context.Context, cmd *cobra.Command, cmdCfg *logsCmdConfig, stream logs.Stream) {
	// Write records to stdout
	writer := bufio.NewWriter(cmd.OutOrStdout())

	headers, colWidths := getTableWriterHeaders(cmdCfg, stream.Sources())
	tw := tablewriter.NewTableWriter(writer, colWidths)

	// Print header
	showHeader := len(cmdCfg.columns) > 0

	if showHeader && !cmdCfg.hideHeader {
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
		for _, col := range cmdCfg.columns {
			switch col {
			case "timestamp":
				row = append(row, record.Timestamp.Format(time.RFC3339Nano))
			case "dot":
				row = append(row, getDotIndicator(record.Source.ContainerID))
			case "node":
				row = append(row, record.Source.Metadata.Node)
			case "region":
				row = append(row, orDefault(record.Source.Metadata.Region, "-"))
			case "zone":
				row = append(row, orDefault(record.Source.Metadata.Zone, "-"))
			case "os":
				row = append(row, orDefault(record.Source.Metadata.OS, "-"))
			case "arch":
				row = append(row, orDefault(record.Source.Metadata.Arch, "-"))
			case "namespace":
				row = append(row, orDefault(record.Source.Namespace, "-"))
			case "pod":
				row = append(row, orDefault(record.Source.PodName, "-"))
			case "container":
				row = append(row, orDefault(record.Source.ContainerName, "-"))
			}
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
	if cmdCfg.withCursors && !cmdCfg.follow && !cmdCfg.all {
		if cmdCfg.head && lastRecord != nil {
			// For head mode, the last record's timestamp is used as the "after" cursor for the next page
			fmt.Fprintf(cmd.OutOrStderr(), "\n--- Next page: --after %s ---\n", lastRecord.Timestamp.Format(time.RFC3339Nano))
		} else if firstRecord != nil {
			// For tail mode, the first record's timestamp would be used as the "before" cursor
			fmt.Fprintf(cmd.OutOrStderr(), "\n--- Prev page: --before %s ---\n", firstRecord.Timestamp.Format(time.RFC3339Nano))
		}
	}
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
		cmdCfg, err := loadLogsCmdConfig(cmd)

		cli.ExitOnError(err)

		// Initalize context that stops on SIGTERM
		rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop() // clean up resources

		// Init connection manager
		env := sharedcfg.EnvironmentDesktop
		if cmdCfg.inCluster {
			env = sharedcfg.EnvironmentCluster
		}
		cm, err := k8shelpers.NewConnectionManager(env, k8shelpers.WithKubeconfigPath(cmdCfg.kubeconfigPath), k8shelpers.WithLazyConnect(true))
		cli.ExitOnError(err)

		stream, err := logs.NewStream(rootCtx, cm, args, cmdCfg.streamOpts...)
		cli.ExitOnError(err)
		defer stream.Close()

		// Start stream
		err = stream.Start(rootCtx)
		cli.ExitOnError(err)

		// output the logs
		printLogs(rootCtx, cmd, cmdCfg, stream)

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
func getTableWriterHeaders(cmdCfg *logsCmdConfig, sources []logs.LogSource) ([]string, []int) {
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

	for _, col := range cmdCfg.columns {
		switch col {
		case "timestamp":
			headers = append(headers, "TIMESTAMP")
			colWidths = append(colWidths, 30)
		case "dot":
			headers = append(headers, "\u25CB")
			colWidths = append(colWidths, 1)
		case "node":
			headers = append(headers, "NODE")
			colWidths = append(colWidths, maxNodeLen)
		case "region":
			headers = append(headers, "REGION")
			colWidths = append(colWidths, maxRegionLen)
		case "zone":
			headers = append(headers, "ZONE")
			colWidths = append(colWidths, maxZoneLen)
		case "os":
			headers = append(headers, "OS")
			colWidths = append(colWidths, maxOSLen)
		case "arch":
			headers = append(headers, "ARCH")
			colWidths = append(colWidths, maxArchLen)
		case "namespace":
			headers = append(headers, "NAMESPACE")
			colWidths = append(colWidths, maxNamespaceLen)
		case "pod":
			headers = append(headers, "POD")
			colWidths = append(colWidths, maxPodLen)
		case "container":
			headers = append(headers, "CONTAINER")
			colWidths = append(colWidths, maxContainerLen)
		}
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

func addLogsCmdFlags(cmd *cobra.Command) {
	flagset := cmd.Flags()
	flagset.SortFlags = false

	flagset.String(KubeContextFlag, "", "Specify the kubeconfig context to use")
	flagset.VarP(&headFlag, "head", "h", "Return last N records (default 10)")
	flagset.Lookup("head").NoOptDefVal = "N"
	flagset.VarP(&tailFlag, "tail", "t", "Return last N records (default 10)")
	flagset.Lookup("tail").NoOptDefVal = "N"
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
	flagset.StringSlice("columns", []string{}, "Set output columns (timestamp,dot,node,region,zone,os,arch,namespace,pod,container)")
	flagset.StringSlice("add-columns", []string{}, "Add output columns (timestamp,dot,node,region,zone,os,arch,namespace,pod,container)")
	flagset.StringSlice("remove-columns", []string{}, "Remove output columns (timestamp,dot,node,region,zone,os,arch,namespace,pod,container)")
	flagset.Bool("with-cursors", false, "Show paging cursors")

	flagset.Bool("hide-header", false, "Hide table header")
	flagset.Bool("all-containers", false, "Show logs from all containers in a Pod")

	//flagset.BoolP("reverse", "r", false, "List records in reverse order")

	flagset.Bool("force", false, "Force command (if necessary)")

	// Define help here to avoid re-defining 'h' shorthand
	flagset.Bool("help", false, "help for logs")

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
	addLogsCmdFlags(logsCmd)
}
