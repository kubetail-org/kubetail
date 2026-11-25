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

package runproxy

import (
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 64 << 10
)

// Server is the server a run serves its door on: HTTP/1.1 alone, plain or,
// with a certificate, over TLS on the same listener (IsTLS).
type Server struct {
	*http.Server
	tls *tls.Config
}

// NewServer is the server a run serves its door h on, with cert for its TLS
// connections; with none, a connection that opens a handshake is closed. Any
// local process can reach a forwarder's port, so a connection that never sends
// its headers, or sits idle between requests, is closed rather than held for
// the run's life. The first byte and a handshake are read under the header
// bound too.
// It sets no read or write timeout, which would cut a watch; a watch is not
// idle while it streams.
func NewServer(h http.Handler, cert *Certificate) *Server {
	return newServer(h, cert, readHeaderTimeout, idleTimeout)
}

func newServer(h http.Handler, cert *Certificate, readHeader, idle time.Duration) *Server {
	s := &Server{Server: &http.Server{
		Handler:           h,
		ReadHeaderTimeout: readHeader,
		IdleTimeout:       idle,
		MaxHeaderBytes:    maxHeaderBytes,
		ConnContext:       connContext,
	}}
	if cert != nil {
		s.tls = &tls.Config{
			Certificates: []tls.Certificate{cert.TLS},
			NextProtos:   []string{"http/1.1"},
			MinVersion:   tls.VersionTLS12,
		}
	}
	return s
}

// Serve serves ln, each connection's transport read from its first byte.
func (s *Server) Serve(ln net.Listener) error {
	return s.Server.Serve(&sniffListener{Listener: ln, config: s.tls})
}
