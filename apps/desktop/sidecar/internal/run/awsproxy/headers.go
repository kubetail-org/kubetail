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
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// drawn are the headers that change what a request does, each drawn as a
// detail of a request that asks. X-Amz-Target is drawn as the operation.
var drawn = []struct{ name, label string }{
	{"X-Amz-Copy-Source", "Copy source"},
	{"X-Amz-Copy-Source-Range", "Copy source range"},
	{"X-Amz-Metadata-Directive", "Metadata"},
	{"X-Amz-Tagging-Directive", "Tags from"},
	{"X-Amz-Acl", "ACL"},
	{"X-Amz-Tagging", "Tags"},
	{"X-Amz-Storage-Class", "Storage class"},
	{"X-Amz-Server-Side-Encryption", "Encryption"},
	{"X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id", "KMS key"},
}

// forwarded is every header that reaches AWS, by exact name: the drawn ones,
// those that only narrow or check a request, and those that mean nothing to
// the operation.
var forwarded = slices.Concat([]string{
	"X-Amz-Target",
	"Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since",
	"X-Amz-Expected-Bucket-Owner",
	"Content-Md5", "X-Amz-Sdk-Checksum-Algorithm", "X-Amz-Checksum-Crc32", "X-Amz-Checksum-Crc32c",
	"X-Amz-Checksum-Crc64nvme", "X-Amz-Checksum-Sha1", "X-Amz-Checksum-Sha256", "X-Amz-Checksum-Mode",
	"Content-Type", "Accept", "User-Agent", "Amz-Sdk-Invocation-Id", "Amz-Sdk-Request",
	"X-Amzn-Query-Mode",
}, drawnNames())

// signing are the x-amz headers the proxy reads and replaces with its own.
var signing = []string{"X-Amz-Date", "X-Amz-Security-Token", "X-Amz-Content-Sha256"}

func drawnNames() []string {
	names := make([]string, 0, len(drawn))
	for _, d := range drawn {
		names = append(names, d.name)
	}
	return names
}

// unlistedHeader is an x-amz header the request carries that the proxy neither
// forwards nor replaces, or "". Sent without it, AWS would run another request
// than the one the command made, so the request is refused.
func unlistedHeader(h http.Header) string {
	for _, name := range slices.Sorted(maps.Keys(h)) {
		if strings.HasPrefix(strings.ToLower(name), "x-amz") &&
			!slices.Contains(forwarded, name) && !slices.Contains(signing, name) {
			return name
		}
	}
	return ""
}

// repeatedHeader is a forwarded header the request carries more than once, or
// "". The proxy reads and draws one value, and AWS might read another.
func repeatedHeader(h http.Header) string {
	for _, name := range forwarded {
		if len(h.Values(name)) > 1 {
			return name
		}
	}
	return ""
}

// headerDetails is a line for each drawn header the request carries.
func headerDetails(h http.Header) []runproxy.Detail {
	var details []runproxy.Detail
	for _, d := range drawn {
		if v := h.Get(d.name); v != "" {
			details = append(details, runproxy.Detail{Label: d.label, Value: v})
		}
	}
	return details
}

// streamedUpload is a body framed as aws-chunked, which no one can be shown.
func streamedUpload(h http.Header) bool {
	return strings.Contains(h.Get("Content-Encoding"), "aws-chunked") ||
		strings.HasPrefix(h.Get("X-Amz-Content-Sha256"), "STREAMING-") ||
		h.Get("X-Amz-Decoded-Content-Length") != "" || h.Get("X-Amz-Trailer") != ""
}

// multipartUpload is S3's start of a multipart upload, or one of its parts. A
// part past 1 MiB cannot be sent, so a start would only leave one unfinished.
func multipartUpload(method string, query url.Values) bool {
	return method == http.MethodPost && query.Has("uploads") ||
		method == http.MethodPut && query.Has("partNumber")
}
