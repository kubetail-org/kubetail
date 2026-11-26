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
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The certificate names 127.0.0.1 alone, with the profile a strict verifier
// asks for, and verifies as its own root.
func TestTheCertificateNamesTheLoopbackAlone(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	c, err := NewCertificate(now)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(c.TLS.Certificate[0])
	require.NoError(t, err)

	assert.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4()}, cert.IPAddresses)
	assert.Empty(t, cert.DNSNames)
	assert.Empty(t, cert.URIs)
	assert.Empty(t, cert.EmailAddresses)
	assert.True(t, cert.BasicConstraintsValid)
	assert.False(t, cert.IsCA)
	assert.Equal(t, x509.KeyUsageDigitalSignature, cert.KeyUsage)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, cert.ExtKeyUsage)
	assert.NotEmpty(t, cert.SubjectKeyId)
	assert.Equal(t, cert.SubjectKeyId, cert.AuthorityKeyId)
	for _, ext := range cert.Extensions {
		if ext.Id.Equal([]int{2, 5, 29, 19}) {
			assert.True(t, ext.Critical, "basic constraints are critical")
		}
	}
	assert.Equal(t, now.Add(-time.Hour), cert.NotBefore)
	assert.Equal(t, now.Add(-time.Hour).AddDate(0, 0, 365), cert.NotAfter)
	key, ok := cert.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, elliptic.P256(), key.Curve)

	block, _ := pem.Decode(c.PEM)
	require.NotNil(t, block)
	assert.Equal(t, c.TLS.Certificate[0], block.Bytes)

	roots := x509.NewCertPool()
	roots.AddCert(cert)
	_, err = cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now})
	assert.NoError(t, err)
	assert.NoError(t, cert.VerifyHostname("127.0.0.1"))
	assert.Error(t, cert.VerifyHostname("localhost"))
}

// The bundle holds the certificate, then the first system bundle that
// exists; with none, the certificate alone. It is owner-only and never
// replaces a file.
func TestTheBundleKeepsTheSystemsRoots(t *testing.T) {
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	dir := t.TempDir()
	system := filepath.Join(dir, "system.pem")
	require.NoError(t, os.WriteFile(system, []byte("system roots\n"), 0o644))

	bundle := filepath.Join(dir, "ca.pem")
	require.NoError(t, c.WriteBundle(bundle, []string{filepath.Join(dir, "missing.pem"), system}))
	got, err := os.ReadFile(bundle)
	require.NoError(t, err)
	assert.Equal(t, string(c.PEM)+"system roots\n", string(got))
	assert.Error(t, c.WriteBundle(bundle, nil), "an existing file is never replaced")

	alone := filepath.Join(dir, "alone.pem")
	require.NoError(t, c.WriteBundle(alone, []string{filepath.Join(dir, "missing.pem")}))
	got, err = os.ReadFile(alone)
	require.NoError(t, err)
	assert.Equal(t, c.PEM, got)
}

// A system bundle that cannot be read is passed over for the next.
func TestAnUnreadableBundleIsPassedOver(t *testing.T) {
	c, err := NewCertificate(time.Now())
	require.NoError(t, err)
	dir := t.TempDir()
	system := filepath.Join(dir, "system.pem")
	require.NoError(t, os.WriteFile(system, []byte("system roots\n"), 0o644))
	bundle := filepath.Join(dir, "ca.pem")
	require.NoError(t, c.WriteBundle(bundle, []string{dir, system}), "a directory where a bundle should be")
	got, err := os.ReadFile(bundle)
	require.NoError(t, err)
	assert.Equal(t, string(c.PEM)+"system roots\n", string(got))
}
