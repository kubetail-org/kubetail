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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"time"
)

// SystemBundles are the system's root bundles, in the order WriteBundle
// tries them: macOS's, then Debian's, then Red Hat's.
var SystemBundles = []string{
	"/etc/ssl/cert.pem",
	"/etc/ssl/certs/ca-certificates.crt",
	"/etc/pki/tls/certs/ca-bundle.crt",
}

// Certificate is one run's TLS identity: a self-signed certificate whose one
// name is 127.0.0.1, and its key, which never leaves the sidecar's memory.
type Certificate struct {
	TLS tls.Certificate
	PEM []byte // the certificate alone
}

// NewCertificate is a fresh run's certificate, valid from an hour before now
// for 365 days. The key dies with the run, so expiry protects nothing; the
// hour absorbs a clock behind the sidecar's.
func NewCertificate(now time.Time) (*Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil { // coverage:ignore crypto/rand does not fail
		return nil, err
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil { // coverage:ignore a P-256 key always marshals
		return nil, err
	}
	keyID := sha256.Sum256(pub)
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil { // coverage:ignore crypto/rand does not fail
		return nil, err
	}
	notBefore := now.Add(-time.Hour)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kubetail run"},
		NotBefore:             notBefore,
		NotAfter:              notBefore.AddDate(0, 0, 365),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		SubjectKeyId:          keyID[:20],
		AuthorityKeyId:        keyID[:20],
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil { // coverage:ignore the template is fixed and valid
		return nil, err
	}
	return &Certificate{
		TLS: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key},
		PEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}, nil
}

// WriteBundle writes path, owner-only and never over a file: the
// certificate, then the first of systemBundles it can read, so a client
// pointed at it still verifies public hosts.
func (c *Certificate) WriteBundle(path string, systemBundles []string) error {
	bundle := c.PEM
	for _, p := range systemBundles {
		if roots, err := os.ReadFile(p); err == nil {
			bundle = append(append([]byte{}, c.PEM...), roots...)
			break
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(bundle)
	return errors.Join(err, f.Close())
}
