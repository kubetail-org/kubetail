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
	"fmt"
	"strings"
	"time"

	"github.com/sosodev/duration"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// creates log source paths based on namespace, pod, and container criteria.
func buildSourcePaths(namespace, pod, container string, cm k8shelpers.ConnectionManager, kubeContext string) ([]string, error) {
	var sourcePaths []string

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

// parseTimeArgMCP parses time arguments for the MCP server
func parseTimeArgMCP(arg string) (time.Time, error) {
	var zero time.Time

	arg = strings.TrimSpace(arg)
	if arg == "" {
		return zero, nil
	} else if timeAgo, err := duration.Parse(arg); err == nil {
		return time.Now().Add(-1 * timeAgo.ToTimeDuration()), nil
	} else if ts, err := time.Parse(time.RFC3339Nano, arg); err == nil {
		return ts, nil
	}

	return zero, fmt.Errorf("unable to parse arg %s", arg)
}

// infer log level from message
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

// extract metadata from log messages for analysis.
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
