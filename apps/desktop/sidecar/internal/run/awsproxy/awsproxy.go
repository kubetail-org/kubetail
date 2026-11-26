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

// Package awsproxy is the proxy a sandboxed command reaches AWS through. The
// run's key id is its grant's token; the proxy reads the service and region
// from the request's SigV4 scope, classifies the operation, drops the
// sandbox's signature and signs the request it builds with the user's
// credentials, for a host of its own table.
package awsproxy

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
)

// Identity is the user's AWS login, resolved outside the sandbox.
type Identity interface {
	// Credentials' failure is shown to the run when it is a Refusal, and
	// otherwise as noLogin.
	Credentials(ctx context.Context) (aws.Credentials, error)
	// Drop forgets the credentials, which AWS said are dead.
	Drop()
}

// A Refusal is a failure whose Refusal text is the run's to read: it names no
// file, account or credential.
type Refusal interface {
	Refusal() string
}

// Upstream is what the proxy reaches AWS with.
type Upstream struct {
	Identity Identity
	Client   *http.Client
	// Buckets are the login's own, shared by every run of one login.
	Buckets *Buckets
}

// Proxy is one run's AWS route.
type Proxy struct {
	grant *runproxy.Grant
	up    Upstream
}

// New is the proxy over grant. It follows no redirect: one is the client's to
// read.
func New(grant *runproxy.Grant, up Upstream) *Proxy {
	client := *up.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	up.Client = &client
	return &Proxy{grant: grant, up: up}
}

// request is one request as the handler has read it so far.
type request struct {
	r       *http.Request
	cred    credential
	row     service
	body    []byte
	op      operation
	foreign bool   // it names a bucket the login does not own
	owner   string // the account its bucket is sent as owned by, or ""
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cred, ok := credentialOf(r)
	token, held := cred.token()
	if !ok || !held {
		token = ""
	}
	r, done, ok := p.grant.Admit(w, r, token)
	if !ok {
		return
	}
	defer done()

	req := &request{r: r, cred: cred}
	row, known := services[cred.service]
	if !known || !sandbox.AWSRegion(cred.region) {
		refuse(w, req, http.StatusForbidden, "kubetail: the sandbox does not reach "+cred.service+"/"+cred.region+
			"; run it outside the sandbox")
		return
	}
	req.row = row
	if why := refusedShape(r, row); why != "" {
		refuse(w, req, http.StatusForbidden, why)
		return
	}
	body, release, status := p.grant.Body(w, r)
	defer release()
	if status != 0 {
		refuse(w, req, status, runproxy.BodyRefusal(status))
		return
	}
	req.body = body

	req.op = classify(cred.service, r, body)
	if req.op.refusal != "" {
		refuse(w, req, http.StatusForbidden, req.op.refusal)
		return
	}
	// A bucket's owner, the decision and a read's forward are read off one
	// reading of the credentials, so all name one login.
	creds, err := p.up.Identity.Credentials(r.Context())
	if err != nil {
		refuse(w, req, http.StatusForbidden, noLoginRefusal(err))
		return
	}
	// A caller's expected owner other than the login's account names another
	// owner than the one the list vouches for.
	expected := r.Header.Get("X-Amz-Expected-Bucket-Owner")
	if req.op.bucket != "" && (expected != "" && expected != creds.AccountID ||
		!p.up.Buckets.owns(r.Context(), creds, cred.region, req.op.bucket, p.listBuckets)) {
		// Its owner's log would record the read, so it asks.
		req.foreign = true
		if req.op.level == permissions.Read {
			req.op.level, req.op.once = permissions.Write, true
		}
	}
	if req.op.bucket != "" && !req.foreign {
		// S3 refuses the request if the bucket changed hands since the list.
		req.owner = creds.AccountID
	}
	if req.op.level == permissions.Read {
		release, status := p.grant.Slot(r.Context())
		defer release()
		if status != 0 {
			refuse(w, req, status, runproxy.SlotRefusal(status))
			return
		}
		p.forward(w, req, creds)
		return
	}
	// The forward reads the credentials again, since a session can expire
	// while the user decides, and sends nothing as another login: another
	// account, or with no account id, another access key.
	out := p.grant.Decide(r.Context(), runproxy.Request{Action: req.action(creds), Sent: req.sent()}, func() error {
		fresh, err := p.up.Identity.Credentials(r.Context())
		switch {
		case err != nil:
			refuse(w, req, http.StatusForbidden, noLoginRefusal(err))
		case loginOf(fresh) != loginOf(creds):
			refuse(w, req, http.StatusForbidden, loginChanged)
		default:
			p.forward(w, req, fresh)
		}
		return nil
	})
	if !out.Forwarded {
		refuse(w, req, out.Status, out.Message)
	}
}

// refusedShape is why a request is not forwarded, whatever it does, or "".
func refusedShape(r *http.Request, row service) string {
	switch {
	case slices.ContainsFunc(strings.Split(r.URL.Path, "/"), func(s string) bool { return s == "." || s == ".." }):
		return "kubetail: the sandbox does not send a path with . or .. segments"
	case strings.HasPrefix(r.URL.Path, "//"):
		// S3 reads a bucket from the first segment, which here is empty.
		return "kubetail: the sandbox does not send a path that starts with an empty segment"
	case runproxy.Upgrades(r.Header):
		return "kubetail: the sandbox does not upgrade a connection to AWS"
	case !parses(r.URL.RawQuery):
		return "kubetail: this request's query does not parse, so it cannot be shown as it would be sent"
	case streamedUpload(r.Header), row.protocol == protocolREST && multipartUpload(r.Method, r.URL.Query()):
		return uploadRefusal
	}
	if name := unlistedHeader(r.Header); name != "" {
		return "kubetail: the sandbox does not send the header " + name + ", and AWS would run another request without it"
	}
	if name := repeatedHeader(r.Header); name != "" {
		return "kubetail: the sandbox does not send the header " + name + " twice: AWS could read a value other than the one shown"
	}
	return ""
}

func parses(query string) bool {
	_, err := url.ParseQuery(query)
	return err == nil
}

// target is the path and query as received, less a presigned URL's
// signature.
func (req *request) target() string {
	u := *req.r.URL
	if req.cred.presigned {
		query := u.Query()
		for _, name := range presignParams {
			query.Del(name)
		}
		u.RawQuery = query.Encode()
	}
	return u.RequestURI()
}

// summary is the request in one line.
func (req *request) summary() string {
	what := req.cred.service + ":" + req.op.name
	if req.op.name == "" {
		what = req.r.Method + " " + req.cred.service + " " + req.r.URL.Path
	}
	return "AWS: call " + what + " in " + req.cred.region
}

// The labels of a request's details.
const (
	labelService     = "Service"
	labelRegion      = "Region"
	labelOperation   = "Operation"
	labelBucketOwner = "Bucket owner"
	notThisLogin     = "not this login's account"
)

// sent is the request as it will reach AWS, and the lines the classifier
// read of it.
func (req *request) sent() runproxy.Sent {
	details := []runproxy.Detail{{Label: labelService, Value: req.cred.service}, {Label: labelRegion, Value: req.cred.region}}
	if req.op.name != "" {
		details = append(details, runproxy.Detail{Label: labelOperation, Value: req.op.name})
	}
	if req.foreign {
		details = append(details, runproxy.Detail{Label: labelBucketOwner, Value: notThisLogin})
	}
	return runproxy.Sent{
		Host:        req.row.host(req.cred.region),
		Method:      req.r.Method,
		Path:        req.target(),
		ContentType: req.r.Header.Get("Content-Type"),
		Body:        req.body,
		Details:     append(details, headerDetails(req.r.Header)...),
	}
}
