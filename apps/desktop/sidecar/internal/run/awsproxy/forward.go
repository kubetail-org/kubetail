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
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// forward sends the request to its row's host, signed with creds, and
// writes the answer: streamed, or, for an error, read and checked first.
func (p *Proxy) forward(w http.ResponseWriter, req *request, creds aws.Credentials) {
	ctx := req.r.Context()
	header := runproxy.Outbound(req.r.Header, forwarded)
	if req.owner != "" {
		header.Set("X-Amz-Expected-Bucket-Owner", req.owner)
	}
	resp, err := p.send(ctx, creds, outbound{
		method: req.r.Method, host: req.row.host(req.cred.region), target: req.target(),
		service: req.cred.service, region: req.row.signingRegion(req.cred.region),
		body: req.body, header: header,
	})
	if err != nil {
		refuse(w, req, http.StatusBadGateway, upstreamFailed)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusBadRequest {
		writeHeader(w, resp)
		stream(w, resp.Body)
		return
	}
	body, err := p.readError(resp)
	if err != nil || len(body) > maxErrorBody || holdsAny(body, creds.AccessKeyID, creds.SessionToken) {
		refuse(w, req, http.StatusBadGateway, uncheckable)
		return
	}
	writeHeader(w, resp)
	_, _ = w.Write(body)
}

// readError reads an error answer, up to one byte past maxErrorBody, and drops
// the credential when its code says AWS holds it dead. The code is read before
// anything decides whether the answer may be passed back: one naming a dead
// credential, as S3's InvalidAccessKeyId does by echoing it, never is.
func (p *Proxy) readError(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
	if slices.Contains(deadCodes, errorCode(resp.Header, body)) {
		// The next login may be another account's, so its buckets are listed
		// anew.
		p.up.Identity.Drop()
		p.up.Buckets.forget()
	}
	return body, err
}

// outbound is a request the proxy builds for AWS.
type outbound struct {
	method, host, target string
	service, region      string // what it is signed for
	body                 []byte
	header               http.Header
}

// signer keeps the keys it derives, so every request shares it.
var signer = v4.NewSigner()

// send builds o, signs it with creds and sends it over HTTPS.
func (p *Proxy) send(ctx context.Context, creds aws.Credentials, o outbound) (*http.Response, error) {
	out, err := http.NewRequestWithContext(ctx, o.method, "https://"+o.host+o.target, bytes.NewReader(o.body))
	if err != nil { // coverage:ignore the host is the table's and the target a parsed request's
		return nil, err
	}
	if len(o.body) == 0 {
		out.Body = http.NoBody
	}
	out.Header = o.header
	out.Header.Set("User-Agent", version.Prefix(out.Header.Get("User-Agent")))
	sum := sha256.Sum256(o.body)
	hash := hex.EncodeToString(sum[:])
	out.Header.Set("X-Amz-Content-Sha256", hash)
	// S3 signs the path as sent; every other service signs it escaped again.
	err = signer.SignHTTP(ctx, creds, out, hash, o.service, o.region, time.Now(),
		func(opts *v4.SignerOptions) { opts.DisableURIPathEscaping = o.service == "s3" })
	if err != nil { // coverage:ignore the signer fails only on a request it cannot read, which this is not
		return nil, err
	}
	return p.up.Client.Do(out)
}

// writeHeader writes resp's status and headers.
func writeHeader(w http.ResponseWriter, resp *http.Response) {
	maps.Copy(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
}

// stream copies body to w as it arrives, so a long download reaches the client
// while it runs. A copy that fails aborts the response: returning would end a
// chunked answer cleanly, and the client would take a cut body for a whole one.
func stream(w http.ResponseWriter, body io.Reader) {
	if _, err := io.Copy(flusher{w, http.NewResponseController(w)}, body); err != nil {
		panic(http.ErrAbortHandler)
	}
}

// flusher flushes each write.
type flusher struct {
	w  io.Writer
	rc *http.ResponseController
}

func (f flusher) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	_ = f.rc.Flush()
	return n, err
}

// maxErrorBody is the most of an error answer the proxy reads to check it.
const maxErrorBody = 64 << 10

// minSecretRun is the shortest run of a secret's letters and digits that
// holdsAny looks for on its own.
const minSecretRun = 20

// holdsAny is whether body holds any of values: whole, or any run of at least
// minSecretRun of its letters and digits. AWS echoes the key id in a signature
// error, and a JSON or URL escape changes only the characters between runs.
func holdsAny(body []byte, values ...string) bool {
	for _, v := range values {
		if v != "" && bytes.Contains(body, []byte(v)) {
			return true
		}
		for _, run := range strings.FieldsFunc(v, func(c rune) bool { return !unicode.IsLetter(c) && !unicode.IsDigit(c) }) {
			if len(run) >= minSecretRun && bytes.Contains(body, []byte(run)) {
				return true
			}
		}
	}
	return false
}

// deadCodes are the codes AWS answers a dead credential with, as a 400 or a
// 403 rather than a 401.
var deadCodes = []string{
	"ExpiredToken", "ExpiredTokenException", "InvalidClientTokenId", "UnrecognizedClientException",
	"InvalidSignatureException", "InvalidAccessKeyId", "InvalidToken", "AuthFailure",
}

var xmlCodeRE = regexp.MustCompile(`<Code>([^<]*)</Code>`)

// errorCode is an error answer's code: the JSON protocols' header or body,
// else the XML's first Code. A JSON code may follow its namespace and a "#".
func errorCode(h http.Header, body []byte) string {
	if t := h.Get("X-Amzn-Errortype"); t != "" {
		code, _, _ := strings.Cut(t, ":")
		return code
	}
	var j struct {
		Type string `json:"__type"`
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &j) == nil && (j.Type != "" || j.Code != "") {
		code := j.Type + j.Code
		if _, after, ok := strings.Cut(code, "#"); ok {
			return after
		}
		return code
	}
	if m := xmlCodeRE.FindSubmatch(body); m != nil {
		return string(m[1])
	}
	return ""
}
