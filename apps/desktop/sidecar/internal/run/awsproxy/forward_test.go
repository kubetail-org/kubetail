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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// A read is signed anew: nothing of the sandbox's signature, key id, date or
// payload hash arrives.
func TestAReadIsSignedAnew(t *testing.T) {
	f := newFixture(t)
	w := f.serve(f.query("ec2", "DescribeInstances&Version=2016-11-15"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "ok", w.Body.String())
	got := f.sent()
	assert.NoError(t, got.signedBy)
	assert.Equal(t, "Action=DescribeInstances&Version=2016-11-15", string(got.body))
	assert.Equal(t, testCreds.SessionToken, got.header.Get("X-Amz-Security-Token"))
	for _, v := range []string{f.keyID(), "sandbox", "20261117T000000Z"} {
		for name, values := range got.header {
			assert.NotContains(t, strings.Join(values, ","), v, name)
		}
	}
}

// AWS reads Kubetail ahead of the command's agent, and Kubetail alone on the
// bucket list the proxy makes itself.
func TestKubetailIsNamedToAWS(t *testing.T) {
	f := newFixture(t)
	r := f.ec2()
	r.Header.Set("User-Agent", "aws-cli/2.31.0")
	require.Equal(t, http.StatusOK, f.serve(r).Code)
	got := f.sent()
	assert.Equal(t, version.UserAgent()+" aws-cli/2.31.0", got.header.Get("User-Agent"))
	assert.NoError(t, got.signedBy)

	f.listing(func() []string { return []string{"mine"} })
	require.Equal(t, http.StatusOK, f.serve(f.list("mine")).Code)
	list := f.sent()
	require.Contains(t, list.uri, "max-buckets")
	assert.Equal(t, version.UserAgent(), list.header.Get("User-Agent"))
}

// A global row signs in us-east-1 whatever region the client signed for.
func TestAGlobalRowSignsInUsEast1(t *testing.T) {
	f := newFixture(t)
	require.Equal(t, http.StatusOK, f.serve(f.query("iam", "ListRoles")).Code)
	got := f.sent()
	assert.Contains(t, got.header.Get("Authorization"), "/us-east-1/iam/aws4_request")
	assert.NoError(t, got.signedBy)
}

// An S3 key's path reaches AWS as the client escaped it.
func TestAnS3KeyKeepsItsEscaping(t *testing.T) {
	f := newFixture(t)
	f.owns("bucket")
	require.Equal(t, http.StatusOK, f.serve(f.request(http.MethodHead, "/bucket/a%20b/c%2Bd", "s3", "")).Code)
	got := f.sent()
	assert.Equal(t, "/bucket/a%20b/c%2Bd", got.uri)
	assert.NoError(t, got.signedBy)
}

// An answer AWS encodes arrives as encoded: an object stored gzipped keeps
// the checksum its client checks.
func TestAGzippedObjectArrivesAsStored(t *testing.T) {
	f := newFixture(t)
	f.owns("bucket")
	gzipped := []byte{0x1f, 0x8b, 0x08}
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipped)
	}
	w := f.serve(f.request(http.MethodGet, "/bucket/k.gz", "s3", ""))
	f.sent()
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, gzipped, w.Body.Bytes())
}

// A body that fails partway aborts the response rather than ending it.
func TestAStreamThatFailsAborts(t *testing.T) {
	w := httptest.NewRecorder()
	body := io.MultiReader(strings.NewReader("part"), iotest.ErrReader(errors.New("cut")))
	assert.PanicsWithValue(t, http.ErrAbortHandler, func() { stream(w, body) })
	assert.Equal(t, "part", w.Body.String())
}

// An upstream that cuts its answer partway reaches the client as a cut
// answer, never a whole one.
func TestACutUpstreamAnswerReachesTheClientCut(t *testing.T) {
	f := newFixture(t)
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<DescribeInstancesResponse>")
		_ = http.NewResponseController(w).Flush()
		conn, _, err := http.NewResponseController(w).Hijack()
		require.NoError(t, err)
		_ = conn.Close()
	}
	proxy := httptest.NewServer(f.proxy)
	t.Cleanup(proxy.Close)
	in := f.ec2()
	out, err := http.NewRequestWithContext(t.Context(), in.Method, proxy.URL+"/", strings.NewReader("Action=DescribeInstances"))
	require.NoError(t, err)
	out.Header = in.Header
	resp, err := proxy.Client().Do(out)
	require.NoError(t, err)
	defer resp.Body.Close()
	f.sent()
	got, err := io.ReadAll(resp.Body)
	assert.Error(t, err)
	assert.Equal(t, "<DescribeInstancesResponse>", string(got))
}

// A secret is found however the error escapes it: JSON writes "/" as "\/"
// and "+" as "\u002b", a URL as "%2F" and "%2B".
func TestAnEscapedSecretIsFound(t *testing.T) {
	token := "IQoJb3JpZ2luX2VjEXAMPLE/abcdefghijklmnopqrstu+vwxyzABCDEFGHIJKLMNOP=="
	for _, body := range []string{
		token,
		`{"message":"IQoJb3JpZ2luX2VjEXAMPLE\/abcdefghijklmnopqrstu\u002bvwxyz"}`,
		"token=IQoJb3JpZ2luX2VjEXAMPLE%2Fabcdefghijklmnopqrstu%2Bvwxyz",
	} {
		assert.True(t, holdsAny([]byte(body), "", token), body)
	}
	assert.False(t, holdsAny([]byte("<Code>AccessDenied</Code>"), token))
}

// A redirect is passed back unfollowed: it is the client's to read.
func TestARedirectIsPassedBack(t *testing.T) {
	f := newFixture(t)
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://elsewhere.example/")
		w.WriteHeader(http.StatusMovedPermanently)
	}
	w := f.serve(f.ec2())
	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "https://elsewhere.example/", w.Header().Get("Location"))
	f.sent()
	f.none()
}

// An upstream that cannot be reached is a 502 that carries no error text, and
// a login that cannot be resolved is its Refusal, else one fixed sentence.
func TestAnUpstreamFailureCarriesNoText(t *testing.T) {
	f := newFixture(t)
	f.upstream.Close()
	w := f.serve(f.ec2())
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), upstreamFailed)
	assert.NotContains(t, w.Body.String(), "dial")

	f = newFixture(t)
	f.id.err = errors.New("sso: token expired at /home/u/.aws/sso/cache/x.json")
	w = f.serve(f.ec2())
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "kubetail: no AWS login was found; run `aws sso login` outside the sandbox")
	assert.NotContains(t, w.Body.String(), "token expired")
	f.none()

	f = newFixture(t)
	f.id.err = fmt.Errorf("wrapped: %w", testRefusal("no AWS login was found for the profile prod"))
	w = f.serve(f.ec2())
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "kubetail: no AWS login was found for the profile prod<")
	assert.NotContains(t, w.Body.String(), "wrapped")
	f.none()
}

// testRefusal is a login's failure worded for the run.
type testRefusal string

func (r testRefusal) Error() string   { return "wrapped: " + string(r) }
func (r testRefusal) Refusal() string { return string(r) }

// An error answer is checked before it is passed back: one too long to check,
// or holding the user's key id or session token, is a 502.
func TestAnErrorAnswerIsChecked(t *testing.T) {
	f := newFixture(t)
	for body, want := range map[string]int{
		"<Error><Code>AccessDenied</Code></Error>":                                          http.StatusForbidden,
		"<Error><Code>SignatureDoesNotMatch</Code><AWSAccessKeyId>" + testCreds.AccessKeyID: http.StatusBadGateway,
		"<Error><Message>" + testCreds.SessionToken + "</Message></Error>":                  http.StatusBadGateway,
		strings.Repeat("x", maxErrorBody+1):                                                 http.StatusBadGateway,
	} {
		f.answers(http.StatusForbidden, body)
		w := f.serve(f.ec2())
		f.sent()
		assert.Equal(t, want, w.Code, body[:min(len(body), 40)])
		if want == http.StatusForbidden {
			assert.Equal(t, body, w.Body.String())
		}
		assert.NotContains(t, w.Body.String(), testCreds.AccessKeyID)
		assert.NotContains(t, w.Body.String(), testCreds.SessionToken)
	}
	assert.Zero(t, f.id.dropped())
}

// AWS answers a dead credential with a code, which drops it.
func TestADeadCredentialIsDropped(t *testing.T) {
	f := newFixture(t)
	for i, code := range deadCodes {
		f.answers(http.StatusForbidden, "<Response><Errors><Error><Code>"+code+"</Code></Error></Errors></Response>")
		f.serve(f.ec2())
		f.sent()
		assert.Equal(t, i+1, f.id.dropped(), code)
	}
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Amzn-Errortype", "ExpiredTokenException:http://internal.amazon.com/")
		w.WriteHeader(http.StatusBadRequest)
	}
	f.serve(f.json("logs", "Logs_20140328.DescribeLogGroups", `{}`))
	f.sent()
	assert.Equal(t, len(deadCodes)+1, f.id.dropped(), "the JSON protocols' header")
}

// A dead credential's error that echoes the user's key id is not passed back,
// and the credential is still dropped.
func TestADeadCredentialEchoingItsKeyIsDropped(t *testing.T) {
	f := newFixture(t)
	f.answers(http.StatusForbidden, "<Error><Code>InvalidAccessKeyId</Code><AWSAccessKeyId>"+testCreds.AccessKeyID+"</AWSAccessKeyId></Error>")
	w := f.serve(f.ec2())
	f.sent()
	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.NotContains(t, w.Body.String(), testCreds.AccessKeyID)
	assert.Equal(t, 1, f.id.dropped())
}

// The codes AWS answers a dead credential with are these eight.
func TestTheDeadCodesAreListed(t *testing.T) {
	assert.Equal(t, []string{
		"ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId", "UnrecognizedClientException",
		"InvalidSignatureException", "InvalidAccessKeyId", "InvalidToken", "AuthFailure",
	}, deadCodes)
}

// An error's code is read from each protocol's shape.
func TestErrorCodeReadsEachProtocol(t *testing.T) {
	for body, want := range map[string]string{
		`{"__type":"com.amazon.coral#ExpiredTokenException"}`: "ExpiredTokenException",
		`{"__type":"ExpiredTokenException"}`:                  "ExpiredTokenException",
		`{"code":"InvalidToken"}`:                             "InvalidToken",
		`<Response><Errors><Error><Code>AuthFailure</Code>`:   "AuthFailure",
		"busy": "",
	} {
		assert.Equal(t, want, errorCode(http.Header{}, []byte(body)), body)
	}
	assert.Equal(t, "Throttling", errorCode(http.Header{"X-Amzn-Errortype": {"Throttling:http://x"}}, nil))
}
