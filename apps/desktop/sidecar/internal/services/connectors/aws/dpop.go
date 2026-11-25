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

package aws

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"time"

	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
)

// A session's tokens are bound to its key (RFC 9449): each token request
// carries a proof signed by it, ES256 over P-256, as AWS's sign-in requires.

// newDPoPKey mints a session's key.
func newDPoPKey() *ecdsa.PrivateKey {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader) // crypto/rand never fails
	return key
}

// encodeKey is key as the entry keeps it: SEC 1, in PEM, as the AWS CLI's
// own cache does.
func encodeKey(key *ecdsa.PrivateKey) string {
	der, _ := x509.MarshalECPrivateKey(key) // a P-256 key marshals
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
}

// parseKey reads a key encodeKey wrote.
func parseKey(s string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("aws: the session's key is no PEM")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

// dpopClaims is a proof's payload: the one request it is good for.
type dpopClaims struct {
	ID       string `json:"jti"`
	Method   string `json:"htm"`
	URL      string `json:"htu"`
	IssuedAt int64  `json:"iat"`
}

// dpopProof is the proof for a POST to url at now: a JWT signed by key, whose
// header carries the public half.
func dpopProof(key *ecdsa.PrivateKey, url string, now time.Time) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{EmbedJWK: true}).WithType("dpop+jwt"),
	)
	if err != nil { // coverage:ignore a P-256 key signs ES256
		return "", err
	}
	return jwt.Signed(signer).
		Claims(dpopClaims{ID: uuid.NewString(), Method: http.MethodPost, URL: url, IssuedAt: now.Unix()}).
		Serialize()
}

// dpopMiddleware sets the request's DPoP proof, last before it is sent.
type dpopMiddleware struct {
	key *ecdsa.PrivateKey
	now func() time.Time
}

func (dpopMiddleware) ID() string { return "kubetailDPoP" }

func (m dpopMiddleware) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (
	middleware.FinalizeOutput, middleware.Metadata, error,
) {
	if req, ok := in.Request.(*smithyhttp.Request); ok {
		proof, err := dpopProof(m.key, req.URL.String(), m.now())
		if err != nil { // coverage:ignore a P-256 key signs ES256
			return middleware.FinalizeOutput{}, middleware.Metadata{}, err
		}
		req.Header.Set("DPoP", proof)
	}
	return next.HandleFinalize(ctx, in)
}
