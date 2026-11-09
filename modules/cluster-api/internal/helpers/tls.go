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

package helpers

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
)

// PoolFromPEM returns an x509.CertPool from the given PEM bytes, or nil
// when the input is empty. Returns an error if the input contains no valid
// certificates.
func PoolFromPEM(pemBytes string) (*x509.CertPool, error) {
	if pemBytes == "" {
		return nil, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(pemBytes)) {
		return nil, fmt.Errorf("no valid PEM certificates")
	}
	return pool, nil
}

// BuildTLSConfig assembles the *tls.Config for the cluster-api HTTP server.
// RequestClientCert collects a peer cert for the aggregation auth middleware
// to verify, and accepts a handshake without one, since kubelet probes and
// the discovery routes share the listener. Cert/key are loaded by
// http.Server itself.
func BuildTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.RequestClientCert,
	}
}
