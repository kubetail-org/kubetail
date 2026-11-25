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

package webfetch

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http/httpproxy"
)

func ok(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") }

// get is one GET of u through tr, answering the body.
func get(t *testing.T, tr http.RoundTripper, u string) (string, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	require.NoError(t, err)
	resp, err := tr.RoundTrip(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// A name resolving to a refused address is refused on the dial, whatever the
// name; a name with a refused and an allowed address reaches the allowed one.
func TestWebFetchRefusesNonPublicNames(t *testing.T) {
	s := newSite(t, http.HandlerFunc(ok))
	tr := NewTransport(s.dialing())
	for _, a := range []string{"127.0.0.2", "10.0.0.1", "169.254.169.254", "::1", "fe80::1", "100.64.0.1"} {
		s.dns.set("a.test", netip.MustParseAddr(a))
		_, err := get(t, tr, s.url("a.test", "/"))
		assert.ErrorIs(t, err, errPrivate, a)
	}

	s.dns.set("a.test", netip.MustParseAddr("10.0.0.1"), s.addr.Addr())
	body, err := get(t, tr, s.url("a.test", "/"))
	require.NoError(t, err)
	assert.Equal(t, "ok", body)
}

// The check is on the address dialled, so a name that answers differently the
// second time is judged by its second answer.
func TestWebFetchRefusesARebindingName(t *testing.T) {
	s := newSite(t, http.HandlerFunc(ok))
	tr := NewTransport(s.dialing())
	// A pooled connection would carry the second fetch without a second lookup.
	tr.DisableKeepAlives = true

	_, err := get(t, tr, s.url("a.test", "/"))
	require.NoError(t, err)

	s.dns.set("a.test", netip.MustParseAddr("127.0.0.2"))
	_, err = get(t, tr, s.url("a.test", "/"))
	assert.ErrorIs(t, err, errPrivate)
}

// The proxy is the one destination the check lets through: with it the
// sidecar dials the proxy, which resolves the name itself.
func TestWebFetchLetsTheProxyThrough(t *testing.T) {
	s := newSite(t, http.HandlerFunc(ok))
	proxy, used := connectProxy(t, s.addr.String())
	d := s.dialing()
	d.Public = Public
	d.Proxy = &httpproxy.Config{HTTPSProxy: proxy}

	body, err := get(t, NewTransport(d), s.url("a.test", "/"))
	require.NoError(t, err)
	assert.Equal(t, "ok", body)
	assert.True(t, used.Load())

	d.Proxy = nil
	_, err = get(t, NewTransport(d), s.url("a.test", "/"))
	assert.ErrorIs(t, err, errPrivate)
}

// Every fetch is https, so HTTPProxy is never dialled as a proxy, and a fetch
// of its address is checked like any other.
func TestWebFetchChecksTheHTTPProxysAddress(t *testing.T) {
	s := newSite(t, http.HandlerFunc(ok))
	d := s.dialing()
	d.Public = Public
	d.Proxy = &httpproxy.Config{HTTPProxy: net.JoinHostPort("a.test", s.port())}

	_, err := get(t, NewTransport(d), s.url("a.test", "/"))
	assert.ErrorIs(t, err, errPrivate)
}

// The proxies' addresses are spelled as net/http spells the address it dials.
func TestProxyAddrIsTheDialsSpelling(t *testing.T) {
	for in, want := range map[string]string{
		"proxy.corp":              "proxy.corp:80",
		"Proxy.Corp:3128":         "Proxy.Corp:3128",
		"http://10.0.0.1:3128":    "10.0.0.1:3128",
		"https://proxy.corp":      "proxy.corp:443",
		"socks5://proxy.corp":     "proxy.corp:1080",
		"http://user:pw@p.corp:8": "p.corp:8",
		"http://[fd00::1]:3128":   "[fd00::1]:3128",
		"http://münchen.de:3128":  "xn--mnchen-3ya.de:3128",
	} {
		got, ok := proxyAddr(in)
		assert.True(t, ok, in)
		assert.Equal(t, want, got, in)
	}
	_, ok := proxyAddr("")
	assert.False(t, ok)
	_, ok = proxyAddr("proxy\x7f.corp")
	assert.False(t, ok, "a setting that is no URL however read")
}

// connectProxy is an HTTP proxy that tunnels every CONNECT to target, whatever
// name it is asked for, and reports whether it was used.
func connectProxy(t *testing.T, target string) (string, *atomic.Bool) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	var used atomic.Bool
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				if err != nil || req.Method != http.MethodConnect {
					return
				}
				up, err := net.Dial("tcp", target)
				if err != nil {
					return
				}
				defer up.Close()
				used.Store(true)
				if _, err := io.WriteString(c, "HTTP/1.1 200 OK\r\n\r\n"); err != nil {
					return
				}
				go func() { _, _ = io.Copy(up, c) }()
				_, _ = io.Copy(c, up)
			}()
		}
	}()
	return ln.Addr().String(), &used
}
