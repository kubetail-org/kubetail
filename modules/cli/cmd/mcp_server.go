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
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog/log"
	"github.com/sosodev/duration"
	"github.com/spf13/cobra"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/proxy"
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

// KubernetesLogsSearchArgs represents the arguments for the kubernetes_logs_search MCP tool
type KubernetesLogsSearchArgs struct {
	// Natural language query (required)
	Query string `json:"query" jsonschema:"required,description=Natural language query for logs (e.g. 'nginx errors', 'API timeouts in production')"`

	// Basic Kubernetes filters
	Namespace string `json:"namespace,omitempty" jsonschema:"description=Kubernetes namespace to search (optional, will search default if not specified)"`
	Pod       string `json:"pod,omitempty" jsonschema:"description=Specific pod name or pattern to search (optional, supports wildcards like 'nginx-*')"`
	Container string `json:"container,omitempty" jsonschema:"description=Specific container name to search (optional)"`

	// Advanced infrastructure filters
	Region string `json:"region,omitempty" jsonschema:"description=Filter by cloud region (e.g. 'us-east-1,us-west-2')"`
	Zone   string `json:"zone,omitempty" jsonschema:"description=Filter by availability zone (e.g. 'us-east-1a,us-east-1b')"`
	Arch   string `json:"arch,omitempty" jsonschema:"description=Filter by CPU architecture (e.g. 'amd64,arm64')"`
	OS     string `json:"os,omitempty" jsonschema:"description=Filter by operating system (e.g. 'linux,windows')"`
	Node   string `json:"node,omitempty" jsonschema:"description=Filter by specific node names"`

	// Content filtering
	Grep string `json:"grep,omitempty" jsonschema:"description=Text pattern to grep for in log lines (regex supported, processed server-side)"`

	// Time-based filtering
	Since string `json:"since,omitempty" jsonschema:"description=Show logs since this time (ISO timestamp or duration like 'PT30M')"`
	Until string `json:"until,omitempty" jsonschema:"description=Show logs until this time (ISO timestamp or duration like 'PT10M')"`

	// Output control
	Tail   int64 `json:"tail,omitempty" jsonschema:"description=Show last N log entries (default: 100)"`
	Follow bool  `json:"follow,omitempty" jsonschema:"description=Stream new log entries in real-time"`
}

// Response structures for AI tool consumption
type LogEntryResponse struct {
	Timestamp string            `json:"timestamp"`
	Message   string            `json:"message"`
	Source    logs.LogSource    `json:"source"`
	Level     string            `json:"level,omitempty"`    // Detected log level
	Tags      []string          `json:"tags,omitempty"`     // Detected tags/labels
	Metadata  map[string]string `json:"metadata,omitempty"` // Additional context
}

type LogSearchResponse struct {
	Query         string                 `json:"query"`
	Summary       LogSummary             `json:"summary"`
	Entries       []LogEntryResponse     `json:"entries"`
	Filters       AppliedFilters         `json:"filters"`
	Processing    ProcessingInfo         `json:"processing"`
	InfraInsights InfrastructureInsights `json:"infrastructure_insights"`
}

type LogSummary struct {
	TotalEntries  int            `json:"total_entries"`
	TimeRange     TimeRange      `json:"time_range"`
	SourceSummary SourceSummary  `json:"sources"`
	LevelCounts   map[string]int `json:"level_counts,omitempty"`
	TopKeywords   []string       `json:"top_keywords,omitempty"`
	ErrorCount    int            `json:"error_count,omitempty"`
	WarningCount  int            `json:"warning_count,omitempty"`
}

type TimeRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type SourceSummary struct {
	Namespaces []string `json:"namespaces"`
	Pods       []string `json:"pods"`
	Containers []string `json:"containers"`
}

type AppliedFilters struct {
	Namespace string   `json:"namespace,omitempty"`
	Pod       string   `json:"pod,omitempty"`
	Container string   `json:"container,omitempty"`
	Region    []string `json:"region,omitempty"`
	Zone      []string `json:"zone,omitempty"`
	Arch      []string `json:"arch,omitempty"`
	OS        []string `json:"os,omitempty"`
	Node      []string `json:"node,omitempty"`
	Grep      string   `json:"grep,omitempty"`
	Since     string   `json:"since,omitempty"`
	Until     string   `json:"until,omitempty"`
	Tail      int64    `json:"tail,omitempty"`
}

type ProcessingInfo struct {
	LogFetcher       string `json:"log_fetcher"`        // "AgentLogFetcher" or "KubeLogFetcher"
	ServerSideGrep   bool   `json:"server_side_grep"`   // Whether grep was processed server-side
	ProcessingTimeMs int64  `json:"processing_time_ms"` // How long the query took
	SourcesScanned   int    `json:"sources_scanned"`    // Number of log sources checked
}

// InfrastructureInsights captures Kubetail's infrastructure-aware capabilities
type InfrastructureInsights struct {
	AdvancedFiltering   bool     `json:"advanced_filtering"`    // Whether infrastructure-based filters were used (cloud region/os/ cpu architecture)
	ServerSideFiltering bool     `json:"server_side_filtering"` // Whether filtering happened on agents vs client
	ProcessingMethod    string   `json:"processing_method"`     // Description of log processing approach
	AppliedFilters      []string `json:"applied_filters"`       // List of applied filters
	InfrastructureSpan  string   `json:"infrastructure_span"`   // Description of infrastructure coverage
}

// ClusterAPIProxyLogFetcher implements LogFetcher using Kubernetes service proxy to reach Cluster API
// This provides automatic service discovery without requiring manual URL configuration
type ClusterAPIProxyLogFetcher struct {
	cm          k8shelpers.ConnectionManager
	kubeContext string
	namespace   string
	serviceName string
	client      *http.Client
}

// NewClusterAPIProxyLogFetcher initializes a ClusterAPIProxyLogFetcher for accessing the cluster API service via Kubernetes proxy.
func NewClusterAPIProxyLogFetcher(cm k8shelpers.ConnectionManager, kubeContext string) (*ClusterAPIProxyLogFetcher, error) {
	// Try to find kubetail-cluster-api service in kubetail-system namespace first
	namespace := "kubetail-system"
	serviceName := "kubetail-cluster-api"

	if clientset, err := cm.GetOrCreateClientset(kubeContext); err == nil {
		// Check if the cluster API service exists in kubetail-system
		if _, err := clientset.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{}); err != nil {
			log.Debug().Str("namespace", namespace).Str("service", serviceName).Msg("kubetail-cluster-api service not found in kubetail-system, trying default namespace")
			namespace = "default"
			// Check if it exists in default namespace
			if _, err := clientset.CoreV1().Services(namespace).Get(context.TODO(), serviceName, metav1.GetOptions{}); err != nil {
				return nil, fmt.Errorf("kubetail-cluster-api service not found in %s or %s namespaces", "kubetail-system", "default")
			}
		}
	}

	log.Debug().Str("namespace", namespace).Str("service", serviceName).Msg("Found kubetail-cluster-api service")

	return &ClusterAPIProxyLogFetcher{
		cm:          cm,
		kubeContext: kubeContext,
		namespace:   namespace,
		serviceName: serviceName,
		client:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// StreamForward returns a channel of LogRecords in chronological order
func (f *ClusterAPIProxyLogFetcher) StreamForward(ctx context.Context, source logs.LogSource, opts logs.FetcherOptions) (<-chan logs.LogRecord, error) {
	return f.streamLogs(ctx, source, opts, false) // false = forward direction
}

// StreamBackward returns a channel of LogRecords in reverse chronological order
func (f *ClusterAPIProxyLogFetcher) StreamBackward(ctx context.Context, source logs.LogSource, opts logs.FetcherOptions) (<-chan logs.LogRecord, error) {
	return f.streamLogs(ctx, source, opts, true) // true = backward direction
}

// streamLogs fetches log records from the cluster API proxy using a GraphQL query and streams them to a channel.
func (f *ClusterAPIProxyLogFetcher) streamLogs(ctx context.Context, source logs.LogSource, opts logs.FetcherOptions, backward bool) (<-chan logs.LogRecord, error) {

	outCh := make(chan logs.LogRecord)

	go func() {
		defer close(outCh)

		handler, token, err := f.getKubernetesServiceProxyHandler(ctx)
		if err != nil {
			log.Debug().Err(err).Msg("Could not create Kubernetes service proxy handler")
			return
		}

		query := f.buildLogRecordsQuery(source, opts, backward, 100)

		reqBody := fmt.Sprintf(`{"query": %s}`, strconv.Quote(query))

		proxyPath := fmt.Sprintf("/api/v1/namespaces/%s/services/%s:http/proxy/graphql", f.namespace, f.serviceName)
		req, err := http.NewRequestWithContext(ctx, "POST", proxyPath, strings.NewReader(reqBody))
		if err != nil {
			log.Debug().Err(err).Msg("Could not create proxy request for log records")
			return
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-Authorization", fmt.Sprintf("Bearer %s", token))

		// Execute request through proxy
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != 200 {
			log.Debug().Int("status", rec.Code).Msg("Cluster API proxy request returned error status")
			return
		}

		// Parse the GraphQL response
		var response struct {
			Data struct {
				LogRecordsFetch struct {
					Records []struct {
						Timestamp string `json:"timestamp"`
						Message   string `json:"message"`
					} `json:"records"`
				} `json:"logRecordsFetch"`
			} `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}

		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			log.Debug().Err(err).Msg("Could not parse GraphQL response from cluster API")
			return
		}

		// Check for GraphQL errors
		if len(response.Errors) > 0 {
			for _, gqlErr := range response.Errors {
				log.Debug().Str("error", gqlErr.Message).Msg("Cluster API GraphQL query error")
			}
			return
		}

		// Process log records
		for _, record := range response.Data.LogRecordsFetch.Records {
			// Parse timestamp
			timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
			if err != nil {
				// Fallback to RFC3339 if nano parsing fails
				timestamp, err = time.Parse(time.RFC3339, record.Timestamp)
				if err != nil {
					log.Debug().Err(err).Str("timestamp", record.Timestamp).Msg("Could not parse timestamp")
					timestamp = time.Now() // Use current time as fallback
				}
			}

			logRecord := logs.LogRecord{
				Message:   record.Message,
				Timestamp: timestamp,
				Source:    source,
			}

			select {
			case <-ctx.Done():
				return
			case outCh <- logRecord:
				// Successfully sent record
			}
		}
	}()

	return outCh, nil
}

// getKubernetesServiceProxyHandler returns an HTTP handler and token for proxying requests to the cluster API service.
func (f *ClusterAPIProxyLogFetcher) getKubernetesServiceProxyHandler(ctx context.Context) (http.Handler, string, error) {

	restConfig, err := f.cm.GetOrCreateRestConfig(f.kubeContext)
	if err != nil {
		return nil, "", err
	}

	// Create proxy handler using kubectl proxy functionality
	handler, err := proxy.NewProxyHandler("/", nil, restConfig, 0, false)
	if err != nil {
		return nil, "", err
	}

	// Get service account token for authentication
	clientset, err := f.cm.GetOrCreateClientset(f.kubeContext)
	if err != nil {
		return nil, "", err
	}

	// Try to create service account token for cluster API access
	// First try kubetail-mcp service account, fallback to default
	var token string
	var tokenErr error

	serviceAccounts := []string{"kubetail-cli", "kubetail-mcp", "default"}

	for _, sa := range serviceAccounts {
		sat, saErr := k8shelpers.NewServiceAccountToken(ctx, clientset, f.namespace, sa, nil)
		if saErr != nil {
			log.Debug().Err(saErr).Str("service_account", sa).Msg("Could not create service account token helper")
			continue
		}

		token, tokenErr = sat.Token(ctx)
		if tokenErr != nil {
			log.Debug().Err(tokenErr).Str("service_account", sa).Msg("Could not get token from service account")
			continue
		}

		log.Debug().Str("service_account", sa).Msg("Successfully obtained service account token")
		break
	}

	if token == "" || tokenErr != nil {
		return nil, "", fmt.Errorf("could not obtain service account token from any of %v: %w", serviceAccounts, tokenErr)
	}

	return handler, token, nil
}

// buildLogRecordsQuery constructs a GraphQL query string for fetching log records from the cluster API.
func (f *ClusterAPIProxyLogFetcher) buildLogRecordsQuery(source logs.LogSource, opts logs.FetcherOptions, backward bool, limit int) string {
	var queryParts []string

	// Build sources array as strings (like "namespace:pod/container")
	sourceStr := source.Namespace + ":"
	if source.PodName != "" {
		sourceStr += source.PodName
		if source.ContainerName != "" {
			sourceStr += "/" + source.ContainerName
		}
	} else {
		sourceStr += "*" // All pods if no specific pod
	}

	queryParts = append(queryParts, fmt.Sprintf(`sources: ["%s"]`, sourceStr))

	if backward {
		queryParts = append(queryParts, `mode: HEAD`)
	} else {
		queryParts = append(queryParts, `mode: TAIL`)
	}

	if opts.Grep != "" {
		queryParts = append(queryParts, fmt.Sprintf(`grep: "%s"`, opts.Grep))
	}

	if !opts.StartTime.IsZero() {
		queryParts = append(queryParts, fmt.Sprintf(`since: "%s"`, opts.StartTime.Format(time.RFC3339)))
	}

	if !opts.StopTime.IsZero() {
		queryParts = append(queryParts, fmt.Sprintf(`until: "%s"`, opts.StopTime.Format(time.RFC3339)))
	}

	queryParts = append(queryParts, fmt.Sprintf(`limit: %d`, limit))

	// Build the complete query
	queryArgs := strings.Join(queryParts, ", ")

	query := fmt.Sprintf(`
		query {
			logRecordsFetch(%s) {
				records {
					timestamp
					message
				}
			}
		}
	`, queryArgs)

	return query
}

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

		s.AddTool(tool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

			// Set defaults
			if tail == 0 {
				tail = 100 // default
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

			var records []logs.LogRecord
			for record := range stream.Records() {
				records = append(records, record)
			}

			if stream.Err() != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Error while streaming logs: %v", stream.Err())), nil
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

			response := buildStructuredResponse(query, records, appliedFilters, processingInfo)

			if len(records) == 0 {
				return mcp.NewToolResultText("No log entries found matching the specified criteria"), nil
			}

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
				// Full structured JSON for advanced AI analysis
				jsonData, err := json.MarshalIndent(response, "", "  ")
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Failed to serialize structured response: %v", err)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Structured log data (JSON):\n\n%s", string(jsonData))), nil

			default: // "summary"
				// AI-friendly summary with key insights
				var fetcherInfo string
				if response.Processing.LogFetcher == "AgentLogFetcher" {
					fetcherInfo = "⚡ AgentLogFetcher (Server-side processing)"
					if response.Processing.ServerSideGrep {
						fetcherInfo += " with server-side filtering"
					}
				} else {
					fetcherInfo = "🔄 KubeLogFetcher (Kubernetes API - client-side processing)"
				}

				summaryText := fmt.Sprintf(`Log Analysis Summary for: "%s"

📊 **Overview:**
- Found %d log entries
- Sources: %d namespaces, %d pods, %d containers  
- Time range: %s to %s
- Data source: %s
- Processing time: %dms

🔍 **Key Insights:**
- Error entries: %d
- Warning entries: %d  
- Top keywords: %v

📋 **Applied Filters:**`,
					query,
					response.Summary.TotalEntries,
					len(response.Summary.SourceSummary.Namespaces),
					len(response.Summary.SourceSummary.Pods),
					len(response.Summary.SourceSummary.Containers),
					response.Summary.TimeRange.Start,
					response.Summary.TimeRange.End,
					fetcherInfo,
					response.Processing.ProcessingTimeMs,
					response.Summary.ErrorCount,
					response.Summary.WarningCount,
					response.Summary.TopKeywords)

				if appliedFilters.Namespace != "" {
					summaryText += fmt.Sprintf("\n- Namespace: %s", appliedFilters.Namespace)
				}
				if appliedFilters.Pod != "" {
					summaryText += fmt.Sprintf("\n- Pod: %s", appliedFilters.Pod)
				}
				if appliedFilters.Grep != "" {
					summaryText += fmt.Sprintf("\n- Grep pattern: %s", appliedFilters.Grep)
				}
				if len(appliedFilters.Region) > 0 {
					summaryText += fmt.Sprintf("\n- Regions: %v", appliedFilters.Region)
				}

				if len(response.Entries) > 0 {
					summaryText += "\n\n📝 **Sample Entries:**"
					sampleCount := 3
					if len(response.Entries) < sampleCount {
						sampleCount = len(response.Entries)
					}

					for i := 0; i < sampleCount; i++ {
						entry := response.Entries[i]
						summaryText += fmt.Sprintf("\n[%s] %s/%s [%s]: %s",
							entry.Timestamp,
							entry.Source.Namespace,
							entry.Source.PodName,
							entry.Level,
							entry.Message)
					}

					if len(response.Entries) > sampleCount {
						summaryText += fmt.Sprintf("\n... and %d more entries", len(response.Entries)-sampleCount)
					}
				}

				summaryText += "\n\n💡 **Note:** Use format='detailed' for full structured data or format='raw' for traditional log lines."

				return mcp.NewToolResultText(summaryText), nil
			}
		})

		log.Info().Msg("MCP Server initialized, starting stdio transport...")

		// Start the server with stdio transport
		if err := server.ServeStdio(s); err != nil {
			log.Fatal().Err(err).Msg("MCP Server error")
		}
	},
}

// buildSourcePaths creates log source paths based on namespace, pod, and container criteria.
func buildSourcePaths(namespace, pod, container string, cm k8shelpers.ConnectionManager, kubeContext string) ([]string, error) {
	var sourcePaths []string

	// If no namespace specified, use default
	if namespace == "" {
		namespace = cm.GetDefaultNamespace(kubeContext)
	}

	var sourcePath string
	if pod != "" {
		if container != "" {
			sourcePath = fmt.Sprintf("%s:%s/%s", namespace, pod, container)
		} else {
			sourcePath = fmt.Sprintf("%s:%s", namespace, pod)
		}
	} else {
		if container != "" {
			sourcePath = fmt.Sprintf("%s:*/%s", namespace, container)
		} else {
			sourcePath = fmt.Sprintf("%s:*", namespace)
		}
	}

	sourcePaths = append(sourcePaths, sourcePath)
	return sourcePaths, nil
}

/* Utility functions */

// parseCommaList splits a comma-separated string into a slice, trimming whitespace.
func parseCommaList(input string) []string {
	if input == "" {
		return nil
	}

	var result []string
	for _, item := range strings.Split(input, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseTimeArgMCP parses time arguments for the MCP server, supporting ISO durations and timestamps.
func parseTimeArgMCP(arg string) (time.Time, error) {
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

// detectLogLevel attempts to infer log level from message content.
func detectLogLevel(message string) string {
	lower := strings.ToLower(message)

	if strings.Contains(lower, "error") || strings.Contains(lower, "err") || strings.Contains(lower, "fatal") {
		return "error"
	}
	if strings.Contains(lower, "warn") || strings.Contains(lower, "warning") {
		return "warning"
	}
	if strings.Contains(lower, "info") || strings.Contains(lower, "information") {
		return "info"
	}
	if strings.Contains(lower, "debug") || strings.Contains(lower, "trace") {
		return "debug"
	}

	// Check for HTTP status codes, 4xx/5xx status
	if strings.Contains(message, " 4") || strings.Contains(message, " 5") {
		return "error"
	}
	if strings.Contains(message, " 2") || strings.Contains(message, " 3") { // 2xx/3xx status
		return "info"
	}

	return "info" // Default level
}

// extractTags identifies relevant tags/labels from log messages for categorization.
func extractTags(message string) []string {
	var tags []string
	lower := strings.ToLower(message)

	// HTTP-related tags
	if strings.Contains(lower, "http") || strings.Contains(message, "GET") || strings.Contains(message, "POST") {
		tags = append(tags, "http")
	}

	// Database-related tags
	if strings.Contains(lower, "sql") || strings.Contains(lower, "database") || strings.Contains(lower, "db") {
		tags = append(tags, "database")
	}

	// Authentication/authorization
	if strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "permission") {
		tags = append(tags, "authentication")
	}

	// API-related
	if strings.Contains(lower, "api") || strings.Contains(lower, "/v1/") || strings.Contains(lower, "/v2/") {
		tags = append(tags, "api")
	}

	// Performance-related
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "slow") || strings.Contains(lower, "latency") {
		tags = append(tags, "performance")
	}

	// Network-related
	if strings.Contains(lower, "connection") || strings.Contains(lower, "network") || strings.Contains(lower, "tcp") {
		tags = append(tags, "network")
	}

	return tags
}

// extractLogMetadata extracts structured metadata from log messages for analysis.
func extractLogMetadata(message, level string) map[string]string {
	metadata := make(map[string]string)

	if strings.Contains(message, " 200 ") || strings.Contains(message, " 201 ") {
		metadata["http_status"] = "success"
		metadata["http_status_code"] = "2xx"
	} else if strings.Contains(message, " 4") && (strings.Contains(message, " 40") || strings.Contains(message, " 41")) {
		metadata["http_status"] = "client_error"
		metadata["http_status_code"] = "4xx"
	} else if strings.Contains(message, " 5") && strings.Contains(message, " 50") {
		metadata["http_status"] = "server_error"
		metadata["http_status_code"] = "5xx"
	}

	if strings.Contains(message, "GET ") {
		metadata["http_method"] = "GET"
	} else if strings.Contains(message, "POST ") {
		metadata["http_method"] = "POST"
	} else if strings.Contains(message, "PUT ") {
		metadata["http_method"] = "PUT"
	} else if strings.Contains(message, "DELETE ") {
		metadata["http_method"] = "DELETE"
	}

	if strings.Contains(message, ".") {
		words := strings.Fields(message)
		for _, word := range words {
			if strings.Count(word, ".") == 3 && len(word) >= 7 {
				metadata["client_ip"] = word
				break
			}
		}
	}

	if strings.Contains(message, "ms") || strings.Contains(message, "seconds") {
		metadata["contains_timing"] = "true"
	}

	if strings.Contains(message, "{") && strings.Contains(message, "}") {
		metadata["log_format"] = "json"
	} else if strings.Count(message, "|") > 2 || strings.Count(message, ",") > 3 {
		metadata["log_format"] = "structured"
	} else {
		metadata["log_format"] = "unstructured"
	}

	metadata["severity"] = level

	return metadata
}

// extractKeywords analyzes a slice of log messages and returns the most frequent, meaningful keywords.
// This is used to help summarize and surface the main topics or issues present in the logs,
// making it easier for both AI tools and human users to quickly understand the context of log search results.
func extractKeywords(messages []string) []string {
	wordCount := make(map[string]int)
	commonWords := map[string]bool{
		"the": true, "and": true, "or": true, "but": true, "in": true, "on": true, "at": true,
		"to": true, "for": true, "of": true, "with": true, "by": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "have": true, "has": true, "had": true,
		"do": true, "does": true, "did": true, "will": true, "would": true, "could": true, "should": true,
		"a": true, "an": true, "this": true, "that": true, "these": true, "those": true,
	}

	for _, message := range messages {
		words := strings.Fields(strings.ToLower(message))
		for _, word := range words {
			// remove punctuation
			cleaned := strings.Trim(word, ".,!?:;\"'()[]{}/-")

			// Skip common words and short words
			if len(cleaned) < 3 || commonWords[cleaned] {
				continue
			}

			wordCount[cleaned]++
		}
	}

	type wordFreq struct {
		word  string
		count int
	}

	var frequencies []wordFreq
	for word, count := range wordCount {
		if count > 1 {
			frequencies = append(frequencies, wordFreq{word, count})
		}
	}

	// Sort by frequency
	for i := 0; i < len(frequencies)-1; i++ {
		for j := i + 1; j < len(frequencies); j++ {
			if frequencies[j].count > frequencies[i].count {
				frequencies[i], frequencies[j] = frequencies[j], frequencies[i]
			}
		}
	}

	// top 5 keywords
	var keywords []string
	limit := 5
	if len(frequencies) < limit {
		limit = len(frequencies)
	}

	for i := 0; i < limit; i++ {
		keywords = append(keywords, frequencies[i].word)
	}

	return keywords
}

// buildStructuredResponse creates a structured response for log search results, including summary and insights.
func buildStructuredResponse(query string, records []logs.LogRecord, filters AppliedFilters, processingInfo ProcessingInfo) *LogSearchResponse {
	if len(records) == 0 {
		InfraInsights := buildInfrastructureInsights(filters, processingInfo)
		return &LogSearchResponse{
			Query: query,
			Summary: LogSummary{
				TotalEntries: 0,
			},
			Entries:       []LogEntryResponse{},
			Filters:       filters,
			Processing:    processingInfo,
			InfraInsights: InfraInsights,
		}
	}

	var entries []LogEntryResponse
	var messages []string
	var errorCount, warningCount int
	levelCounts := make(map[string]int)
	namespaces := make(map[string]bool)
	pods := make(map[string]bool)
	containers := make(map[string]bool)

	var earliestTime, latestTime time.Time

	for i, record := range records {
		level := detectLogLevel(record.Message)

		metadata := extractLogMetadata(record.Message, level)

		entry := LogEntryResponse{
			Timestamp: record.Timestamp.Format(time.RFC3339),
			Message:   record.Message,
			Source:    record.Source,
			Level:     level,
			Tags:      extractTags(record.Message),
			Metadata:  metadata,
		}

		entries = append(entries, entry)
		messages = append(messages, record.Message)

		levelCounts[level]++
		if level == "error" {
			errorCount++
		}
		if level == "warning" {
			warningCount++
		}

		namespaces[record.Source.Namespace] = true
		pods[record.Source.PodName] = true
		containers[record.Source.ContainerName] = true

		if i == 0 || record.Timestamp.Before(earliestTime) {
			earliestTime = record.Timestamp
		}
		if i == 0 || record.Timestamp.After(latestTime) {
			latestTime = record.Timestamp
		}
	}

	summary := LogSummary{
		TotalEntries: len(records),
		TimeRange: TimeRange{
			Start: earliestTime.Format(time.RFC3339),
			End:   latestTime.Format(time.RFC3339),
		},
		SourceSummary: SourceSummary{
			Namespaces: mapKeysToSlice(namespaces),
			Pods:       mapKeysToSlice(pods),
			Containers: mapKeysToSlice(containers),
		},
		LevelCounts:  levelCounts,
		TopKeywords:  extractKeywords(messages),
		ErrorCount:   errorCount,
		WarningCount: warningCount,
	}

	InfraInsights := buildInfrastructureInsights(filters, processingInfo)

	return &LogSearchResponse{
		Query:         query,
		Summary:       summary,
		Entries:       entries,
		Filters:       filters,
		Processing:    processingInfo,
		InfraInsights: InfraInsights,
	}
}

// mapKeysToSlice converts a map[string]bool to []string
func mapKeysToSlice(m map[string]bool) []string {
	var result []string
	for key := range m {
		result = append(result, key)
	}
	return result
}

// buildInfrastructureInsights summarizes Kubetail's infrastructure-aware capabilities for the response.
func buildInfrastructureInsights(filters AppliedFilters, processing ProcessingInfo) InfrastructureInsights {
	var appliedFiltersList []string
	var infrastructureSpan string
	var processingMethod string

	// Detect advanced infrastructure filtering
	advancedFiltering := len(filters.Region) > 0 || len(filters.Zone) > 0 ||
		len(filters.Arch) > 0 || len(filters.OS) > 0 || len(filters.Node) > 0

	// Determine server-side processing
	serverSideFiltering := processing.LogFetcher == "AgentLogFetcher"

	// Collect applied filters
	if len(filters.Region) > 0 {
		appliedFiltersList = append(appliedFiltersList, "Multi-region log aggregation")
		infrastructureSpan += fmt.Sprintf("Regions: %v ", filters.Region)
	}
	if len(filters.Zone) > 0 {
		appliedFiltersList = append(appliedFiltersList, "Availability zone filtering")
		infrastructureSpan += fmt.Sprintf("Zones: %v ", filters.Zone)
	}
	if len(filters.Arch) > 0 {
		appliedFiltersList = append(appliedFiltersList, "CPU architecture-aware filtering")
		infrastructureSpan += fmt.Sprintf("Architectures: %v ", filters.Arch)
	}
	if len(filters.OS) > 0 {
		appliedFiltersList = append(appliedFiltersList, "Operating system filtering")
		infrastructureSpan += fmt.Sprintf("OS: %v ", filters.OS)
	}
	if len(filters.Node) > 0 {
		appliedFiltersList = append(appliedFiltersList, "Node-specific targeting")
		infrastructureSpan += fmt.Sprintf("Nodes: %v ", filters.Node)
	}

	if serverSideFiltering {
		appliedFiltersList = append(appliedFiltersList, "Agent-based server-side log processing")
		processingMethod = "Logs filtered at source by cluster agents, reducing network transfer and improving query performance"
		if processing.ServerSideGrep {
			appliedFiltersList = append(appliedFiltersList, "Server-side regex filtering")
		}
	} else {
		processingMethod = "Direct Kubernetes API access with client-side processing"
	}

	if infrastructureSpan == "" {
		infrastructureSpan = "Single namespace/cluster scope"
	} else {
		infrastructureSpan = strings.TrimSpace(infrastructureSpan)
	}

	if filters.Grep != "" {
		appliedFiltersList = append(appliedFiltersList, "Advanced regex pattern matching")
	}

	appliedFiltersList = append(appliedFiltersList, "Real-time log streaming", "Multi-container correlation", "Structured metadata extraction")

	return InfrastructureInsights{
		AdvancedFiltering:   advancedFiltering,
		ServerSideFiltering: serverSideFiltering,
		ProcessingMethod:    processingMethod,
		AppliedFilters:      appliedFiltersList,
		InfrastructureSpan:  infrastructureSpan,
	}
}

// createSmartLogFetcher creates a LogFetcher with ClusterAPIProxy priority and graceful fallback
// This is Kubetail's key advantage: server-side processing when cluster agents are available
// Returns the fetcher and a string indicating which type was used ("AgentLogFetcher" or "KubeLogFetcher")
func createSmartLogFetcher(cm k8shelpers.ConnectionManager, kubeContext string) (logs.LogFetcher, string, error) {

	clientset, err := cm.GetOrCreateClientset(kubeContext)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Kubernetes clientset")
		return nil, "KubeLogFetcher", err
	}

	// Try to create AgentLogFetcher via proxy
	fetcher, fetcherType, err := tryCreateAgentLogFetcher(cm, kubeContext, clientset)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to create AgentLogFetcher, using KubeLogFetcher")
		return logs.NewKubeLogFetcher(clientset), "KubeLogFetcher", nil
	}

	log.Info().Msgf("Using %s for server-side log processing", fetcherType)
	return fetcher, fetcherType, nil
}

// tryCreateAgentLogFetcher attempts to create an ClusterAPIProxyLogFetcher (AgentLogFetcher), falling back KubeLogFetcher if unavailable.
func tryCreateAgentLogFetcher(cm k8shelpers.ConnectionManager, kubeContext string, clientset kubernetes.Interface) (logs.LogFetcher, string, error) {
	log.Debug().Msg("Using ClusterAPIProxyLogFetcher for accessing Kubetail agents")

	fetcher, err := NewClusterAPIProxyLogFetcher(cm, kubeContext)
	if err == nil {
		log.Debug().Msg("Successfully created ClusterAPIProxyLogFetcher for AgentLogFetcher access")
		return fetcher, "AgentLogFetcher", nil
	}
	log.Debug().Err(err).Msg("Could not create ClusterAPIProxyLogFetcher, falling back to KubeLogFetcher (Kubernetes API)")

	// Fallback to KubeLogFetcher
	return logs.NewKubeLogFetcher(clientset), "KubeLogFetcher", nil
}

func init() {
	rootCmd.AddCommand(mcpServerCmd)

	mcpServerCmd.Flags().String(KubeconfigFlag, "", "Path to kubeconfig file")
	mcpServerCmd.Flags().String(KubeContextFlag, "", "Kubernetes context to use")
}
