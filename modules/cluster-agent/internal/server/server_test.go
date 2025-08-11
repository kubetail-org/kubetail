// Copyright 2024-2025 Andres Morey
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

package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestNewServer(t *testing.T) {
	server, err := NewServer(&config.Config{})
	assert.NoError(t, err)
	assert.NotNil(t, server)
	assert.IsType(t, &grpc.Server{}, server)
}

func TestNewServerTLSConfiguration(t *testing.T) {
	// Create temporary directory for test certificates
	tempDir := t.TempDir()

	// Generate test certificate and key
	certFile, keyFile, caFile := generateTestCerts(t, tempDir)

	tests := []struct {
		name        string
		tlsEnabled  bool
		certFile    string
		keyFile     string
		caFile      string
		clientAuth  tls.ClientAuthType
		wantErr     bool
		errContains string
	}{
		{
			name:       "TLS disabled",
			tlsEnabled: false,
			wantErr:    false,
		},
		{
			name:       "TLS enabled with valid cert and key",
			tlsEnabled: true,
			certFile:   certFile,
			keyFile:    keyFile,
			wantErr:    false,
		},
		{
			name:       "TLS with mTLS and valid CA",
			tlsEnabled: true,
			certFile:   certFile,
			keyFile:    keyFile,
			caFile:     caFile,
			clientAuth: tls.RequireAndVerifyClientCert,
			wantErr:    false,
		},
		{
			name:       "TLS with NoClientCert auth",
			tlsEnabled: true,
			certFile:   certFile,
			keyFile:    keyFile,
			clientAuth: tls.NoClientCert,
			wantErr:    false,
		},
		{
			name:       "TLS with RequestClientCert auth",
			tlsEnabled: true,
			certFile:   certFile,
			keyFile:    keyFile,
			clientAuth: tls.RequestClientCert,
			wantErr:    false,
		},
		{
			name:        "TLS enabled with missing cert file",
			tlsEnabled:  true,
			certFile:    "/nonexistent/cert.pem",
			keyFile:     keyFile,
			wantErr:     true,
			errContains: "",
		},
		{
			name:        "TLS enabled with missing key file",
			tlsEnabled:  true,
			certFile:    certFile,
			keyFile:     "/nonexistent/key.pem",
			wantErr:     true,
			errContains: "",
		},
		{
			name:        "TLS enabled with missing CA file",
			tlsEnabled:  true,
			certFile:    certFile,
			keyFile:     keyFile,
			caFile:      "/nonexistent/ca.pem",
			wantErr:     true,
			errContains: "",
		},
		{
			name:        "TLS enabled with invalid CA file",
			tlsEnabled:  true,
			certFile:    certFile,
			keyFile:     keyFile,
			caFile:      createInvalidCAFile(t, tempDir),
			wantErr:     true,
			errContains: "failed to append CA cert to pool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.ClusterAgent.TLS.Enabled = tt.tlsEnabled
			cfg.ClusterAgent.TLS.CertFile = tt.certFile
			cfg.ClusterAgent.TLS.KeyFile = tt.keyFile
			cfg.ClusterAgent.TLS.CAFile = tt.caFile
			cfg.ClusterAgent.TLS.ClientAuth = tt.clientAuth

			server, err := NewServer(cfg)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				} else {
					assert.ErrorIs(t, err, os.ErrNotExist)
				}
				assert.Nil(t, server)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, server)
				assert.IsType(t, &grpc.Server{}, server)

				// Verify TLS configuration is applied when enabled
				if tt.tlsEnabled {
					// We can't directly inspect the server's TLS config, but we can verify
					// that the server was created without error, which means the TLS config
					// was successfully applied
					assert.NotNil(t, server)
				}
			}
		})
	}
}

// generateTestCerts creates a test certificate, key, and CA file for testing
func generateTestCerts(t *testing.T, tempDir string) (certFile, keyFile, caFile string) {
	// Generate private key
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	// Create certificate template
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Test Org"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
		DNSNames:    []string{"localhost"},
	}

	// Create certificate
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	require.NoError(t, err)

	// Write certificate file
	certFile = filepath.Join(tempDir, "cert.pem")
	certOut, err := os.Create(certFile)
	require.NoError(t, err)
	defer certOut.Close()

	err = pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, err)

	// Write key file
	keyFile = filepath.Join(tempDir, "key.pem")
	keyOut, err := os.Create(keyFile)
	require.NoError(t, err)
	defer keyOut.Close()

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	require.NoError(t, err)

	err = pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})
	require.NoError(t, err)

	// Write CA file (using the same cert as CA for simplicity)
	caFile = filepath.Join(tempDir, "ca.pem")
	caOut, err := os.Create(caFile)
	require.NoError(t, err)
	defer caOut.Close()

	err = pem.Encode(caOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	require.NoError(t, err)

	return certFile, keyFile, caFile
}

// createInvalidCAFile creates a file with invalid PEM content for testing
func createInvalidCAFile(t *testing.T, tempDir string) string {
	invalidCAFile := filepath.Join(tempDir, "invalid_ca.pem")
	err := os.WriteFile(invalidCAFile, []byte("invalid pem content"), 0644)
	require.NoError(t, err)
	return invalidCAFile
}
