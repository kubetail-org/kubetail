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

package bash

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// get sends d a request for host carrying token, and answers its status and
// body.
func get(t *testing.T, d http.Handler, method, host, token string) (int, string) {
	t.Helper()
	r := httptest.NewRequest(method, "http://"+host+"/x", nil)
	r.Host = host
	if token != "" {
		r.Header.Set(tokenHeader, token)
	}
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	return w.Code, w.Body.String()
}

// Each host reaches its own handler; anything else is the cluster grant's
// 401, whatever its host or method.
func TestTheDoorRoutesByHost(t *testing.T) {
	t.Parallel()

	aws := mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))
	gcp := mountFor(t, newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid"))
	cluster := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "cluster") })
	d, err := newDoor(cluster, []CloudMount{aws, gcp})
	require.NoError(t, err)

	_, body := get(t, d, http.MethodGet, "cluster.kubetail.invalid", "")
	assert.Equal(t, "cluster", body)
	code, body := get(t, d, http.MethodGet, "127.0.0.1:6443", aws.Grant.Token())
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "read aws", body)
	_, body = get(t, d, http.MethodGet, "storage.gcp.kubetail.invalid", gcp.Grant.Token())
	assert.Equal(t, "read gcp", body)

	_, want := get(t, d, http.MethodGet, "unknown.kubetail.invalid", "")
	assert.Contains(t, want, "kubetail: unauthorized")
	for name, req := range map[string][2]string{
		"an unknown host":   {http.MethodGet, "unknown.kubetail.invalid"},
		"localhost":         {http.MethodGet, "localhost:6443"},
		"a CONNECT":         {http.MethodConnect, "127.0.0.1:6443"},
		"the cluster's own": {http.MethodConnect, "cluster.kubetail.invalid"},
	} {
		code, body := get(t, d, req[0], req[1], aws.Grant.Token())
		assert.Equal(t, http.StatusUnauthorized, code, name)
		assert.Equal(t, want, body, name)
	}
}

// A cloud platform's request asking for a stream past HTTP is refused at the
// door, before its proxy; the cluster's goes to its grant, which refuses it
// itself.
func TestTheDoorRefusesACloudPlatformsUpgrade(t *testing.T) {
	t.Parallel()

	aws := mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))
	cluster := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "cluster") })
	d, err := newDoor(cluster, []CloudMount{aws})
	require.NoError(t, err)
	for _, h := range []http.Header{{"Upgrade": {"websocket"}}, {"Connection": {"keep-alive, Upgrade"}}} {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:6443/x", nil)
		r.Header = h.Clone()
		r.Header.Set(tokenHeader, aws.Grant.Token())
		w := httptest.NewRecorder()
		d.ServeHTTP(w, r)
		assert.Equal(t, http.StatusForbidden, w.Code, h)
		assert.Contains(t, w.Body.String(), refusedUpgrade, h)
	}
	r := httptest.NewRequest(http.MethodGet, "http://cluster.kubetail.invalid/x", nil)
	r.Header.Set("Upgrade", "SPDY/3.1")
	w := httptest.NewRecorder()
	d.ServeHTTP(w, r)
	assert.Equal(t, "cluster", w.Body.String())
}

// A run with no cluster still answers the cluster's host 401.
func TestTheDoorWithNoClusterRefusesItsHost(t *testing.T) {
	t.Parallel()

	d, err := newDoor(nil, []CloudMount{mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))})
	require.NoError(t, err)
	code, _ := get(t, d, http.MethodGet, "cluster.kubetail.invalid", "")
	assert.Equal(t, http.StatusUnauthorized, code)
}

// The door is not trusted: one cloud platform's token on another's route is
// that grant's refusal.
func TestAWrongRouteIsUnauthorized(t *testing.T) {
	t.Parallel()

	aws := mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))
	gcp := mountFor(t, newFakePlatform(permissions.GCP, "storage.gcp.kubetail.invalid"))
	d, err := newDoor(nil, []CloudMount{aws, gcp})
	require.NoError(t, err)
	code, _ := get(t, d, http.MethodGet, "storage.gcp.kubetail.invalid", aws.Grant.Token())
	assert.Equal(t, http.StatusUnauthorized, code)
}

// A cloud platform's grant reads its switch on every request that holds its
// token: off, the request is refused 401; on again, it is served. The door
// reads no switch.
func TestSwitchingACloudPlatformOffClosesItsRoute(t *testing.T) {
	t.Parallel()

	sw := switchesOn(permissions.AWS)
	aws := mountSwitched(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"), sw)
	d, err := newDoor(nil, []CloudMount{aws})
	require.NoError(t, err)

	sw.set(permissions.AWS, false)
	code, body := get(t, d, http.MethodGet, "127.0.0.1:6443", aws.Grant.Token())
	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Contains(t, body, "kubetail: unauthorized")

	sw.set(permissions.AWS, true)
	_, body = get(t, d, http.MethodGet, "127.0.0.1:6443", aws.Grant.Token())
	assert.Equal(t, "read aws", body)

	sw.fail(errors.New("the database is locked"))
	code, _ = get(t, d, http.MethodGet, "127.0.0.1:6443", aws.Grant.Token())
	assert.Equal(t, http.StatusUnauthorized, code, "a switch that cannot be read is off")
}

func TestTwoMountsCannotNameOneHost(t *testing.T) {
	t.Parallel()

	aws := mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))
	clash := mountFor(t, newFakePlatform(permissions.Azure, "127.0.0.1:6443"))
	_, err := newDoor(nil, []CloudMount{aws, clash})
	assert.ErrorContains(t, err, "127.0.0.1:6443 over plain HTTP")

	overTLS := newFakePlatform(permissions.Azure, "127.0.0.1:6443")
	overTLS.tls = true
	_, err = newDoor(nil, []CloudMount{aws, mountFor(t, overTLS)})
	assert.NoError(t, err, "one host on two transports is two routes")
	twiceOverTLS := newFakePlatform(permissions.GCP, "127.0.0.1:6443")
	twiceOverTLS.tls = true
	_, err = newDoor(nil, []CloudMount{mountFor(t, overTLS), mountFor(t, twiceOverTLS)})
	assert.ErrorContains(t, err, "127.0.0.1:6443 over TLS")

	onCluster := mountFor(t, newFakePlatform(permissions.GCP, "cluster.kubetail.invalid"))
	_, err = newDoor(http.NotFoundHandler(), []CloudMount{onCluster})
	assert.ErrorContains(t, err, "cluster.kubetail.invalid")
}

// sendOver sends a GET for host over TLS or plain HTTP to addr, carrying
// token, and answers its status and body.
func sendOver(t *testing.T, addr string, roots *x509.CertPool, overTLS bool, host, token string) (int, string) {
	t.Helper()
	scheme := "http"
	if overTLS {
		scheme = "https"
	}
	r, err := http.NewRequest(http.MethodGet, scheme+"://"+addr+"/x", nil)
	require.NoError(t, err)
	r.Host = host
	r.Header.Set(tokenHeader, token)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"}}}
	resp, err := client.Do(r)
	require.NoError(t, err)
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

// The loopback authority names a route per transport: a plain request reaches
// the plain cloud platform, a TLS one the TLS cloud platform, and the cluster's host over
// TLS is the door's 401.
func TestTheLoopbackAuthorityRoutesByTransport(t *testing.T) {
	t.Parallel()

	plain := mountFor(t, newFakePlatform(permissions.AWS, "127.0.0.1:6443"))
	secure := newFakePlatform(permissions.Azure, "127.0.0.1:6443")
	secure.tls = true
	azure := mountFor(t, secure)
	cluster := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "cluster") })
	d, err := newDoor(cluster, []CloudMount{plain, azure})
	require.NoError(t, err)
	cert, err := runproxy.NewCertificate(time.Now())
	require.NoError(t, err)
	srv := runproxy.NewServer(d, cert)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(cert.PEM)
	addr := ln.Addr().String()

	_, body := sendOver(t, addr, roots, false, "127.0.0.1:6443", plain.Grant.Token())
	assert.Equal(t, "read aws", body)
	_, body = sendOver(t, addr, roots, true, "127.0.0.1:6443", azure.Grant.Token())
	assert.Equal(t, "read azure", body)
	_, body = sendOver(t, addr, roots, false, "cluster.kubetail.invalid", "")
	assert.Equal(t, "cluster", body)
	code, _ := sendOver(t, addr, roots, true, "cluster.kubetail.invalid", "")
	assert.Equal(t, http.StatusUnauthorized, code)
	code, _ = sendOver(t, addr, roots, true, "127.0.0.1:6443", plain.Grant.Token())
	assert.Equal(t, http.StatusUnauthorized, code, "the plain cloud platform's token over TLS is Azure's refusal")
}
