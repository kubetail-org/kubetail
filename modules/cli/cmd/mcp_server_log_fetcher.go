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

	"github.com/rs/zerolog/log"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/proxy"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

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
	return f.streamLogs(ctx, source, opts, false)
}

// StreamBackward returns a channel of LogRecords in reverse chronological order
func (f *ClusterAPIProxyLogFetcher) StreamBackward(ctx context.Context, source logs.LogSource, opts logs.FetcherOptions) (<-chan logs.LogRecord, error) {
	return f.streamLogs(ctx, source, opts, true)
}

// fetch log records from the cluster API proxy using a GraphQL query
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

		if len(response.Errors) > 0 {
			for _, gqlErr := range response.Errors {
				log.Debug().Str("error", gqlErr.Message).Msg("Cluster API GraphQL query error")
			}
			return
		}

		for _, record := range response.Data.LogRecordsFetch.Records {
			timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
			if err != nil {
				timestamp, err = time.Parse(time.RFC3339, record.Timestamp)
				if err != nil {
					log.Debug().Err(err).Str("timestamp", record.Timestamp).Msg("Could not parse timestamp")
					timestamp = time.Now()
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
	var token string
	var tokenErr error

	serviceAccounts := []string{"kubetail-cli", "default"}

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

// construct a GraphQL query string for fetching log records from the cluster API.
func (f *ClusterAPIProxyLogFetcher) buildLogRecordsQuery(source logs.LogSource, opts logs.FetcherOptions, backward bool, limit int) string {
	var queryParts []string

	// Build sources array as strings (e.g "namespace:pod/container")
	sourceStr := source.Namespace + ":"
	if source.PodName != "" {
		sourceStr += source.PodName
		if source.ContainerName != "" {
			sourceStr += "/" + source.ContainerName
		}
	} else {
		sourceStr += "*"
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
