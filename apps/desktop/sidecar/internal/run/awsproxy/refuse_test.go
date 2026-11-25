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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A refusal is written in the shape its service's clients read: the query
// envelope, EC2's own, the REST XML services' bare Error, or JSON, for a call
// naming X-Amz-Target and for EKS's REST JSON.
func TestARefusalIsShapedForItsService(t *testing.T) {
	f := newFixture(t)
	withMFA := func(r *http.Request) *http.Request {
		r.Header.Set("X-Amz-Mfa", "x")
		return r
	}
	for name, c := range map[string]struct {
		r                   *http.Request
		contentType, prefix string
	}{
		"query": {f.query("sts", "AssumeRole"), "text/xml",
			"?>\n<ErrorResponse><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>kubetail: "},
		"ec2": {f.query("ec2", "CreateKeyPair"), "text/xml",
			"?>\n<Response><Errors><Error><Code>AccessDenied</Code><Message>kubetail: "},
		"s3": {f.request(http.MethodGet, "/bucket?session", "s3", ""), "text/xml",
			"?>\n<Error><Code>AccessDenied</Code><Message>kubetail: "},
		"route53": {withMFA(f.request(http.MethodGet, "/2013-04-01/hostedzone", "route53", "")), "text/xml",
			"?>\n<Error><Code>AccessDenied</Code><Message>kubetail: "},
		"eks": {withMFA(f.request(http.MethodGet, "/clusters", "eks", "")), "application/json",
			`{"__type":"AccessDeniedException","message":"kubetail: `},
		"json": {f.json("ecr", "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken", `{}`), "application/x-amz-json-1.1",
			`{"__type":"AccessDeniedException","message":"kubetail: `},
	} {
		w := f.serve(c.r)
		assert.Equal(t, http.StatusForbidden, w.Code, name)
		assert.Equal(t, c.contentType, w.Header().Get("Content-Type"), name)
		assert.Contains(t, w.Body.String(), c.prefix, name)
		if c.contentType != "text/xml" {
			assert.Equal(t, "AccessDeniedException", w.Header().Get("X-Amzn-Errortype"), name)
		}
	}
	f.none()
}

// A JSON call's refusal answers in the call's own JSON type, and in 1.1 when
// it named none.
func TestAJSONRefusalKeepsTheCallsType(t *testing.T) {
	f := newFixture(t)
	r := f.json("ecr", "AmazonEC2ContainerRegistry_V20150921.GetAuthorizationToken", `{}`)
	r.Header.Set("Content-Type", "application/x-amz-json-1.0")
	assert.Equal(t, "application/x-amz-json-1.0", f.serve(r).Header().Get("Content-Type"))

	r.Header.Set("Content-Type", "text/plain")
	assert.Equal(t, "application/x-amz-json-1.1", f.serve(r).Header().Get("Content-Type"))
}

// A body past 1 MiB, and a compressed one, are refused in the row's shape,
// and reach nothing.
func TestABodyTheGrantRefusesIsAnsweredInShape(t *testing.T) {
	f := newFixture(t)
	w := f.serve(f.query("ec2", "DescribeInstances&x="+strings.Repeat("a", 1<<20)))
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	assert.Contains(t, w.Body.String(), "<Message>kubetail: the sandbox does not send a body past 1 MiB")

	r := f.json("monitoring", "GraniteServiceVersion20100801.PutMetricData", `{}`)
	r.Header.Set("Content-Encoding", "gzip")
	w = f.serve(r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), `"message":"kubetail: the sandbox does not send an encoded body`)
	f.none()
}
