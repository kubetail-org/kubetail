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
	"cmp"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/testutil"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/sandbox"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/session"
)

// testCreds are the user's credentials, as the identity resolves them.
var testCreds = aws.Credentials{
	AccessKeyID: "AKIAIOSFODNN7EXAMPLE", SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	SessionToken: "FwoGZXIvYXdzEXAMPLESESSIONTOKEN", Source: "test", AccountID: "123456789012",
}

// identity is the proxy's Identity seam: the test credentials, or err, and a
// count of drops.
type identity struct {
	mu        sync.Mutex
	err       error
	drops     int
	noAccount bool // the credentials carry no account id
	// laterAccount, when set, is the account every read after the first carries.
	laterAccount string
	// laterKey, when set, is the access key every read after the first carries.
	laterKey string
	laterErr error // what every read after the first fails with
	calls    int
}

func (id *identity) Credentials(context.Context) (aws.Credentials, error) {
	id.mu.Lock()
	defer id.mu.Unlock()
	id.calls++
	creds := testCreds
	if id.noAccount {
		creds.AccountID = ""
	}
	if id.laterAccount != "" && id.calls > 1 {
		creds.AccountID = id.laterAccount
	}
	if id.laterKey != "" && id.calls > 1 {
		creds.AccessKeyID = id.laterKey
	}
	if id.laterErr != nil && id.calls > 1 {
		return aws.Credentials{}, id.laterErr
	}
	return creds, id.err
}

func (id *identity) Drop() {
	id.mu.Lock()
	defer id.mu.Unlock()
	id.drops++
}

func (id *identity) dropped() int {
	id.mu.Lock()
	defer id.mu.Unlock()
	return id.drops
}

// asker answers every request with approve for duration, hands each to
// asked, and each record to recorded.
type asker struct {
	approve  bool
	duration permissions.Duration
	asked    chan runproxy.Request
	recorded chan record
}

// record is one request decided with nobody asked.
type record struct {
	req    runproxy.Request
	d      permissions.Decision
	reason string
}

func (a *asker) Ask(_ context.Context, r runproxy.Request) (permissions.Answer, error) {
	a.asked <- r
	return permissions.Answer{Approved: a.approve, Duration: cmp.Or(a.duration, permissions.DurationOnce)}, nil
}

func (a *asker) Record(_ context.Context, r runproxy.Request, d permissions.Decision, reason string) error {
	a.recorded <- record{r, d, reason}
	return nil
}

// received is one request the upstream got, with its body read.
type received struct {
	host, method, uri string
	header            http.Header
	body              []byte
	signedBy          error // nil when the test credentials signed it
}

// fixture is a proxy over a grant and an upstream that records each request
// and answers as answer says, by default 200 "ok".
type fixture struct {
	t        *testing.T
	grant    *runproxy.Grant
	policyMu sync.Mutex
	policy   permissions.Policy // every account's, ask by default
	// accountModes are the accounts with a mode of their own.
	accountModes map[string]permissions.Mode
	off          atomic.Bool // the grant's switch, on until a test turns it off
	id           *identity
	asker        *asker
	buckets      *Buckets
	seen         chan received
	answer       http.HandlerFunc
	proxy        *Proxy
	upstream     *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{
		t: t, id: &identity{}, buckets: NewBuckets(), seen: make(chan received, 32),
		asker:  &asker{approve: true, asked: make(chan runproxy.Request, 8), recorded: make(chan record, 8)},
		answer: func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok") },
		policy: permissions.Policy{Mode: permissions.ModeAsk}, accountModes: map[string]permissions.Mode{},
	}
	f.grant = runproxy.NewGrant(permissions.AWS, func(context.Context) bool { return !f.off.Load() }, f.session(session.Chat), f.asker, "",
		runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8})
	t.Cleanup(f.grant.End)
	f.upstream = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.seen <- received{
			host: r.Host, method: r.Method, uri: r.RequestURI, header: r.Header.Clone(), body: body,
			signedBy: verify(r, body),
		}
		f.answer(w, r)
	}))
	t.Cleanup(f.upstream.Close)
	addr := f.upstream.Listener.Addr().String()
	client := &http.Client{Transport: &http.Transport{
		// Every host of the table dials the test's upstream.
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // the test's own upstream
		DisableCompression: true,
	}}
	f.proxy = New(f.grant, Upstream{Identity: f.id, Client: client, Buckets: f.buckets})
	return f
}

// session is a session of kind reading the fixture's policy.
func (f *fixture) session(kind session.Kind) session.Session {
	return session.Session{Kind: kind, Policy: func(_ context.Context, _ permissions.Target, account string) permissions.Policy {
		f.policyMu.Lock()
		defer f.policyMu.Unlock()
		p := f.policy
		if mode, ok := f.accountModes[account]; ok {
			p.Mode = mode
		}
		return p
	}}
}

// setAccountMode gives account a mode of its own, over setPolicy's.
func (f *fixture) setAccountMode(account string, mode permissions.Mode) {
	f.policyMu.Lock()
	defer f.policyMu.Unlock()
	f.accountModes[account] = mode
}

// setPolicy makes every account's mode mode, under rules.
func (f *fixture) setPolicy(mode permissions.Mode, rules ...permissions.Rule) {
	f.policyMu.Lock()
	defer f.policyMu.Unlock()
	f.policy = permissions.Policy{Mode: mode, Rules: rules}
}

// verify is nil when the test credentials signed r as received.
func verify(r *http.Request, body []byte) error {
	when, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		return err
	}
	scope := strings.Split(strings.SplitN(r.Header.Get("Authorization"), "Credential=", 2)[1], "/")
	region, service := scope[2], scope[3]
	again, _ := http.NewRequest(r.Method, "https://"+r.Host+r.RequestURI, bytes.NewReader(body))
	again.Header = r.Header.Clone()
	again.Header.Del("Authorization")
	err = v4.NewSigner(func(o *v4.SignerOptions) { o.DisableURIPathEscaping = service == "s3" }).
		SignHTTP(context.Background(), testCreds, again, r.Header.Get("X-Amz-Content-Sha256"), service, region, when)
	if err != nil {
		return err
	}
	if again.Header.Get("Authorization") != r.Header.Get("Authorization") {
		return errors.New("the signature does not match")
	}
	return nil
}

// sandboxRequest is a sandbox's request to service in region, signed with
// keyID. A body is a form.
func sandboxRequest(method, target, keyID, region, service, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = "127.0.0.1:4100"
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+keyID+"/20261117/"+region+"/"+service+
		"/aws4_request, SignedHeaders=host;x-amz-date, Signature=sandbox")
	r.Header.Set("X-Amz-Date", "20261117T000000Z")
	r.Header.Set("X-Amz-Content-Sha256", "sandbox-hash")
	if body != "" {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	}
	return r
}

// keyID is the run's key id for the fixture's grant.
func (f *fixture) keyID() string {
	return sandbox.AWSKeyIDPrefix + strings.ToUpper(f.grant.Token())
}

// request is the run's request to service in eu-west-1.
func (f *fixture) request(method, target, service, body string) *http.Request {
	return sandboxRequest(method, target, f.keyID(), "eu-west-1", service, body)
}

// query is the run's call of a query operation.
func (f *fixture) query(service, action string) *http.Request {
	return f.request(http.MethodPost, "/", service, "Action="+action)
}

// json is the run's call of a JSON operation.
func (f *fixture) json(service, target, body string) *http.Request {
	r := f.request(http.MethodPost, "/", service, body)
	r.Header.Set("X-Amz-Target", target)
	r.Header.Set("Content-Type", "application/x-amz-json-1.1")
	return r
}

// ec2 is the run's DescribeInstances, a read.
func (f *fixture) ec2() *http.Request { return f.query("ec2", "DescribeInstances") }

// owns makes names the login's buckets, listed just now.
func (f *fixture) owns(names ...string) {
	f.buckets.names = map[string]bool{}
	for _, name := range names {
		f.buckets.names[name] = true
	}
	f.buckets.login, f.buckets.listedAt = loginOf(testCreds), f.buckets.now()
}

// answers makes the upstream answer status and body.
func (f *fixture) answers(status int, body string) {
	f.answer = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func (f *fixture) serve(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.proxy.ServeHTTP(w, r)
	return w
}

// sent is the one request the upstream received.
func (f *fixture) sent() received {
	f.t.Helper()
	return testutil.Recv(f.t, f.seen, "the upstream request")
}

// none fails when the upstream received anything.
func (f *fixture) none() {
	f.t.Helper()
	select {
	case got := <-f.seen:
		f.t.Fatalf("the upstream received %s %s", got.method, got.uri)
	default:
	}
}

// With the switch off, the run's own key id is refused 401 and nothing reaches
// AWS.
func TestASwitchedOffPlatformIsUnauthorized(t *testing.T) {
	f := newFixture(t)
	f.off.Store(true)
	assert.Equal(t, http.StatusUnauthorized, f.serve(f.ec2()).Code)
	f.none()
}

// Only the run's own key id opens the proxy; nothing else reaches AWS.
func TestOnlyTheRunsKeyIdOpens(t *testing.T) {
	f := newFixture(t)
	sigv4a := httptest.NewRequest(http.MethodGet, "/", nil)
	sigv4a.Header.Set("Authorization", "AWS4-ECDSA-P256-SHA256 Credential="+f.keyID()+"/20261117/s3/aws4_request")
	for name, r := range map[string]*http.Request{
		"none":   httptest.NewRequest(http.MethodPost, "/", strings.NewReader("Action=DescribeInstances")),
		"wrong":  sandboxRequest(http.MethodPost, "/", sandbox.AWSKeyIDPrefix+strings.Repeat("AB", 32), "eu-west-1", "ec2", "Action=DescribeInstances"),
		"real":   sandboxRequest(http.MethodPost, "/", testCreds.AccessKeyID, "eu-west-1", "ec2", "Action=DescribeInstances"),
		"sigv2":  httptest.NewRequest(http.MethodGet, "/b?AWSAccessKeyId="+f.keyID(), nil),
		"sigv4a": sigv4a,
	} {
		assert.Equal(t, http.StatusUnauthorized, f.serve(r).Code, name)
	}
	f.grant.End()
	assert.Equal(t, http.StatusUnauthorized, f.serve(f.ec2()).Code)
	f.none()
}

// A presigned URL opens with the key id in its query, and none of its
// signature reaches AWS.
func TestAPresignedKeyIdOpens(t *testing.T) {
	f := newFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/?Action=DescribeRegions&X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential="+
		f.keyID()+"%2F20261117%2Feu-west-1%2Fec2%2Faws4_request&X-Amz-Date=20261117T000000Z&X-Amz-Expires=60"+
		"&X-Amz-SignedHeaders=host&X-Amz-Signature=s&X-Amz-Security-Token=t", nil)
	w := f.serve(r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got := f.sent()
	assert.Equal(t, "/?Action=DescribeRegions", got.uri)
	assert.NoError(t, got.signedBy)
}

// The host comes from the table and the scope alone; an unknown service or a
// region the sandbox does not reach is refused, naming what was read.
func TestTheHostComesFromTheTable(t *testing.T) {
	f := newFixture(t)
	f.owns("things")
	for name, row := range services {
		r := f.query(name, "DescribeThings")
		switch {
		case row.protocol == protocolREST:
			r = f.request(http.MethodGet, "/things", name, "")
		case row.protocol == protocolJSON:
			r = f.json(name, "Service.DescribeThings", `{}`)
		case name == "sts":
			r = f.query(name, "GetCallerIdentity")
		}
		r.Host = "evil.example"
		w := f.serve(r)
		require.Equal(t, http.StatusOK, w.Code, name+": "+w.Body.String())
		got := f.sent()
		assert.Equal(t, row.host("eu-west-1"), got.host, name)
		assert.NoError(t, got.signedBy, name)
	}
	for _, scope := range [][2]string{{"lambda", "eu-west-1"}, {"ec2", "cn-north-1"}, {"ec2", "evil.example#"}} {
		service, region := scope[0], scope[1]
		w := f.serve(sandboxRequest(http.MethodPost, "/", f.keyID(), region, service, "Action=DescribeInstances"))
		assert.Equal(t, http.StatusForbidden, w.Code, scope)
		assert.Contains(t, w.Body.String(), service+"/"+region)
	}
	f.none()
}

// Shapes the proxy will not forward are refused before AWS.
func TestARefusedShapeReachesNoUpstream(t *testing.T) {
	f := newFixture(t)
	with := func(name string, values ...string) *http.Request {
		r := f.ec2()
		r.Header[name] = values
		return r
	}
	for name, r := range map[string]*http.Request{
		"dot":                 f.request(http.MethodGet, "/bucket/../other", "s3", ""),
		"empty first segment": f.request(http.MethodHead, "//foreign-bucket/key", "s3", ""),
		"repeated target":     with("X-Amz-Target", "AmazonEC2.DescribeInstances", "AmazonEC2.TerminateInstances"),
		"repeated type":       with("Content-Type", "application/x-www-form-urlencoded", "text/plain"),
		"upgrade":             with("Upgrade", "h2c"),
		"connection":          with("Connection", "keep-alive, Upgrade"),
		"query":               f.request(http.MethodGet, "/?a=%zz", "ec2", ""),
		"header":              with("X-Amz-Mfa", "x"),
	} {
		w := f.serve(r)
		assert.Equal(t, http.StatusForbidden, w.Code, name)
		assert.Contains(t, w.Body.String(), "kubetail: ", name)
	}
	f.none()
}

// A streamed upload and a multipart one are refused with their sentence,
// before the body is read or anyone is asked.
func TestAStreamedUploadIsRefused(t *testing.T) {
	f := newFixture(t)
	for name, set := range map[string]func(r *http.Request){
		"encoding": func(r *http.Request) { r.Header.Set("Content-Encoding", "aws-chunked") },
		"hash":     func(r *http.Request) { r.Header.Set("X-Amz-Content-Sha256", "STREAMING-UNSIGNED-PAYLOAD-TRAILER") },
		"length":   func(r *http.Request) { r.Header.Set("X-Amz-Decoded-Content-Length", "5") },
		"trailer":  func(r *http.Request) { r.Header.Set("X-Amz-Trailer", "x-amz-checksum-crc32") },
		"start":    func(r *http.Request) { r.Method = http.MethodPost; r.URL.RawQuery = "uploads" },
		"part":     func(r *http.Request) { r.URL.RawQuery = "partNumber=1&uploadId=u" },
	} {
		body := &readCounter{r: strings.NewReader("hello")}
		r := f.request(http.MethodPut, "/bucket/key", "s3", "")
		r.Body = body
		set(r)
		w := f.serve(r)
		assert.Equal(t, http.StatusForbidden, w.Code, name)
		assert.Contains(t, w.Body.String(), uploadRefusal, name)
		assert.Zero(t, body.n, name)
	}
	assert.Empty(t, f.asker.asked)
	f.none()
}

// readCounter counts the bytes read of a request's body.
type readCounter struct {
	r io.Reader
	n int
}

func (c *readCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func (c *readCounter) Close() error { return nil }

// A refused operation is answered unasked.
func TestARefusedOperationReachesNoUpstream(t *testing.T) {
	f := newFixture(t)
	for service, action := range map[string]string{"sts": "AssumeRole", "ec2": "CreateKeyPair", "iam": "CreateAccessKey"} {
		w := f.serve(f.query(service, action))
		assert.Equal(t, http.StatusForbidden, w.Code, service)
		assert.Contains(t, w.Body.String(), "kubetail: "+service+":"+action+" answers a credential")
	}
	assert.Empty(t, f.asker.asked)
	f.none()
}

// An operation named where its service does not read it, or twice, is refused
// unasked: AWS would run another operation than the one classified.
func TestAnOperationNamedElsewhereReachesNoUpstream(t *testing.T) {
	f := newFixture(t)
	reading := func(r *http.Request) *http.Request {
		r.Header.Set("X-Amz-Target", "AmazonEC2.DescribeInstances")
		return r
	}
	for name, r := range map[string]*http.Request{
		"a target hiding a refused action": reading(f.query("ec2", "CreateKeyPair")),
		"a target hiding a destroy":        reading(f.query("ec2", "TerminateInstances")),
		"an action on a JSON row":          f.request(http.MethodPost, "/?Action=DescribeLogGroups", "logs", ""),
	} {
		w := f.serve(r)
		assert.Equal(t, http.StatusForbidden, w.Code, name)
		assert.Contains(t, w.Body.String(), "names its operation where", name)
	}
	assert.Empty(t, f.asker.asked)
	f.none()
}

// A read whose answer can hold a secret asks as a data read, and is refused
// unasked for the monitor, though its name reads like any other: a Logs
// Insights query's results, which may be unmasked whichever command started the
// query, and a VPN connection's sample configuration, which holds its tunnels'
// pre-shared keys.
func TestASecretReadAsksAndIsRefusedForTheMonitor(t *testing.T) {
	for name, build := range map[string]func(keyID string) *http.Request{
		"query results": func(keyID string) *http.Request {
			r := sandboxRequest(http.MethodPost, "/", keyID, "eu-west-1", "logs", `{"queryId":"q"}`)
			r.Header.Set("X-Amz-Target", "Logs_20140328.GetQueryResults")
			r.Header.Set("Content-Type", "application/x-amz-json-1.1")
			return r
		},
		"vpn configuration": func(keyID string) *http.Request {
			return sandboxRequest(http.MethodPost, "/", keyID, "eu-west-1", "ec2",
				"Action=GetVpnConnectionDeviceSampleConfiguration&VpnConnectionId=vpn-1&VpnConnectionDeviceTypeId=d")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.asker.approve = false
			assert.Equal(t, http.StatusForbidden, f.serve(build(f.keyID())).Code)
			assert.Equal(t, permissions.DataRead, testutil.Recv(t, f.asker.asked, "the request").Action.AccessLevel)

			f.setPolicy(permissions.ModeAuto)
			g := runproxy.NewGrant(permissions.AWS, func(context.Context) bool { return !f.off.Load() }, f.session(session.Monitor), f.asker, "",
				runproxy.Limits{QPS: 1000, Burst: 1000, Open: 8, Bodies: 8})
			t.Cleanup(g.End)
			w := httptest.NewRecorder()
			New(g, f.proxy.up).ServeHTTP(w, build(sandbox.AWSKeyIDPrefix+strings.ToUpper(g.Token())))
			assert.Equal(t, http.StatusForbidden, w.Code)
			assert.Empty(t, f.asker.asked, "refused unasked")
			f.none()
		})
	}
}

// A read whose request ended before a slot was free is not sent.
func TestAReadThatEndsBeforeItsSlotIsNotSent(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	w := f.serve(f.ec2().WithContext(ctx))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "kubetail: unauthorized")
	f.none()
}

// A change waits on the user, drawn with its details, and reaches AWS once
// approved; denied, it reaches nothing.
func TestOnlyAnApprovalForwardsAChange(t *testing.T) {
	f := newFixture(t)
	w := f.serve(f.query("ec2", "TerminateInstances&InstanceId.1=i-1"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	asked := testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, permissions.Destructive, asked.Action.AccessLevel)
	assert.Equal(t, "AWS: call ec2:TerminateInstances in eu-west-1", asked.Action.Summary)
	assert.Equal(t, runproxy.Sent{
		Host: "ec2.eu-west-1.amazonaws.com", Method: http.MethodPost, Path: "/",
		ContentType: "application/x-www-form-urlencoded; charset=utf-8",
		Body:        []byte("Action=TerminateInstances&InstanceId.1=i-1"),
		Details: []runproxy.Detail{
			{Label: "Service", Value: "ec2"}, {Label: "Region", Value: "eu-west-1"},
			{Label: "Operation", Value: "TerminateInstances"},
		},
	}, asked.Sent)
	assert.NoError(t, f.sent().signedBy)
	f.none()

	f.asker.approve = false
	w = f.serve(f.query("ec2", "StopInstances"))
	testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "did not approve")
	f.none()
}

// A REST request, which names no operation, is summed up by its method and
// path.
func TestARESTRequestIsSummedUpByItsPath(t *testing.T) {
	f := newFixture(t)
	f.asker.approve = false
	f.serve(f.request(http.MethodPost, "/clusters", "eks", ""))
	asked := testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, "AWS: call POST eks /clusters in eu-west-1", asked.Action.Summary)
	assert.Equal(t, []runproxy.Detail{{Label: "Service", Value: "eks"}, {Label: "Region", Value: "eu-west-1"}}, asked.Sent.Details)
}

// Each header that changes a request is drawn, and reaches AWS.
func TestTheDetailsDrawEveryHeaderThatChangesTheRequest(t *testing.T) {
	f := newFixture(t)
	f.owns("bucket")
	r := f.request(http.MethodPut, "/bucket/key", "s3", "")
	for _, d := range drawn {
		r.Header.Set(d.name, "v-"+d.label)
	}
	require.Equal(t, http.StatusOK, f.serve(r).Code)
	asked := testutil.Recv(t, f.asker.asked, "the request")
	got := f.sent()
	for _, d := range drawn {
		assert.Contains(t, asked.Sent.Details, runproxy.Detail{Label: d.label, Value: "v-" + d.label})
		assert.Equal(t, "v-"+d.label, got.header.Get(d.name))
	}
}

// A binary upload cannot be shown, so it is refused; text sent whole waits
// with its body drawn.
func TestAnUploadIsShownOrRefused(t *testing.T) {
	f := newFixture(t)
	f.owns("bucket")
	r := f.request(http.MethodPut, "/bucket/k.bin", "s3", "")
	r.Body, r.ContentLength = io.NopCloser(bytes.NewReader([]byte{0xff, 0xfe, 0x00})), 3
	w := f.serve(r)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "cannot be shown")
	f.none()

	r = f.request(http.MethodPut, "/bucket/k.txt", "s3", "")
	r.Body, r.ContentLength = io.NopCloser(strings.NewReader("hello")), 5
	r.Header.Set("X-Amz-Checksum-Crc32", "NhCmhg==")
	require.Equal(t, http.StatusOK, f.serve(r).Code)
	asked := testutil.Recv(t, f.asker.asked, "the request")
	assert.Equal(t, []byte("hello"), asked.Sent.Body)
	got := f.sent()
	assert.Equal(t, "hello", string(got.body))
	assert.Equal(t, "NhCmhg==", got.header.Get("X-Amz-Checksum-Crc32"))
	assert.NoError(t, got.signedBy)
}
