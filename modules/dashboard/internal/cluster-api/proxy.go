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

package clusterapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"k8s.io/kubectl/pkg/proxy"
	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// For parsing paths of the form /:kubeContext/:namespace/:serviceName/*relPath
var desktopProxyPathRegex = regexp.MustCompile(`^/([^/]+)/([^/]+)/([^/]+)/(.*)$`)

// For parsing cookie paths
var cookiepathRegex = regexp.MustCompile(`Path=[^;]*`)

// Proxy interface
type Proxy interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
	Shutdown()
}

// Represents DesktopProxy
type DesktopProxy struct {
	cm         k8shelpers.ConnectionManager
	pathPrefix string
	phCache    map[string]http.Handler
	satCache   map[string]*k8shelpers.ServiceAccountToken
	mu         sync.Mutex
	shutdownCh chan struct{}
}

// ServeHTTP
func (p *DesktopProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origPath := r.URL.Path

	// Trim prefix
	proxyPath := strings.TrimPrefix(origPath, p.pathPrefix)

	// Parse url
	matches := desktopProxyPathRegex.FindStringSubmatch(proxyPath)
	if matches == nil {
		http.Error(w, fmt.Sprintf("did not understand url: %s", origPath), http.StatusInternalServerError)
		return
	}
	kubeContext, namespace, serviceName, relPath := matches[1], matches[2], matches[3], matches[4]

	// Get Kubernetes proxy handler
	h, err := p.getOrCreateKubernetesProxyHandler(kubeContext)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-write url
	newPath := path.Join("/api/v1/namespaces", namespace, "services", fmt.Sprintf("%s:http", serviceName), "proxy", relPath)
	if strings.HasSuffix(newPath, "/proxy") {
		newPath += "/"
	}
	u := *r.URL
	u.Path = newPath
	r.URL = &u

	// Get service-account-token
	sat, err := p.getOrCreateServiceAccountToken(r.Context(), kubeContext, namespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Add token to authentication header
	token, err := sat.Token(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	r.Header.Add("X-Forwarded-Authorization", fmt.Sprintf("Bearer %s", token))

	// Passthrough if upgrade request
	if r.Header.Get("Upgrade") != "" {
		h.ServeHTTP(w, r)
		return
	}

	// Execute
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	// Re-write cookie path
	cookiePath := strings.TrimSuffix(origPath, relPath)
	for k, v := range rec.Header() {
		if k == "Set-Cookie" {
			for _, cookie := range v {
				modifiedCookie := cookiepathRegex.ReplaceAllString(cookie, fmt.Sprintf("Path=%s", cookiePath))
				w.Header().Add("Set-Cookie", modifiedCookie)
			}
		} else {
			w.Header()[k] = v
		}
	}

	// Send result to client
	w.WriteHeader(rec.Code)
	w.Write(rec.Body.Bytes())
}

// Shutdown
func (p *DesktopProxy) Shutdown() {
	close(p.shutdownCh)
}

// Get or create Kubernetes proxy handler
func (p *DesktopProxy) getOrCreateKubernetesProxyHandler(kubeContext string) (http.Handler, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check cache
	h, exists := p.phCache[kubeContext]
	if !exists {
		// Get rest config
		restConfig, err := p.cm.GetOrCreateRestConfig(ptr.To(kubeContext))
		if err != nil {
			return nil, err
		}

		// Create proxy handler
		h, err = proxy.NewProxyHandler("/", nil, restConfig, 0, false)
		if err != nil {
			return nil, err
		}

		// Add to cache
		p.phCache[kubeContext] = h
	}

	return h, nil
}

// Get or create service-account-token
func (p *DesktopProxy) getOrCreateServiceAccountToken(ctx context.Context, kubeContext string, namespace string) (*k8shelpers.ServiceAccountToken, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate cache key
	k := fmt.Sprintf("%s/%s", kubeContext, namespace)

	// Check cache
	sat, exists := p.satCache[k]
	if !exists {
		clientset, err := p.cm.GetOrCreateClientset(ptr.To(kubeContext))
		if err != nil {
			return nil, err
		}

		// Initialize new service-account-token
		sat, err = k8shelpers.NewServiceAccountToken(ctx, clientset, namespace, "kubetail-cli", p.shutdownCh)
		if err != nil {
			return nil, err
		}

		// Add to cache
		p.satCache[k] = sat
	}

	return sat, nil
}

// Create new DesktopProxy
func NewDesktopProxy(cm k8shelpers.ConnectionManager, pathPrefix string) (*DesktopProxy, error) {
	return &DesktopProxy{
		cm:         cm,
		pathPrefix: pathPrefix,
		phCache:    make(map[string]http.Handler),
		satCache:   make(map[string]*k8shelpers.ServiceAccountToken),
		shutdownCh: make(chan struct{}),
	}, nil
}

// Represents InClusterProxy
type InClusterProxy struct {
	*httputil.ReverseProxy
}

// Shutdown
func (p *InClusterProxy) Shutdown() {
}

// Create new InClusterProxy
func NewInClusterProxy(clusterAPIEndpoint string, pathPrefix string) (*InClusterProxy, error) {
	// Parse endpoint url
	endpointUrl, err := url.Parse(clusterAPIEndpoint)
	if err != nil {
		return nil, err
	}

	// Get token
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenPath)
	if err != nil {
		return nil, err
	}

	// Init reverseProxy
	reverseProxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			// Re-write url
			targetUrl := endpointUrl
			targetUrl.Path = path.Join("/", strings.TrimPrefix(r.URL.Path, pathPrefix))
			r.URL = targetUrl

			// Add token to authentication header
			r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		},
		ModifyResponse: func(resp *http.Response) error {
			// Re-write cookie path
			pathArg := fmt.Sprintf("Path=%s", path.Join("/", pathPrefix)+"/")
			cookies := resp.Header["Set-Cookie"]
			for i, cookie := range cookies {
				cookies[i] = cookiepathRegex.ReplaceAllString(cookie, pathArg)
			}
			resp.Header["Set-Cookie"] = cookies

			return nil
		},
	}

	return &InClusterProxy{reverseProxy}, nil
}
