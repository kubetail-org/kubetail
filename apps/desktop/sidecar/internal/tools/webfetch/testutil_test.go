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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/net/dns/dnsmessage"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/tools"
)

// hosts is every name the test certificate covers. A wildcard covers one
// level only, so each is named.
var hosts = []string{"a.test", "www.a.test", "b.test"}

// site is a TLS server for the test's hosts, reached through a resolver the
// test controls.
type site struct {
	srv  *httptest.Server
	dns  *dnsServer
	pool *x509.CertPool
	addr netip.AddrPort
}

// newSite starts h over TLS on a certificate naming hosts, and a DNS table
// resolving each of them to the server.
func newSite(t *testing.T, h http.Handler) *site {
	t.Helper()
	cert, pool := testCert(t)
	srv := httptest.NewUnstartedServer(h)
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	addr := netip.MustParseAddrPort(srv.Listener.Addr().String())
	s := &site{srv: srv, dns: &dnsServer{table: map[string][]netip.Addr{}}, pool: pool, addr: addr}
	for _, h := range hosts {
		s.dns.set(h, addr.Addr())
	}
	return s
}

// url is https on name at the server's port.
func (s *site) url(name, path string) string {
	return "https://" + net.JoinHostPort(name, s.port()) + path
}

func (s *site) port() string { return strconv.Itoa(int(s.addr.Port())) }

// dialing is how a test's client reaches the site: the test's resolver and
// trust, and a Public that also admits the server's own address.
func (s *site) dialing() Dialing {
	return Dialing{
		Resolver: s.dns.resolver(),
		Public:   func(a netip.Addr) bool { return a == s.addr.Addr() || Public(a) },
		RootCAs:  s.pool,
	}
}

// tool is the tool over a client of d, with a bound no test waits out.
func (s *site) tool(d Dialing) *Tool {
	return New(NewTransport(d), time.Minute)
}

// run is one call of tl fetching u, with no chat's directory.
func run(t *testing.T, tl *Tool, u string) (string, bool) {
	t.Helper()
	return tl.Run(t.Context(), tools.Runtime{}, call(u))
}

// testCert is a self-signed certificate naming hosts, and a pool trusting it.
func testCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "webfetch test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		DNSNames:              hosts,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// dnsServer answers A and AAAA queries from a table the test sets, and can
// change between queries. A name it does not hold is NXDOMAIN.
type dnsServer struct {
	mu    sync.Mutex
	table map[string][]netip.Addr
}

func (d *dnsServer) set(name string, addrs ...netip.Addr) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.table[strings.ToLower(name)+"."] = addrs
}

// resolver is a pure-Go resolver whose every query reaches d, over a pipe
// framed as DNS over TCP.
func (d *dnsServer) resolver() *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go d.serve(server)
		return client, nil
	}}
}

func (d *dnsServer) serve(c net.Conn) {
	defer c.Close()
	for {
		var n uint16
		if binary.Read(c, binary.BigEndian, &n) != nil {
			return
		}
		query := make([]byte, n)
		if _, err := io.ReadFull(c, query); err != nil {
			return
		}
		answer, err := d.answer(query)
		if err != nil {
			return
		}
		if binary.Write(c, binary.BigEndian, uint16(len(answer))) != nil {
			return
		}
		if _, err := c.Write(answer); err != nil {
			return
		}
	}
}

func (d *dnsServer) answer(query []byte) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	addrs, ok := d.table[strings.ToLower(q.Name.String())]
	d.mu.Unlock()

	rcode := dnsmessage.RCodeSuccess
	if !ok {
		rcode = dnsmessage.RCodeNameError
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{ID: h.ID, Response: true, Authoritative: true, RCode: rcode})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	if err := b.StartAnswers(); err != nil {
		return nil, err
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 0}
	for _, a := range addrs {
		switch {
		case q.Type == dnsmessage.TypeA && a.Is4():
			err = b.AResource(rh, dnsmessage.AResource{A: a.As4()})
		case q.Type == dnsmessage.TypeAAAA && a.Is6():
			err = b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: a.As16()})
		}
		if err != nil {
			return nil, err
		}
	}
	return b.Finish()
}
