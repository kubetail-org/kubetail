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

package app

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubetail-org/kubetail/modules/cluster-api/internal/helpers"
	"github.com/kubetail-org/kubetail/modules/shared/httphelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// setImpersonateOnRequest writes the authenticated identity to the request
// context for ImpersonatingRoundTripper.
func setImpersonateOnRequest(c *gin.Context, info *k8shelpers.ImpersonateInfo) {
	ctx := context.WithValue(c.Request.Context(), k8shelpers.K8SImpersonateCtxKey, info)
	c.Request = c.Request.WithContext(ctx)
}

// aggregationAuthConfig holds the parsed contents of kube-system's
// extension-apiserver-authentication ConfigMap. Built once at startup; each
// request only does cert verification + header reads.
//
// AllowedNames is the `requestheader-allowed-names` allowlist; an empty list
// means any front-proxy CN is accepted (matches kube-apiserver behavior).
type aggregationAuthConfig struct {
	ProxyCAs             *x509.CertPool
	AllowedNames         []string
	UsernameHeaders      []string
	GroupHeaders         []string
	ExtraHeadersPrefixes []string
}

// newAggregationAuthMiddleware authenticates the caller as the kube-apiserver
// front-proxy (proxy CA + request headers). Direct mTLS from any other holder
// of a cluster cert is rejected — the cluster-api accepts requests only via
// the kube-apiserver chain. The authenticated identity lands on the request
// context as *k8shelpers.ImpersonateInfo.
func newAggregationAuthMiddleware(cfg *aggregationAuthConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		r := c.Request

		// The listener is fixed at RequestClientCert so probes and discovery
		// routes can complete a handshake without a cert; this middleware is
		// the actual enforcement point that a peer cert is present.
		if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "client certificate required"})
			return
		}

		now := time.Now()

		// The TLS handshake only proves possession of the private key for the
		// leaf (PeerCertificates[0]); the rest of the slice is unauthenticated
		// chain material the client supplied. Verifying any cert from the slice
		// would let an attacker append a trusted CA/intermediate as a "chain"
		// entry and have it accepted as the proxy identity.
		var proxyCert *x509.Certificate
		if cfg.ProxyCAs != nil {
			opts := x509.VerifyOptions{
				Roots:       cfg.ProxyCAs,
				CurrentTime: now,
				KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			if len(r.TLS.PeerCertificates) > 1 {
				opts.Intermediates = x509.NewCertPool()
				for _, cert := range r.TLS.PeerCertificates[1:] {
					opts.Intermediates.AddCert(cert)
				}
			}
			leaf := r.TLS.PeerCertificates[0]
			if _, err := leaf.Verify(opts); err == nil {
				proxyCert = leaf
			}
		}
		if proxyCert == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "no valid certificate found"})
			return
		}

		if len(cfg.AllowedNames) > 0 {
			cn := proxyCert.Subject.CommonName
			if !slices.Contains(cfg.AllowedNames, cn) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": fmt.Sprintf("proxy CN %q not in allowed list", cn),
				})
				return
			}
		}

		var user string
		for _, h := range cfg.UsernameHeaders {
			if v := c.GetHeader(h); v != "" {
				user = v
				break
			}
		}
		if user == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing user header"})
			return
		}

		// kube-apiserver emits one X-Remote-Group header per group via
		// http.Header.Add. Use Values to read all of them — Get/GetHeader
		// returns only the first, which silently drops the rest and breaks
		// any RBAC binding whose subject isn't on the first group.
		var groups []string
		for _, h := range cfg.GroupHeaders {
			if vals := r.Header.Values(h); len(vals) > 0 {
				groups = append(groups, vals...)
				break
			}
		}

		// Request-header extra prefixes are case-insensitive (kube-apiserver
		// behavior); incoming header map keys are canonicalized by net/http,
		// so a lowercase configured prefix (e.g. "x-remote-extra-") would
		// never match a sensitive CutPrefix against "X-Remote-Extra-Foo".
		var extras map[string][]string
		if len(cfg.ExtraHeadersPrefixes) > 0 {
			for name, vals := range r.Header {
				lname := strings.ToLower(name)
				for _, prefix := range cfg.ExtraHeadersPrefixes {
					if after, ok := strings.CutPrefix(lname, strings.ToLower(prefix)); ok {
						if extras == nil {
							extras = map[string][]string{}
						}
						extras[after] = vals
					}
				}
			}
		}

		setImpersonateOnRequest(c, &k8shelpers.ImpersonateInfo{
			User:   user,
			Groups: groups,
			Extras: extras,
		})

		// A session namespace scope forwarded by the dashboard reverse proxy
		// (see httphelpers.HeaderForwardedNamespaces). It can only narrow this
		// server's allowed-namespaces, never widen it, so it is honored without
		// a dedicated feature flag.
		if v := c.GetHeader(httphelpers.HeaderForwardedNamespaces); v != "" {
			var nsList []string
			for _, ns := range strings.Split(v, ",") {
				if ns = strings.TrimSpace(ns); ns != "" {
					nsList = append(nsList, ns)
				}
			}
			if len(nsList) > 0 {
				ctx := context.WithValue(c.Request.Context(), k8shelpers.K8SSessionNamespacesCtxKey, nsList)
				c.Request = c.Request.WithContext(ctx)
			}
		}

		c.Next()
	}
}

// loadAggregationAuthConfig reads kube-system's
// extension-apiserver-authentication ConfigMap and returns a parsed
// aggregationAuthConfig. The kube-apiserver maintains this ConfigMap; any
// service registered as an APIService is expected to read it for its
// front-proxy CA + request-header configuration.
func loadAggregationAuthConfig(ctx context.Context, cs kubernetes.Interface) (*aggregationAuthConfig, error) {
	cm, err := cs.CoreV1().ConfigMaps("kube-system").Get(ctx, "extension-apiserver-authentication", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("read extension-apiserver-authentication: %w", err)
	}

	proxyCAs, err := helpers.PoolFromPEM(cm.Data["requestheader-client-ca-file"])
	if err != nil {
		return nil, fmt.Errorf("requestheader-client-ca-file in extension-apiserver-authentication: %w", err)
	}

	out := &aggregationAuthConfig{ProxyCAs: proxyCAs}
	for _, f := range []struct {
		key string
		dst *[]string
	}{
		{"requestheader-allowed-names", &out.AllowedNames},
		{"requestheader-username-headers", &out.UsernameHeaders},
		{"requestheader-group-headers", &out.GroupHeaders},
		{"requestheader-extra-headers-prefix", &out.ExtraHeadersPrefixes},
	} {
		if raw := cm.Data[f.key]; raw != "" {
			if err := json.Unmarshal([]byte(raw), f.dst); err != nil {
				return nil, fmt.Errorf("parse %s: %w", f.key, err)
			}
		}
	}
	return out, nil
}
