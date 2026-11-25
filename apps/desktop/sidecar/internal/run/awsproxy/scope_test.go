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

package awsproxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testAuth = "AWS4-HMAC-SHA256 Credential=KUBETAILAB/20261117/eu-west-1/ec2/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc"

// A credential is read from Authorization, or from a presigned URL's query.
func TestTheCredentialIsReadFromEitherPlace(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", testAuth)
	c, ok := credentialOf(r)
	require.True(t, ok)
	assert.Equal(t, credential{keyID: "KUBETAILAB", region: "eu-west-1", service: "ec2"}, c)

	r = httptest.NewRequest(http.MethodGet, "/bucket/k?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=KUBETAILAB%2F20261117%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Signature=x", nil)
	c, ok = credentialOf(r)
	require.True(t, ok)
	assert.Equal(t, credential{keyID: "KUBETAILAB", region: "us-east-1", service: "s3", presigned: true}, c)
}

// None, both, another scheme and a malformed scope are refused.
func TestACredentialThatDoesNotReadIsRefused(t *testing.T) {
	for name, set := range map[string]func(r *http.Request){
		"none": func(*http.Request) {},
		"sigv4a": func(r *http.Request) {
			r.Header.Set("Authorization", "AWS4-ECDSA-P256-SHA256 Credential=KUBETAILAB/20261117/s3/aws4_request, Signature=x")
		},
		"sigv2": func(r *http.Request) { r.Header.Set("Authorization", "AWS KUBETAILAB:sig") },
		"short": func(r *http.Request) {
			r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=KUBETAILAB/20261117/eu-west-1/aws4_request, Signature=x")
		},
		"suffix": func(r *http.Request) {
			r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential=KUBETAILAB/20261117/eu-west-1/ec2/aws5_request, Signature=x")
		},
		"no cred": func(r *http.Request) {
			r.Header.Set("Authorization", "AWS4-HMAC-SHA256 SignedHeaders=host, Signature=x")
		},
		"both": func(r *http.Request) {
			r.Header.Set("Authorization", testAuth)
			r.URL.RawQuery = "X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=KUBETAILAB%2F20261117%2Fus-east-1%2Fs3%2Faws4_request"
		},
		"presigned sigv2": func(r *http.Request) { r.URL.RawQuery = "AWSAccessKeyId=KUBETAILAB&Signature=x&Expires=1" },
		"presigned other": func(r *http.Request) {
			r.URL.RawQuery = "X-Amz-Algorithm=AWS4-ECDSA-P256-SHA256&X-Amz-Credential=KUBETAILAB%2F20261117%2Fus-east-1%2Fs3%2Faws4_request"
		},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		set(r)
		_, ok := credentialOf(r)
		assert.False(t, ok, name)
	}
}

// The token is the key id less its prefix, in the grant's lower case; any
// other key id holds none.
func TestTheTokenIsTheKeyIdLessItsPrefix(t *testing.T) {
	token, ok := credential{keyID: "KUBETAILABCD"}.token()
	assert.True(t, ok)
	assert.Equal(t, "abcd", token)
	for _, id := range []string{"AKIAIOSFODNN7EXAMPLE", "KUBETAIL", "kubetailab"} {
		_, ok := credential{keyID: id}.token()
		assert.False(t, ok, id)
	}
}
