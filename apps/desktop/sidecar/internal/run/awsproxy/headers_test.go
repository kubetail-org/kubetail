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
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// Each header boto3 sends is forwarded or read here, and any other x-amz
// header refuses the request, naming it.
func TestAnUnlistedAmzHeaderIsRefused(t *testing.T) {
	sent := http.Header{}
	for _, name := range []string{
		"Content-Type", "Content-Length", "User-Agent", "Accept-Encoding", "Expect", "X-Amz-Date",
		"X-Amz-Content-Sha256", "Amz-Sdk-Invocation-Id", "Amz-Sdk-Request", "X-Amz-Target",
		"X-Amzn-Query-Mode", "X-Amz-Checksum-Crc32", "X-Amz-Sdk-Checksum-Algorithm", "X-Amz-Checksum-Mode",
		"Authorization",
	} {
		sent.Set(name, "v")
	}
	assert.Empty(t, unlistedHeader(sent))
	for _, name := range []string{
		"X-Amz-Bypass-Governance-Retention", "X-Amz-Mfa", "X-Amz-Request-Payer", "X-Amz-Grant-Read",
		"X-Amz-Meta-Owner", "x-amzn-anything",
	} {
		h := sent.Clone()
		h.Set(name, "v")
		assert.Equal(t, http.CanonicalHeaderKey(name), unlistedHeader(h), name)
	}
}

// Only the list reaches AWS: never the sandbox's signature, its date, its
// payload hash or a token it holds.
func TestOnlyListedHeadersReachAWS(t *testing.T) {
	in := http.Header{}
	for _, name := range []string{"Authorization", "X-Amz-Date", "X-Amz-Security-Token", "X-Amz-Content-Sha256", "Cookie", "Accept-Encoding", "Range", "X-Amz-Acl"} {
		in.Set(name, "v")
	}
	out := runproxy.Outbound(in, forwarded)
	assert.Equal(t, http.Header{"Range": {"v"}, "X-Amz-Acl": {"v"}}, out)
}

// Each header that changes what a request does is drawn, by its label.
func TestTheHeadersThatChangeARequestAreDrawn(t *testing.T) {
	h := http.Header{}
	for _, d := range drawn {
		h.Set(d.name, "value of "+d.name)
	}
	h.Set("Range", "bytes=0-1")
	got := headerDetails(h)
	assert.Len(t, got, len(drawn))
	assert.Contains(t, got, runproxy.Detail{Label: "Copy source", Value: "value of X-Amz-Copy-Source"})
	for _, d := range drawn {
		assert.Contains(t, forwarded, d.name)
	}
}

// Any of four signs makes an upload a streamed one.
func TestAStreamedUploadIsSeenByAnyOfItsSigns(t *testing.T) {
	assert.False(t, streamedUpload(http.Header{"X-Amz-Content-Sha256": {"e3b0c442"}, "Content-Encoding": {"identity"}}))
	for _, h := range []http.Header{
		{"Content-Encoding": {"aws-chunked"}},
		{"Content-Encoding": {"gzip, aws-chunked"}},
		{"X-Amz-Content-Sha256": {"STREAMING-UNSIGNED-PAYLOAD-TRAILER"}},
		{"X-Amz-Decoded-Content-Length": {"5"}},
		{"X-Amz-Trailer": {"x-amz-checksum-crc32"}},
	} {
		assert.True(t, streamedUpload(h), h)
	}
}

// A multipart upload's start and each part are refused; listing parts is not.
func TestAMultipartUploadIsSeen(t *testing.T) {
	for method, query := range map[string]string{"POST": "uploads", "PUT": "partNumber=1&uploadId=u"} {
		q, _ := url.ParseQuery(query)
		assert.True(t, multipartUpload(method, q), method)
	}
	q, _ := url.ParseQuery("uploadId=u")
	assert.False(t, multipartUpload(http.MethodGet, q))
	assert.False(t, multipartUpload(http.MethodPost, q))
}
