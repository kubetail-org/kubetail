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
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

const mcpServerHelp = `
Start an MCP server that exposes Kubernetes log search functionality to AI tools.

This command starts a Model Context Protocol (MCP) server that enables AI tools
like Claude Desktop to search and retrieve Kubernetes logs through
natural language queries.

The server exposes a 'kubernetes_logs_search' tool that can:
- Search logs across pods and namespaces
- Filter logs with grep patterns  
- Query logs using natural language
- Filter by infrastructure attributes (region, zone, architecture)

Example queries an AI tool can make:
- "Show me error logs from nginx pods"
- "Find API timeout logs in the production namespace"
- "Get recent logs from the frontend deployment"

Usage with Claude Desktop:
Add this to your claude_desktop_config.json:
{
  "mcpServers": {
    "kubetail": {
      "command": "kubetail",
      "args": ["mcp-server"]
    }
  }
}
`

// mcpServerCmd represents the mcp-server command
var mcpServerCmd = &cobra.Command{
	Use:   "mcp-server",
	Short: "Start MCP server for AI tool integration",
	Long:  strings.ReplaceAll(mcpServerHelp, "\t", "  "),
	Run: func(cmd *cobra.Command, args []string) {
		kubeContext, _ := cmd.Flags().GetString(KubeContextFlag)
		kubeconfigPath, _ := cmd.Flags().GetString(KubeconfigFlag)

		log.Info().Msg("Starting Kubetail MCP Server...")

		s := server.NewMCPServer(
			"Kubetail MCP Server",
			"1.0.0",
			server.WithToolCapabilities(false),
		)

		// connection manager
		cm, err := k8shelpers.NewDesktopConnectionManager(k8shelpers.WithKubeconfigPath(kubeconfigPath), k8shelpers.WithLazyConnect(true))
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to create connection manager")
		}

		// Register the tool
		tool := mcp.NewTool("kubernetes_logs_search",
			mcp.WithDescription("Search and retrieve Kubernetes logs with advanced filtering capabilities. Unlike generic K8s log access, this provides server-side filtering by region, zone, architecture, and more."),

			// Required parameter
			mcp.WithString("query",
				mcp.Required(),
				mcp.Description("Natural language query for logs (e.g. 'nginx errors', 'API timeouts in production')"),
			),

			// Basic Kubernetes filters
			mcp.WithString("namespace",
				mcp.Description("Kubernetes namespace to search (optional)"),
			),
			mcp.WithString("pod",
				mcp.Description("Specific pod name or pattern to search (optional, supports wildcards)"),
			),
			mcp.WithString("container",
				mcp.Description("Specific container name to search (optional)"),
			),

			// Advanced infrastructure filters
			mcp.WithString("region",
				mcp.Description("Filter by cloud region - comma-separated (e.g. 'us-east-1,us-west-2')"),
			),
			mcp.WithString("zone",
				mcp.Description("Filter by availability zone - comma-separated (e.g. 'us-east-1a,us-east-1b')"),
			),
			mcp.WithString("arch",
				mcp.Description("Filter by CPU architecture - comma-separated (e.g. 'amd64,arm64')"),
			),
			mcp.WithString("os",
				mcp.Description("Filter by operating system - comma-separated (e.g. 'linux,windows')"),
			),
			mcp.WithString("node",
				mcp.Description("Filter by specific node names - comma-separated"),
			),

			// Content filtering
			mcp.WithString("grep",
				mcp.Description("Server-side regex pattern to filter log content (processed before download)"),
			),

			// Time-based filtering
			mcp.WithString("since",
				mcp.Description("Show logs since this time (ISO timestamp like '2023-01-01T00:00:00Z' or duration like 'PT30M')"),
			),
			mcp.WithString("until",
				mcp.Description("Show logs until this time (ISO timestamp or duration)"),
			),

			// Output control
			mcp.WithNumber("tail",
				mcp.Description("Show last N log entries (default: 100)"),
			),
			mcp.WithBoolean("follow",
				mcp.Description("Stream new log entries in real-time"),
			),
			mcp.WithString("format",
				mcp.Description("Response format: 'detailed' (default - structured JSON for AI analysis), 'summary' (human-friendly overview), 'raw' (traditional log lines)"),
			),
		)

		s.AddTool(tool, handleKubernetesLogSearchRequest(cm, kubeContext))

		log.Info().Msg("MCP Server initialized, starting stdio transport...")

		// Start the server with stdio transport
		if err := server.ServeStdio(s); err != nil {
			log.Fatal().Err(err).Msg("MCP Server error")
		}
	},
}

// creates a handler function for the kubernetes_logs_search MCP tool
func handleKubernetesLogSearchRequest(cm k8shelpers.ConnectionManager, kubeContext string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		log.Info().Str("tool", "kubernetes_logs_search").Msg("Processing log search request")

		args, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		query, ok := args["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query parameter is required"), nil
		}

		// Kubernetes filters
		namespace, _ := args["namespace"].(string)
		pod, _ := args["pod"].(string)
		container, _ := args["container"].(string)

		// Infrastructure filters
		region, _ := args["region"].(string)
		zone, _ := args["zone"].(string)
		arch, _ := args["arch"].(string)
		osFilter, _ := args["os"].(string)
		node, _ := args["node"].(string)

		// Content and time filters
		grep, _ := args["grep"].(string)
		since, _ := args["since"].(string)
		until, _ := args["until"].(string)

		// Output control
		tail, _ := args["tail"].(float64)
		follow, _ := args["follow"].(bool)
		format, _ := args["format"].(string)

		// defaults
		if tail == 0 {
			tail = 30 // default
		}
		if format == "" {
			format = "detailed" // Default to structured JSON for AI consumption
		}

		sourcePaths, err := buildSourcePaths(namespace, pod, container, cm, kubeContext)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to build source paths: %v", err)), nil
		}

		if len(sourcePaths) == 0 {
			return mcp.NewToolResultText("No matching pods found for the specified criteria"), nil
		}

		streamOpts := []logs.Option{
			logs.WithKubeContext(kubeContext),
			logs.WithTail(int64(tail)),
			logs.WithFollow(follow),
		}

		if region != "" {
			regionList := parseCommaList(region)
			streamOpts = append(streamOpts, logs.WithRegions(regionList))
		}
		if zone != "" {
			zoneList := parseCommaList(zone)
			streamOpts = append(streamOpts, logs.WithZones(zoneList))
		}
		if arch != "" {
			archList := parseCommaList(arch)
			streamOpts = append(streamOpts, logs.WithArches(archList))
		}
		if osFilter != "" {
			osList := parseCommaList(osFilter)
			streamOpts = append(streamOpts, logs.WithOSes(osList))
		}
		if node != "" {
			nodeList := parseCommaList(node)
			streamOpts = append(streamOpts, logs.WithNodes(nodeList))
		}

		if grep != "" {
			streamOpts = append(streamOpts, logs.WithGrep(grep))
		}

		if since != "" {
			sinceTime, err := parseTimeArgMCP(since)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid since time: %v", err)), nil
			}
			streamOpts = append(streamOpts, logs.WithSince(sinceTime))
		}
		if until != "" {
			untilTime, err := parseTimeArgMCP(until)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Invalid until time: %v", err)), nil
			}
			streamOpts = append(streamOpts, logs.WithUntil(untilTime))
		}

		// Initialize SmartLogFetcher
		logFetcher, logFetcherType, err := createSmartLogFetcher(cm, kubeContext)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create log fetcher: %s", err)), nil
		}
		streamOpts = append(streamOpts, logs.WithLogFetcher(logFetcher))

		startTime := time.Now()
		stream, err := logs.NewStream(ctx, cm, sourcePaths, streamOpts...)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to create log stream: %v", err)), nil
		}
		defer stream.Close()

		if err := stream.Start(ctx); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to start log stream: %v", err)), nil
		}

		processingTime := time.Since(startTime).Milliseconds()

		appliedFilters := AppliedFilters{
			Namespace: namespace,
			Pod:       pod,
			Container: container,
			Grep:      grep,
			Since:     since,
			Until:     until,
			Tail:      int64(tail),
		}

		if region != "" {
			appliedFilters.Region = parseCommaList(region)
		}
		if zone != "" {
			appliedFilters.Zone = parseCommaList(zone)
		}
		if arch != "" {
			appliedFilters.Arch = parseCommaList(arch)
		}
		if osFilter != "" {
			appliedFilters.OS = parseCommaList(osFilter)
		}
		if node != "" {
			appliedFilters.Node = parseCommaList(node)
		}

		processingInfo := ProcessingInfo{
			LogFetcher:       logFetcherType,
			ServerSideGrep:   grep != "" && logFetcherType == "AgentLogFetcher",
			ProcessingTimeMs: processingTime,
			SourcesScanned:   len(sourcePaths),
		}

		// Handle streaming (follow) requests
		if follow {
			return handleStreamingRequest(ctx, stream, query, sourcePaths, streamOpts, since,
				appliedFilters, processingInfo)
		}

		// For non-streaming requests, collect all records
		var records []logs.LogRecord
		for record := range stream.Records() {
			records = append(records, record)
		}

		if stream.Err() != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Error while streaming logs: %v", stream.Err())), nil
		}

		response := buildStructuredResponse(query, records, appliedFilters, processingInfo)

		if len(records) == 0 {
			return mcp.NewToolResultText("No log entries found matching the specified criteria"), nil
		}

		return formatResponse(response, format, records)
	}
}

// handles follow=true requests with a timeout-based approach
func handleStreamingRequest(ctx context.Context, stream *logs.Stream, query string,
	sourcePaths []string, streamOpts []logs.Option, since string,
	appliedFilters AppliedFilters, processingInfo ProcessingInfo) (*mcp.CallToolResult, error) {

	var streamingSince time.Time

	// since parameter was provided
	if since != "" {
		var err error
		streamingSince, err = parseTimeArgMCP(since)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid since time for streaming: %v", err)), nil
		}
	} else {
		// no since parameter, use current time, start collecting from 5 seconds ago
		streamingSince = time.Now().Add(-5 * time.Second)
	}

	hasSinceOption := false
	for i, opt := range streamOpts {
		// check the option, if it's a WithSince
		if optStr := fmt.Sprintf("%T", opt); strings.Contains(optStr, "WithSince") {
			streamOpts[i] = logs.WithSince(streamingSince)
			hasSinceOption = true
			break
		}
	}

	// add since option if not found
	if !hasSinceOption {
		streamOpts = append(streamOpts, logs.WithSince(streamingSince))
	}

	var initialRecords []logs.LogRecord
	recordCount := 0
	recordLimit := 20 // initial batch size

	timeout := time.After(2 * time.Second)
	collectDone := false

	for !collectDone {
		select {
		case record, ok := <-stream.Records():
			if !ok {
				collectDone = true
				break
			}
			initialRecords = append(initialRecords, record)
			recordCount++
			if recordCount >= recordLimit {
				collectDone = true
			}
		case <-timeout:
			collectDone = true
		case <-ctx.Done():
			return mcp.NewToolResultError("Request cancelled"), nil
		}
	}

	// get latest timestamp from logs for next polling request
	var latestTimestamp time.Time
	if len(initialRecords) > 0 {
		for _, record := range initialRecords {
			if record.Timestamp.After(latestTimestamp) {
				latestTimestamp = record.Timestamp
			}
		}
	} else {
		latestTimestamp = time.Now()
	}

	// 1 millisecond increment to prevent duplicate logs in next poll
	nextPollTime := latestTimestamp.Add(time.Millisecond)

	response := buildStructuredResponse(query, initialRecords, appliedFilters, processingInfo)

	// meta info
	if response.InfraInsights.AppliedFilters == nil {
		response.InfraInsights.AppliedFilters = []string{}
	}
	response.InfraInsights.AppliedFilters = append(
		response.InfraInsights.AppliedFilters,
		"Real-time log streaming requires repeated calls with the same parameters")

	if len(initialRecords) == 0 {
		response.Summary.TotalEntries = 0
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize response: %v", err)), nil
		}

		// Use current time as the next poll point
		nextPollStr := time.Now().Format(time.RFC3339)
		return mcp.NewToolResultText(fmt.Sprintf("No log entries found. For continuous updates, call again with: since=\"%s\"\n\n%s",
			nextPollStr, string(jsonData))), nil
	}

	jsonData, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize response: %v", err)), nil
	}

	nextPollStr := nextPollTime.Format(time.RFC3339)

	return mcp.NewToolResultText(fmt.Sprintf("Found %d log entries. For continuous updates, call again with: since=\"%s\"\n\n%s",
		len(initialRecords), nextPollStr, string(jsonData))), nil
}


func formatResponse(response *LogSearchResponse, format string, records []logs.LogRecord) (*mcp.CallToolResult, error) {
	switch format {
	case "raw":
		var logLines []string
		for _, record := range records {
			logLine := fmt.Sprintf("[%s] %s/%s/%s: %s",
				record.Timestamp.Format("2006-01-02 15:04:05"),
				record.Source.Namespace,
				record.Source.PodName,
				record.Source.ContainerName,
				record.Message)
			logLines = append(logLines, logLine)
		}
		result := fmt.Sprintf("Found %d log entries:\n\n%s", len(logLines), strings.Join(logLines, "\n"))
		return mcp.NewToolResultText(result), nil

	case "detailed":
		jsonData, err := json.MarshalIndent(response, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize structured response: %v", err)), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("Structured log data (JSON):\n\n%s", string(jsonData))), nil

	default: // "summary"
		return formatSummaryResponse(response), nil
	}
}

func formatSummaryResponse(response *LogSearchResponse) *mcp.CallToolResult {
	var fetcherInfo string
	if response.Processing.LogFetcher == "AgentLogFetcher" {
		fetcherInfo = "⚡ AgentLogFetcher (Server-side processing)"
		if response.Processing.ServerSideGrep {
			fetcherInfo += " with server-side filtering"
		}
	} else {
		fetcherInfo = "KubeLogFetcher (Kubernetes API - client-side processing)"
	}

	var summaryText strings.Builder

	fmt.Fprintf(&summaryText, "Logs for query: \"%s\" (Found %d entries)\n\n",
		response.Query, response.Summary.TotalEntries)

	fmt.Fprintf(&summaryText, "**Log Entries:**\n")

	// prioritize error and warning messages first
	var errorLogs, warningLogs, otherLogs []LogEntryResponse
	for _, entry := range response.Entries {
		if entry.Level == "error" {
			errorLogs = append(errorLogs, entry)
		} else if entry.Level == "warning" {
			warningLogs = append(warningLogs, entry)
		} else {
			otherLogs = append(otherLogs, entry)
		}
	}

	// limit total entries to display 10 entries
	remainingEntries := 10

	// errors first
	if len(errorLogs) > 0 {
		fmt.Fprintf(&summaryText, "\n **ERROR LOGS:**\n")
		for _, entry := range errorLogs {
			if remainingEntries <= 0 {
				break
			}
			fmt.Fprintf(&summaryText, "  • [%s] %s/%s (%s):\n    %s\n\n",
				entry.Timestamp,
				entry.Source.Namespace,
				entry.Source.PodName,
				entry.Source.ContainerName,
				entry.Message)
			remainingEntries--
		}
	}

	// warnings
	if len(warningLogs) > 0 && remainingEntries > 0 {
		fmt.Fprintf(&summaryText, "\n **WARNING LOGS:**\n")
		for _, entry := range warningLogs {
			if remainingEntries <= 0 {
				break
			}
			fmt.Fprintf(&summaryText, "  • [%s] %s/%s (%s):\n    %s\n\n",
				entry.Timestamp,
				entry.Source.Namespace,
				entry.Source.PodName,
				entry.Source.ContainerName,
				entry.Message)
			remainingEntries--
		}
	}

	// other logs
	if len(otherLogs) > 0 && remainingEntries > 0 {
		fmt.Fprintf(&summaryText, "\n **INFO LOGS:**\n")
		for _, entry := range otherLogs {
			if remainingEntries <= 0 {
				break
			}
			fmt.Fprintf(&summaryText, "  • [%s] %s/%s (%s):\n    %s\n\n",
				entry.Timestamp,
				entry.Source.Namespace,
				entry.Source.PodName,
				entry.Source.ContainerName,
				entry.Message)
			remainingEntries--
		}
	}

	// indicate there are more entries
	if len(response.Entries) > 10 {
		fmt.Fprintf(&summaryText, "\n Showing 10 of %d entries. Use format='raw' to see all logs.\n",
			len(response.Entries))
	}

	// Display patterns and insights from log analysis
	fmt.Fprintf(&summaryText, "\n **Log Analysis:**\n")

	// insights about log patterns
	if response.Summary.ErrorCount > 0 {
		fmt.Fprintf(&summaryText, "• Found %d error messages - potential issues detected\n",
			response.Summary.ErrorCount)
	} else {
		fmt.Fprintf(&summaryText, "• No errors detected in logs\n")
	}

	if response.Summary.WarningCount > 0 {
		fmt.Fprintf(&summaryText, "• Found %d warning messages\n",
			response.Summary.WarningCount)
	}

	fmt.Fprintf(&summaryText, "• Timespan: %s to %s\n",
		response.Summary.TimeRange.Start,
		response.Summary.TimeRange.End)

	// show top keywords
	if len(response.Summary.TopKeywords) > 0 {
		fmt.Fprintf(&summaryText, "• Key topics: %s\n",
			strings.Join(response.Summary.TopKeywords, ", "))
	}

	// source information
	fmt.Fprintf(&summaryText, "\n**Sources:**\n")

	// List all namespaces
	if len(response.Summary.SourceSummary.Namespaces) > 0 {
		fmt.Fprintf(&summaryText, "• Namespaces: %s\n",
			strings.Join(response.Summary.SourceSummary.Namespaces, ", "))
	}

	// List all pods
	if len(response.Summary.SourceSummary.Pods) > 0 {
		if len(response.Summary.SourceSummary.Pods) <= 5 {
			fmt.Fprintf(&summaryText, "• Pods: %s\n",
				strings.Join(response.Summary.SourceSummary.Pods, ", "))
		} else {
			fmt.Fprintf(&summaryText, "• Pods: %d pods including %s\n",
				len(response.Summary.SourceSummary.Pods),
				strings.Join(response.Summary.SourceSummary.Pods[:3], ", "))
		}
	}

	// other meta info
	fmt.Fprintf(&summaryText, "\n **Technical Details:**\n")
	fmt.Fprintf(&summaryText, "• Data source: %s\n", fetcherInfo)
	fmt.Fprintf(&summaryText, "• Processing time: %dms\n", response.Processing.ProcessingTimeMs)
	fmt.Fprintf(&summaryText, "• Sources scanned: %d\n", response.Processing.SourcesScanned)

	if response.InfraInsights.InfrastructureSpan != "" {
		fmt.Fprintf(&summaryText, "• Infrastructure: %s\n",
			response.InfraInsights.InfrastructureSpan)
	}

	// applied filters
	if response.Filters.Namespace != "" || response.Filters.Pod != "" ||
		response.Filters.Container != "" || response.Filters.Grep != "" ||
		len(response.Filters.Region) > 0 || len(response.Filters.Zone) > 0 ||
		len(response.Filters.Node) > 0 || response.Filters.Since != "" ||
		response.Filters.Until != "" {

		fmt.Fprintf(&summaryText, "\n**Applied Filters:**\n")

		if response.Filters.Namespace != "" {
			fmt.Fprintf(&summaryText, "• Namespace: %s\n", response.Filters.Namespace)
		}
		if response.Filters.Pod != "" {
			fmt.Fprintf(&summaryText, "• Pod: %s\n", response.Filters.Pod)
		}
		if response.Filters.Container != "" {
			fmt.Fprintf(&summaryText, "• Container: %s\n", response.Filters.Container)
		}
		if response.Filters.Grep != "" {
			fmt.Fprintf(&summaryText, "• Grep pattern: %s\n", response.Filters.Grep)
		}
		if len(response.Filters.Region) > 0 {
			fmt.Fprintf(&summaryText, "• Regions: %s\n", strings.Join(response.Filters.Region, ", "))
		}
		if len(response.Filters.Zone) > 0 {
			fmt.Fprintf(&summaryText, "• Zones: %s\n", strings.Join(response.Filters.Zone, ", "))
		}
		if len(response.Filters.Node) > 0 {
			fmt.Fprintf(&summaryText, "• Nodes: %s\n", strings.Join(response.Filters.Node, ", "))
		}
		if response.Filters.Since != "" {
			fmt.Fprintf(&summaryText, "• Since: %s\n", response.Filters.Since)
		}
		if response.Filters.Until != "" {
			fmt.Fprintf(&summaryText, "• Until: %s\n", response.Filters.Until)
		}
	}

	fmt.Fprintf(&summaryText, "\n **Note:** Use format='detailed' for full JSON data or format='raw' for traditional log lines.\n\n")

	return mcp.NewToolResultText(summaryText.String())
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)

	mcpServerCmd.Flags().String(KubeconfigFlag, "", "Path to kubeconfig file")
	mcpServerCmd.Flags().String(KubeContextFlag, "", "Kubernetes context to use")
}
