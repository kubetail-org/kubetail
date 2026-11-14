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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// testCA is a minimal in-memory CA for issuing client certs in tests.
type testCA struct {
	cert *x509.Certificate
	key  ed25519.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T, cn string) *testCA {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &testCA{cert: cert, key: priv, pool: pool}
}

// issue signs a leaf cert with the given CN. The returned cert satisfies
// ExtKeyUsageClientAuth so x509 verification accepts it for client auth.
func (ca *testCA) issue(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, pub, ca.key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert
}

// requestWithCert builds an HTTP request whose TLS.PeerCertificates contains
// the given chain (leaf first). Headers can be passed in to simulate the
// kube-apiserver front-proxy.
func requestWithCert(certs []*x509.Certificate, headers map[string]string) *http.Request {
	r := httptest.NewRequest("GET", "/apis/api.kubetail.com/v1/anything", nil)
	if certs != nil {
		r.TLS = &tls.ConnectionState{PeerCertificates: certs}
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// runMiddleware executes mw against r and returns the recorder plus the
// *ImpersonateInfo set on the request context (nil if the middleware
// aborted).
func runMiddleware(mw gin.HandlerFunc, r *http.Request) (*httptest.ResponseRecorder, *k8shelpers.ImpersonateInfo) {
	var impersonate *k8shelpers.ImpersonateInfo
	router := gin.New()
	router.Use(mw)
	router.Any("/*any", func(c *gin.Context) {
		impersonate, _ = c.Request.Context().Value(k8shelpers.K8SImpersonateCtxKey).(*k8shelpers.ImpersonateInfo)
		c.String(http.StatusOK, "ok")
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w, impersonate
}

func newTestAuthCfg(proxyCA *testCA, allowedNames ...string) *aggregationAuthConfig {
	return &aggregationAuthConfig{
		ProxyCAs:             proxyCA.pool,
		AllowedNames:         allowedNames,
		UsernameHeaders:      []string{"X-Remote-User"},
		GroupHeaders:         []string{"X-Remote-Group"},
		ExtraHeadersPrefixes: []string{"X-Remote-Extra-"},
	}
}

func TestAggregationAuth_RejectsRequestWithoutPeerCert(t *testing.T) {
	mw := newAggregationAuthMiddleware(newTestAuthCfg(newTestCA(t, "proxy-ca")))
	w, _ := runMiddleware(mw, requestWithCert(nil, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

// Threat model: any client whose cert is in the cluster's client-ca-file
// pool (kubectl admin, controller certs, system:node, …) holds a valid
// cluster cert but is NOT kube-apiserver. They must be rejected — the
// cluster-api accepts requests only from the front-proxy chain. Without
// this guarantee, any cluster user could call cluster-api directly with
// X-Remote-User headers and impersonate an arbitrary identity.
func TestAggregationAuth_RejectsNonFrontProxyCert(t *testing.T) {
	otherCA := newTestCA(t, "client-ca") // simulates a cluster client-CA
	proxyCA := newTestCA(t, "proxy-ca")
	leaf := otherCA.issue(t, "alice") // legitimate cluster client cert

	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA))
	r := requestWithCert([]*x509.Certificate{leaf}, nil)
	r.Header.Set("X-Remote-User", "system:masters")
	r.Header.Add("X-Remote-Group", "system:masters")
	r.Header.Set("X-Remote-Extra-Scopes", "openid")
	w, impersonate := runMiddleware(mw, r)

	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	assert.Nil(t, impersonate, "non-front-proxy cert must not produce an identity, even with spoofed headers")
}

// Threat model: TLS only proves possession of the leaf's private key
// (PeerCertificates[0]). The remaining slice entries are unauthenticated
// chain material the client supplies. If the middleware verifies any cert
// in the slice, an attacker holding any leaf can append a trusted cert
// (the proxy CA, a trusted intermediate, etc.) as PeerCertificates[1] and
// have it accepted as the front-proxy identity — bypassing the entire
// proof-of-possession guarantee.
func TestAggregationAuth_RejectsTrustedCertAppendedAsChainEntry(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	attackerCA := newTestCA(t, "attacker-ca")
	attackerLeaf := attackerCA.issue(t, "front-proxy-client")

	// Attacker presents their own (untrusted) leaf, then appends the trusted
	// proxy CA cert as a "chain" entry. AllowedNames is left empty so the CN
	// check cannot mask the cert-verification bypass.
	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA))
	r := requestWithCert([]*x509.Certificate{attackerLeaf, proxyCA.cert}, nil)
	r.Header.Set("X-Remote-User", "system:masters")
	w, impersonate := runMiddleware(mw, r)

	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	assert.Nil(t, impersonate, "appended trusted cert is unauthenticated chain material, not proof of possession")
}

func TestAggregationAuth_FrontProxyHeadersExtractIdentity(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "front-proxy-client")

	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA, "front-proxy-client"))
	r := requestWithCert([]*x509.Certificate{proxyLeaf}, nil)
	r.Header.Set("X-Remote-User", "bob")
	// kube-apiserver emits one X-Remote-Group header per group (Add, not Set).
	r.Header.Add("X-Remote-Group", "devs")
	r.Header.Add("X-Remote-Group", "sre")
	r.Header.Set("X-Remote-Extra-Scopes", "openid")
	w, impersonate := runMiddleware(mw, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)
	require.NotNil(t, impersonate)
	assert.Equal(t, "bob", impersonate.User)
	assert.Equal(t, []string{"devs", "sre"}, impersonate.Groups)
	assert.Equal(t, []string{"openid"}, impersonate.Extras["scopes"])
}

// The dashboard reverse proxy forwards a session's namespace scope as
// X-Forwarded-Namespaces; the middleware must land it on the request context
// so the resolvers can narrow their allowed-namespaces to it.
func TestAggregationAuth_ForwardedNamespacesOnContext(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "front-proxy-client")

	tests := []struct {
		name   string
		header string
		want   []string
	}{
		{"single namespace", "ns1", []string{"ns1"}},
		{"multiple namespaces", "ns1,ns2", []string{"ns1", "ns2"}},
		{"whitespace and empties trimmed", " ns1 , , ns2 ", []string{"ns1", "ns2"}},
		{"absent header leaves context unset", "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA, "front-proxy-client"))
			r := requestWithCert([]*x509.Certificate{proxyLeaf}, nil)
			r.Header.Set("X-Remote-User", "bob")
			if tt.header != "" {
				r.Header.Set("X-Forwarded-Namespaces", tt.header)
			}

			var got []string
			router := gin.New()
			router.Use(mw)
			router.Any("/*any", func(c *gin.Context) {
				got, _ = c.Request.Context().Value(k8shelpers.K8SSessionNamespacesCtxKey).([]string)
				c.String(http.StatusOK, "ok")
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)

			require.Equal(t, http.StatusOK, w.Result().StatusCode)
			assert.Equal(t, tt.want, got)
		})
	}
}

// kube-apiserver treats requestheader-extra-headers-prefix case-insensitively
// and is often configured with lowercase prefixes. Net/http canonicalizes the
// header map keys, so a sensitive prefix match would silently drop every
// forwarded extra attribute and quietly break any policy that consumes them.
func TestAggregationAuth_ExtraPrefixCaseInsensitive(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "front-proxy-client")

	cfg := newTestAuthCfg(proxyCA, "front-proxy-client")
	cfg.ExtraHeadersPrefixes = []string{"x-remote-extra-"}

	mw := newAggregationAuthMiddleware(cfg)
	r := requestWithCert([]*x509.Certificate{proxyLeaf}, nil)
	r.Header.Set("X-Remote-User", "bob")
	r.Header.Set("X-Remote-Extra-Scopes", "openid")
	w, impersonate := runMiddleware(mw, r)

	require.Equal(t, http.StatusOK, w.Result().StatusCode)
	require.NotNil(t, impersonate)
	assert.Equal(t, []string{"openid"}, impersonate.Extras["scopes"])
}

func TestAggregationAuth_FrontProxyCNNotAllowed(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "stranger")

	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA, "front-proxy-client"))
	r := requestWithCert([]*x509.Certificate{proxyLeaf}, map[string]string{"X-Remote-User": "bob"})
	w, _ := runMiddleware(mw, r)
	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

func TestAggregationAuth_RejectsCertFromUntrustedCA(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	untrustedCA := newTestCA(t, "untrusted-ca")
	leaf := untrustedCA.issue(t, "alice")

	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA, "front-proxy-client"))
	r := requestWithCert([]*x509.Certificate{leaf}, map[string]string{"X-Remote-User": "alice"})
	w, impersonate := runMiddleware(mw, r)

	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
	assert.Nil(t, impersonate)
}

func TestAggregationAuth_FrontProxyMissingUsernameHeader(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "front-proxy-client")

	mw := newAggregationAuthMiddleware(newTestAuthCfg(proxyCA, "front-proxy-client"))
	w, _ := runMiddleware(mw, requestWithCert([]*x509.Certificate{proxyLeaf}, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Result().StatusCode)
}

// kube-apiserver emits one X-Remote-Group header per group via http.Header.Add,
// so reading the header with Get/GetHeader silently drops every group after
// the first — and any RBAC binding subject not on that first group quietly
// stops applying. This test pins the multi-value extraction across edge cases
// (single value, many values, duplicates, no values, alternate header name).
func TestAggregationAuth_ExtractsAllGroups(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")
	proxyLeaf := proxyCA.issue(t, "front-proxy-client")

	tests := []struct {
		name         string
		groupHeaders []string // cfg.GroupHeaders override
		setHeaders   map[string][]string
		want         []string
	}{
		{
			name:       "single group",
			setHeaders: map[string][]string{"X-Remote-Group": {"devs"}},
			want:       []string{"devs"},
		},
		{
			name: "many groups all retained in order",
			setHeaders: map[string][]string{"X-Remote-Group": {
				"system:authenticated", "system:masters", "devs", "sre", "oncall",
			}},
			want: []string{"system:authenticated", "system:masters", "devs", "sre", "oncall"},
		},
		{
			name:       "duplicate values preserved",
			setHeaders: map[string][]string{"X-Remote-Group": {"devs", "devs", "sre"}},
			want:       []string{"devs", "devs", "sre"},
		},
		{
			name:       "no group header yields nil",
			setHeaders: nil,
			want:       nil,
		},
		{
			// Configured to look at X-Custom-Groups first; the ordering rule is
			// "first header name with any values wins" — pin it here so a
			// well-meaning refactor doesn't switch to "merge across all names"
			// (which would let an attacker append groups via an unexpected
			// header that happened to slip past upstream filtering).
			name:         "alternate group header honored",
			groupHeaders: []string{"X-Custom-Groups", "X-Remote-Group"},
			setHeaders: map[string][]string{
				"X-Remote-Group":  {"devs"},
				"X-Custom-Groups": {"sre", "oncall"},
			},
			want: []string{"sre", "oncall"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newTestAuthCfg(proxyCA, "front-proxy-client")
			if tt.groupHeaders != nil {
				cfg.GroupHeaders = tt.groupHeaders
			}
			mw := newAggregationAuthMiddleware(cfg)

			r := requestWithCert([]*x509.Certificate{proxyLeaf}, nil)
			r.Header.Set("X-Remote-User", "alice")
			for k, vs := range tt.setHeaders {
				for _, v := range vs {
					r.Header.Add(k, v)
				}
			}

			w, impersonate := runMiddleware(mw, r)
			require.Equal(t, http.StatusOK, w.Result().StatusCode)
			require.NotNil(t, impersonate)
			assert.Equal(t, tt.want, impersonate.Groups)
		})
	}
}

// caPEM returns the PEM encoding of ca.cert. Used to populate the ConfigMap
// data in loadAggregationAuthConfig tests.
func (ca *testCA) pem(t *testing.T) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.cert.Raw}))
}

func TestLoadAggregationAuthConfig(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")

	cs := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "extension-apiserver-authentication",
			Namespace: "kube-system",
		},
		Data: map[string]string{
			"requestheader-client-ca-file":       proxyCA.pem(t),
			"requestheader-allowed-names":        `["front-proxy-client"]`,
			"requestheader-username-headers":     `["X-Remote-User"]`,
			"requestheader-group-headers":        `["X-Remote-Group"]`,
			"requestheader-extra-headers-prefix": `["X-Remote-Extra-"]`,
		},
	})

	got, err := loadAggregationAuthConfig(context.Background(), cs)
	require.NoError(t, err)

	require.NotNil(t, got.ProxyCAs)
	assert.Equal(t, []string{"front-proxy-client"}, got.AllowedNames)
	assert.Equal(t, []string{"X-Remote-User"}, got.UsernameHeaders)
	assert.Equal(t, []string{"X-Remote-Group"}, got.GroupHeaders)
	assert.Equal(t, []string{"X-Remote-Extra-"}, got.ExtraHeadersPrefixes)

	// Sanity: a leaf signed by proxyCA verifies against the loaded pool.
	leaf := proxyCA.issue(t, "front-proxy-client")
	_, verr := leaf.Verify(x509.VerifyOptions{Roots: got.ProxyCAs, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	require.NoError(t, verr)
}

func TestLoadAggregationAuthConfigMissingConfigMap(t *testing.T) {
	cs := fake.NewClientset()
	_, err := loadAggregationAuthConfig(context.Background(), cs)
	require.Error(t, err)
}

func TestLoadAggregationAuthConfigBadJSON(t *testing.T) {
	proxyCA := newTestCA(t, "proxy-ca")

	cs := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "extension-apiserver-authentication", Namespace: "kube-system"},
		Data: map[string]string{
			"requestheader-client-ca-file":       proxyCA.pem(t),
			"requestheader-allowed-names":        `not json`,
			"requestheader-username-headers":     `["X-Remote-User"]`,
			"requestheader-group-headers":        `["X-Remote-Group"]`,
			"requestheader-extra-headers-prefix": `["X-Remote-Extra-"]`,
		},
	})
	_, err := loadAggregationAuthConfig(context.Background(), cs)
	require.Error(t, err)
}
