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
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"

	"golang.org/x/net/http/httpproxy"
	"golang.org/x/net/idna"
)

// Dialing is how the client reaches the network. app passes the production
// values; a test passes a resolver, a proxy and a trust it controls.
type Dialing struct {
	Resolver *net.Resolver
	// Public reports whether an address may be dialled.
	Public func(netip.Addr) bool
	// Proxy is the proxy configuration; nil is none.
	Proxy *httpproxy.Config
	// RootCAs is the trust for TLS; nil is the system's.
	RootCAs *x509.CertPool
}

// NewTransport is what every fetch goes through. It dials no address d.Public
// refuses but the https proxy's, and, being a transport rather than a client, follows
// no redirect and keeps no cookie.
func NewTransport(d Dialing) *http.Transport {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	// The fetch's own bound is the one clock on it.
	tr.TLSHandshakeTimeout = 0
	tr.TLSClientConfig = &tls.Config{RootCAs: d.RootCAs, MinVersion: tls.VersionTLS12}
	tr.Proxy = nil

	var proxyAt string
	if d.Proxy != nil {
		proxyFor := d.Proxy.ProxyFunc()
		tr.Proxy = func(r *http.Request) (*url.URL, error) { return proxyFor(r.URL) }
		// Every fetch is https, so HTTPSProxy is the only proxy ever dialled.
		proxyAt, _ = proxyAddr(d.Proxy.HTTPSProxy)
	}

	// The control sees the address actually dialled, after DNS, on every
	// connection, so a name cannot resolve past it the second time.
	checked := &net.Dialer{Resolver: d.Resolver, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil || !d.Public(ap.Addr()) {
			return errPrivate
		}
		return nil
	}}
	proxy := &net.Dialer{Resolver: d.Resolver}
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// A corporate proxy usually sits on a private address.
		if proxyAt != "" && addr == proxyAt {
			return proxy.DialContext(ctx, network, addr)
		}
		return checked.DialContext(ctx, network, addr)
	}
	return tr
}

// proxyAddr is a proxy setting's address as net/http dials it: read as
// httpproxy reads the setting, where no scheme is http, then spelled as
// net/http's canonicalAddr spells it — an ASCII host as written, any other
// through idna, and a missing port the scheme's default.
func proxyAddr(setting string) (string, bool) {
	if setting == "" {
		return "", false
	}
	u, err := url.Parse(setting)
	if err != nil || u.Scheme == "" || u.Host == "" {
		if u, err = url.Parse("http://" + setting); err != nil {
			return "", false
		}
	}
	host := u.Hostname()
	if ascii, err := idna.Lookup.ToASCII(host); err == nil && !isASCII(host) {
		host = ascii
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443", "socks5": "1080", "socks5h": "1080"}[u.Scheme]
	}
	return net.JoinHostPort(host, port), true
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
