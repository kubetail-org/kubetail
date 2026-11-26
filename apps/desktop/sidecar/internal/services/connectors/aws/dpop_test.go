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
	"testing"
	"time"

	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/require"
)

func TestAKeyReadsBackFromItsPEM(t *testing.T) {
	key := newDPoPKey()
	pem := encodeKey(key)
	got, err := parseKey(pem)
	require.NoError(t, err)
	require.True(t, key.Equal(got))
}

func TestAKeyThatIsNoPEMIsAnError(t *testing.T) {
	_, err := parseKey("not a key")
	require.Error(t, err)
}

func TestAProofNamesTheRequestAndVerifiesUnderItsKey(t *testing.T) {
	key := newDPoPKey()
	now := time.Unix(1_800_000_000, 0)
	proof, err := dpopProof(key, "https://us-east-1.signin.aws.amazon.com/v1/token", now)
	require.NoError(t, err)

	pub, claims, err := verifyProof(proof)
	require.NoError(t, err)
	require.True(t, key.PublicKey.Equal(pub))
	require.Equal(t, "POST", claims.Method)
	require.Equal(t, "https://us-east-1.signin.aws.amazon.com/v1/token", claims.URL)
	require.Equal(t, now.Unix(), claims.IssuedAt)
	require.NotEmpty(t, claims.ID)
}

func TestEachProofHasItsOwnID(t *testing.T) {
	key := newDPoPKey()
	first, err := dpopProof(key, "https://h/v1/token", time.Now())
	require.NoError(t, err)
	second, err := dpopProof(key, "https://h/v1/token", time.Now())
	require.NoError(t, err)

	_, a, err := verifyProof(first)
	require.NoError(t, err)
	_, b, err := verifyProof(second)
	require.NoError(t, err)
	require.NotEqual(t, a.ID, b.ID)
}

// The middleware joins the stack under its own name, beside the SDK's.
func TestTheProofMiddlewareJoinsTheStackByName(t *testing.T) {
	stack := middleware.NewStack("test", smithyhttp.NewStackRequest)
	require.NoError(t, stack.Finalize.Add(dpopMiddleware{key: newDPoPKey(), now: time.Now}, middleware.After))
	_, ok := stack.Finalize.Get("kubetailDPoP")
	require.True(t, ok)
}
