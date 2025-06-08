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
	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// arguments for the kubernetes_logs_search MCP tool
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
