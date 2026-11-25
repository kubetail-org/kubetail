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

// The connection a holder talks to one server over, and everything that happens across it:
// building one, retiring one, the raw-path request the probes read an endpoint with, and what a
// failed one means in this package's reason vocabulary. The connection probe builds them; the
// pool retires them.
package kubeconn

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/version"
)

// ErrNoConnection reports that there is no connection to hand out: a context that does not
// resolve, or one whose connection has yet to be built.
var ErrNoConnection = errors.New("no connection for kube-context")

// ErrIdentityMismatch reports a connection that is not the cluster the caller asked for —
// re-pointed at another, or not identified yet. Told apart from ErrNoConnection because the
// remedy differs: an outage passes on its own, a context now naming another cluster does not.
var ErrIdentityMismatch = errors.New("connection is not the requested cluster")

// The tuning every connection carries. The credentials come from the user's file; the budget
// and the name we dial under are this package's to set.
const (
	defaultQPS   float32 = 50
	defaultBurst int     = 100
	// listIdleTimeout is the read-inactivity window on one non-watch request. A kind sync
	// holds its start slot until its watch is open, i.e. past the cold list, so one wedged
	// LIST costs a permanent fraction of the fleet's start capacity.
	listIdleTimeout = 2 * time.Minute
)

// Connection is one identity and the clients built over the credentials reaching it. The clients
// share one http.Client, so they share one connection pool — with HTTP/2 that is a single TCP
// connection carrying every concurrent request to that API server.
//
// The clients and the config never change after it is built — a caller that mutates Config is
// editing what every other holder is using. The identity below is the one thing written after,
// once, by the probe that confirms it.
type Connection struct {
	// Config is what the clients below were built from. Exposed for the client-go
	// constructors that take one and build their own transport (exec, port-forward);
	// anything that can use Dynamic or HTTPClient should.
	Config *rest.Config
	// BaseURL is Config.Host resolved to an absolute URL, carrying the scheme and any path
	// prefix; APIPath is the versioned path beside it. Raw paths join onto them.
	BaseURL *url.URL
	APIPath string
	// HTTPClient is authenticated and pooled. Raw API paths go through it directly, and
	// unthrottled: the QPS bucket lives in rest.RESTClient, so it reaches Dynamic alone.
	HTTPClient *http.Client
	Dynamic    dynamic.Interface

	// serverUID is the cluster reached over THIS connection, recorded by the probe that read
	// it. Unset until then, which reads as not identified — never as a match.
	//
	// Here rather than beside the connection in the supervisor's observable, because the probe
	// that made the request is the only party that knows which connection the answer came
	// over. Anything that pairs the two later is re-deriving it, and a connection replaced
	// between the two commits pairs a new one with the old one's answer.
	identity atomic.Pointer[connIdentity]

	// seq identifies this connection among every one this process builds, so that
	// "the connection was replaced" is a fact a reader can compare rather than infer.
	// Immutable, unlike identity: the stamp lands a round trip after the build, and a
	// news value derived from it alone cannot tell a rebuild from a re-stamp of the same
	// uid — which is exactly what a rotation for an unchanged cluster looks like.
	seq uint64

	// done closes when this connection is retired. Nil in a connection nobody built, which
	// reads as never retired.
	done chan struct{}
	once sync.Once

	// counts is fed by the dialer under every client built from Config. Its own allocation:
	// client-go's transport cache keys on the dialer, which must not keep the connection alive.
	// Nil in a connection nobody built.
	counts *byteCounts
}

// Bytes is the wire bytes this connection has received and sent since it was built: TLS
// ciphertext, headers and framing included. Every socket dialed from Config counts, so an
// upgraded stream (exec, port-forward) does too.
func (c *Connection) Bytes() (received, sent uint64) {
	if c.counts == nil {
		return 0, 0
	}
	return c.counts.received.Load(), c.counts.sent.Load()
}

// connSeq numbers the connections this process builds. Only ever compared, never read as
// a count.
var connSeq atomic.Uint64

// Seq identifies this connection. Zero for a connection nobody built, which no live one
// shares.
func (c *Connection) Seq() uint64 { return c.seq }

// connIdentity is what a connection has been confirmed as: the cluster read over it, and
// whether it still vouches for that. One value behind one pointer, so the transition from
// vouching to not is a single word — two fields a reader loaded separately would let a caller
// through between them, holding the old uid against the replacement server.
type connIdentity struct {
	uid string
	// conflicted means a second, different uid was read over this connection since.
	conflicted bool
}

// ServerUID is the cluster this connection vouches for, and whether it vouches for one. A
// connection nobody has identified yet answers ("", false), and so does one whose server
// changed underneath it — a caller scoped to one cluster must refuse both rather than assume.
func (c *Connection) ServerUID() (string, bool) {
	id := c.identity.Load()
	if id == nil || id.conflicted {
		return "", false
	}
	return id.uid, true
}

// IdentityFor reports whether this connection may serve work scoped to serverUID, and why not.
// The one place the three answers are told apart — vouching for another cluster, vouching for
// none yet, vouching for none any more — so a caller cannot re-derive them from the parts and
// drift.
func (c *Connection) IdentityFor(serverUID string) error {
	id := c.identity.Load()
	switch {
	case id == nil:
		return fmt.Errorf("%w: the connection has not said which server it reached", ErrIdentityMismatch)
	case id.conflicted:
		return fmt.Errorf("%w: the server behind the connection was replaced, so it vouches for neither cluster", ErrIdentityMismatch)
	case id.uid != serverUID:
		return fmt.Errorf("%w: the connection reached %s, not %s", ErrIdentityMismatch, id.uid, serverUID)
	}
	return nil
}

// conflicted reports that a second, different uid was read over this connection — the server
// behind it was replaced. Distinct from `ServerUID`'s ("", false), which cannot tell that from a
// connection nothing has identified yet, and only the former is worth rebuilding for.
func (c *Connection) conflicted() bool {
	id := c.identity.Load()
	return id != nil && id.conflicted
}

// setServerUID records the cluster a probe reached over this connection.
//
// The stamp is never overwritten. A second, different uid is the server being replaced behind
// unchanged credentials, and **both identities become untrustworthy over this connection**:
// whoever asked for the old one would be handed the new server, and the new one cannot be
// vouched for by a connection that has already answered as something else. So the conflict is
// recorded and the connection stops vouching, which refuses every caller.
//
// Refusing is the whole of what this does. What clears it is the connection probe's rebuild arm,
// which reads the conflict back through conflicted(); retiring the old connection belongs to the
// pool, and alone would not help, since Conn hands out a retired connection too (Done is how a
// holder hears about one).
func (c *Connection) setServerUID(uid string) {
	for {
		id := c.identity.Load()
		switch {
		case id == nil:
			if c.identity.CompareAndSwap(nil, &connIdentity{uid: uid}) {
				return
			}
			// Another run stamped first; re-read and judge against what it wrote.
			continue // coverage:ignore only a concurrent stamp between the Load and the swap reaches it
		case id.conflicted, id.uid == uid:
			return
		}
		if c.identity.CompareAndSwap(id, &connIdentity{uid: id.uid, conflicted: true}) {
			return
		}
	}
}

// dynamicBuilder builds a connection's dynamic client.
type dynamicBuilder func(cfg *rest.Config, c *http.Client) (dynamic.Interface, error)

// Option tunes the connections a Service builds, or the one NewConnection builds.
type Option func(*options)

type options struct {
	faults Faults
}

// WithFaults fails the requests faults names inside the connection, before the transport.
func WithFaults(faults Faults) Option {
	return func(o *options) { o.faults = faults }
}

// NewConnection materializes the clients for one set of credentials.
//
// NewForConfigAndClient, never NewForConfig: the latter builds a fresh client and a fresh
// pool, which is the whole thing one connection per context exists to avoid.
func NewConnection(cfg *rest.Config, opts ...Option) (*Connection, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	return newConnection(cfg, o, func(cfg *rest.Config, c *http.Client) (dynamic.Interface, error) {
		return dynamic.NewForConfigAndClient(cfg, c)
	})
}

// newConnection is NewConnection with the dynamic client's build passed in. It is the one
// seam in the build: every other failure here is reachable from a config a caller can write,
// and this one is not — the host is already parsed and the rate limiter is ours to set, so
// nothing a kubeconfig can say reaches it.
func newConnection(cfg *rest.Config, o options, newDynamic dynamicBuilder) (*Connection, error) {
	conn := &Connection{seq: connSeq.Add(1), done: make(chan struct{}), counts: &byteCounts{}}

	// A copy, because the tuning below is ours and the caller's config is not.
	own := rest.CopyConfig(cfg)
	own.QPS = defaultQPS
	own.Burst = defaultBurst
	own.UserAgent = version.UserAgent()
	own.Dial = newCountingDialer(conn.counts)

	// DefaultServerUrlFor rather than DefaultServerURL: it derives the scheme from whether the
	// config actually carries CA or client-cert data, so a scheme-less plain-HTTP endpoint (a
	// port-forward) stays HTTP instead of failing at a handshake.
	baseURL, apiPath, err := rest.DefaultServerUrlFor(own)
	if err != nil {
		return nil, fmt.Errorf("resolve server URL: %w", err)
	}
	// The fault wrapper first, since each later Wrap is the outer one: the idle bound then
	// bounds a faulted request as it does a real one.
	if wrap := o.faults.wrapper(baseURL.Path); wrap != nil {
		own.Wrap(wrap)
	}
	own.Wrap(newIdleTimeoutWrapper(listIdleTimeout))

	httpClient, err := rest.HTTPClientFor(own)
	if err != nil {
		return nil, fmt.Errorf("build http client: %w", err)
	}

	dyn, err := newDynamic(own, httpClient)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client: %w", err)
	}

	conn.Config = own
	conn.BaseURL = baseURL
	conn.APIPath = apiPath
	conn.HTTPClient = httpClient
	conn.Dynamic = dyn
	return conn, nil
}

// Done closes when this connection is retired: the credentials moved, or the last claim on the
// context went.
//
// For the holder nothing else reaches: a long-lived stream is blocked in a read, so a field it
// is not re-reading cannot tell it that what it derived here no longer holds. A caller that asks
// per operation re-calls Lease.Conn instead.
func (c *Connection) Done() <-chan struct{} { return c.done }

// Retire tells every holder this connection is void and gives its idle sockets back. Once,
// because publish and Release can both reach one on the way out and neither can cheaply prove
// the other did not.
//
// The pool's to call. Exported for a fake pool in another package's tests, which has to
// retire what it replaces for its holders to behave as they do under this one.
func (c *Connection) Retire() {
	c.once.Do(func() {
		// A connection nobody built carries neither, and reads as never retired — the
		// contract Done states.
		if c.done != nil {
			close(c.done)
		}
		if c.HTTPClient != nil {
			c.HTTPClient.CloseIdleConnections()
		}
	})
}

// errBadRequest is a request that could not be built, and errMalformed a response that would not
// parse — the latter usually meaning a proxy or captive portal answered for the API server.
var (
	errBadRequest = errors.New("malformed request")
	errMalformed  = errors.New("malformed response")
)

// httpErr is a response that was not 2xx. The code is the whole evidence most raw endpoints
// leave, so it travels as data rather than inside a formatted string — and the body with it,
// because /readyz answers a 500 with the component list that *is* the answer.
type httpErr struct {
	path   string
	code   int
	status string
	body   string
}

func (e *httpErr) Error() string { return fmt.Sprintf("%s: %s", e.path, e.status) }

// maxBody bounds what is read off any response: enough for an error page or the readyz component
// list, and short of anything a hostile endpoint could stream. A body past it is left unread, so
// an HTTP/1.1 fallback drops that connection rather than reusing it.
const maxBody = 64 << 10

// getJSON reads one raw API path and decodes the response into out.
func (c *Connection) getJSON(ctx context.Context, path string, out any) error {
	body, err := c.request(ctx, http.MethodGet, path, "application/json", nil)
	if err != nil {
		return err
	}
	return decode(path, body, out)
}

// postJSON creates one resource and decodes the answer into out.
func (c *Connection) postJSON(ctx context.Context, path string, body []byte, out any) error {
	answer, err := c.request(ctx, http.MethodPost, path, "application/json", body)
	if err != nil {
		return err
	}
	return decode(path, answer, out)
}

// getText reads one raw API path that answers in plain text.
func (c *Connection) getText(ctx context.Context, path string) (string, error) {
	return c.request(ctx, http.MethodGet, path, "text/plain", nil)
}

func decode(path, body string, out any) error {
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("%w from %s: %w", errMalformed, path, err)
	}
	return nil
}

// request performs one request against a raw API path and hands back its body.
func (c *Connection) request(ctx context.Context, method, path, accept string, payload []byte) (string, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL.JoinPath(path).String(), body)
	if err != nil {
		return "", fmt.Errorf("%w %s: %w", errBadRequest, path, err)
	}
	req.Header.Set("Accept", accept)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()

	// Read either way: a body is drained so the connection can be reused, and on the one
	// endpoint that answers a failure with detail it is what the probe came for.
	read, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Before the read error, because the status line already arrived and is the stronger
		// evidence: a 403 whose error page stops mid-stream is still a grant to fix, where a
		// malformed answer would point at a proxy. Whatever arrived rides along, since the
		// one endpoint that answers a failure with detail may have been cut off mid-list.
		return "", &httpErr{path: path, code: resp.StatusCode, status: resp.Status, body: string(read)}
	}
	if readErr != nil {
		return "", fmt.Errorf("%w from %s: %w", errMalformed, path, readErr)
	}
	return string(read), nil
}

// classify names why a request failed.
//
// Order is load-bearing: everything below arrives wrapped in the same *url.Error, so the
// specific tests come before the transport catch-all. A rejected certificate read as a server
// that never answered would send the user looking for an outage instead of at their CA.
//
// Cancellation is not here. A run ending because the caller went away records nothing at all,
// which is a Skip rather than a reason.
func classify(err error) Reason {
	var status *httpErr
	switch {
	case errors.As(err, &status):
		// A code with no meaning of its own is Malformed rather than a fault of the API
		// server's: the likeliest sender is a proxy between us and it.
		if reason, ok := statusReason(int32(status.code)); ok {
			return reason
		}
		return ReasonMalformed
	case isTLSError(err):
		return ReasonTLSInvalid
	case errors.Is(err, context.DeadlineExceeded):
		// Ours, not the caller's: the deadline was the probe's own, and the answer that did
		// not arrive in time is news about the cluster.
		return ReasonTimeout
	case errors.Is(err, errMalformed):
		return ReasonMalformed
	case errors.Is(err, errBadRequest):
		return ReasonInternal
	default:
		// Nothing answered: DNS, a refused connection, no route. The transport has many
		// spellings for it and they all mean the same thing to a reader.
		return ReasonUnreachable
	}
}

// ClassifyRequest names why a request through a Connection's clients failed: a Status the API
// server answered with, or the transport failing under it. Anything else — a watch that ended
// early, a frame that would not apply — is ReasonUnknown, since naming it would claim a cause
// nobody observed.
func ClassifyRequest(err error) Reason {
	var status apierrors.APIStatus
	var transport *url.Error
	switch {
	case errors.As(err, &status):
		// A Status is the API server's own, so a 404 means the resource is gone, and a code
		// with no reason of its own stays unknown rather than pointing at a proxy.
		code := status.Status().Code
		if code == http.StatusNotFound {
			return ReasonNotFound
		}
		reason, _ := statusReason(code)
		return reason
	case errors.As(err, &transport), errors.Is(err, context.DeadlineExceeded):
		return classify(err)
	default:
		return ReasonUnknown
	}
}

// statusReason names a response by its code; false, with ReasonUnknown, for a code with no
// reason of its own.
func statusReason(code int32) (Reason, bool) {
	switch code {
	case http.StatusUnauthorized:
		return ReasonUnauthorized, true
	case http.StatusForbidden:
		return ReasonForbidden, true
	case http.StatusTooManyRequests:
		return ReasonThrottled, true
	case http.StatusInternalServerError:
		return ReasonInternalError, true
	case http.StatusServiceUnavailable:
		return ReasonServiceUnavailable, true
	default:
		return ReasonUnknown, false
	}
}

// isTLSError reports whether the handshake is what failed. Four spellings, because the
// certificate can be rejected by our own verification or by the far end reporting it.
func isTLSError(err error) bool {
	// Three of the four are value types, which is what crypto/x509 returns them as: a pointer
	// target matches nothing the transport emits.
	var (
		unknownAuthority x509.UnknownAuthorityError
		invalidCert      x509.CertificateInvalidError
		wrongHost        x509.HostnameError
		verification     *tls.CertificateVerificationError
	)
	return errors.As(err, &unknownAuthority) ||
		errors.As(err, &invalidCert) ||
		errors.As(err, &wrongHost) ||
		errors.As(err, &verification)
}

// The env vars apimachinery's transport defaults read, and the values we want. Against
// client-go's 30s/15s defaults these turn a silently-dropped API-server connection into
// a ~15s detection instead of ~45s, which is how fast anything watching a cluster
// notices it is gone. → docs/adr/2026-09-30-the-cluster-service-is-one-package-over-private-leaves.md.
const (
	readIdleTimeoutEnv = "HTTP2_READ_IDLE_TIMEOUT_SECONDS"
	pingTimeoutEnv     = "HTTP2_PING_TIMEOUT_SECONDS"

	readIdleTimeoutSeconds = 10
	pingTimeoutSeconds     = 5
)

// configureHTTP2Keepalive tightens client-go's HTTP/2 health check, only where the
// operator has not set a value. The vars are read lazily per transport build, so New
// calling this is enough for every connection the pool goes on to build.
func configureHTTP2Keepalive() {
	setKeepaliveDefaults(os.LookupEnv, os.Setenv)
}

// setKeepaliveDefaults sets each keepalive var lookup does not find. The environment is
// passed in so a test can use its own rather than the process's.
func setKeepaliveDefaults(lookup func(string) (string, bool), set func(string, string) error) {
	for key, val := range map[string]int{
		readIdleTimeoutEnv: readIdleTimeoutSeconds,
		pingTimeoutEnv:     pingTimeoutSeconds,
	} {
		if _, ok := lookup(key); !ok {
			_ = set(key, strconv.Itoa(val))
		}
	}
}
