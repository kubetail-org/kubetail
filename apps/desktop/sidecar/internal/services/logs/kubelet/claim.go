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

package kubelet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/safe"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/cluster"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/logline"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/services/logs/refusal"
)

// ErrRevoked is a cluster Kubetail does not connect to: disabled, marked for
// deletion, or gone, before the read or under it.
var ErrRevoked = refusal.New(refusal.CodeRevoked, "Kubetail no longer connects to this cluster")

// maxStatusBody is how much of a refusal's body is read for its message.
const maxStatusBody = 4 << 10

// StatusError is a request the API server refused: its HTTP code and the message
// its Status carried, cleaned as a line is.
type StatusError struct {
	Code    int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("the API server answered %d", e.Code)
	}
	return e.Message
}

// ErrRetired is a read the connection's retirement cut short: the pool rebuilt it,
// so a read opened again goes over its successor.
var ErrRetired = errors.New("logs: the connection was replaced under the read")

// claim is one cluster's connection, held for a fetch or a follow: the lease, the
// server UID the mirror's pods came from, and for a follow the record's watch, which
// revokes the claim once Kubetail no longer connects to the cluster.
type Claim struct {
	lease     cluster.Lease
	serverUID string
	// revoked is nil for a claim nothing watches, which is never revoked.
	Revoked <-chan struct{}
	Stop    context.CancelFunc
}

// Acquire claims cluster id's connection for the server serverUID, checking
// the record once. It does not dial. A fetch holds it for one page, which the
// connection's own retirement already bounds, so nothing watches the record.
func Acquire(ctx context.Context, svc cluster.Service, id cluster.ClusterID, serverUID string) (*Claim, error) {
	lease, err := svc.AcquireConnection(ctx, id)
	switch {
	case errors.Is(err, cluster.ErrNotConnectable), errors.Is(err, cluster.ErrNotFound):
		return nil, ErrRevoked
	case err != nil:
		return nil, err
	}
	return &Claim{lease: lease, serverUID: serverUID, Stop: func() {}}, nil
}

// Open is Acquire watching the record until close, opening the watch
// again retry after one that ends, for a follow that holds its claim for as long as
// the user looks.
func Open(ctx context.Context, svc cluster.Service, id cluster.ClusterID, serverUID string, retry time.Duration) (*Claim, error) {
	c, err := Acquire(ctx, svc, id, serverUID)
	if err != nil {
		return nil, err
	}
	watchCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	stream, err := svc.Clusters().WatchCollectionByID(watchCtx, id)
	if err != nil {
		stop()
		c.lease.Release()
		return nil, err
	}
	revoked := make(chan struct{})
	go func() {
		if watchRevoked(watchCtx, svc, id, stream, retry) {
			close(revoked)
		}
	}()
	c.Revoked, c.Stop = revoked, stop
	return c, nil
}

// watchRevoked is whether the record's watch showed the cluster Kubetail no longer
// connects to, watching until it does or ctx ends. A watch that ends is opened
// again after retry, its snapshot saying whether the record still stands; one
// that cannot be opened again, or ends before its snapshot closes, revokes, since
// a claim nothing watches could outlive the cluster.
func watchRevoked(ctx context.Context, svc cluster.Service, id cluster.ClusterID, stream *cluster.Stream[cluster.ClusterCollectionWatchFrame], retry time.Duration) bool {
	for {
		seen, synced := false, false
		for f := range stream.Frames {
			switch {
			case f.Type == cluster.CollectionWatchFrameDeleted || f.Cluster != nil && !connectable(f.Cluster):
				return true
			case f.Cluster != nil:
				seen = true
			case f.Type == cluster.CollectionWatchFrameBookmark:
				if !seen {
					return true
				}
				synced = true
			}
		}
		if ctx.Err() != nil {
			return false
		}
		if !synced {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(retry):
		}
		var err error
		if stream, err = svc.Clusters().WatchCollectionByID(ctx, id); err != nil {
			return ctx.Err() == nil
		}
	}
}

// connectable is whether AcquireConnection would claim c: enabled, not marked for
// deletion, and from the kubeconfig.
func connectable(c *cluster.Cluster) bool {
	return c.Spec.Enabled && c.DeletionRequestedAt == nil && c.KubeContext() != ""
}

// get is one GET of path with query on the claimed server, through the
// connection's own client, so the user's credentials authenticate it. The body is
// read as a stream: it ends when ctx does, when the claim is revoked (ErrRevoked),
// or when the connection is retired (ErrRetired). A status other than 200 is a
// *StatusError.
func (c *Claim) Get(ctx context.Context, path string, query url.Values) (io.ReadCloser, error) {
	if c.IsRevoked() {
		return nil, ErrRevoked
	}
	conn, err := c.lease.ConnFor(ctx, c.serverUID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	go func() {
		select {
		case <-c.Revoked:
			cancel(ErrRevoked)
		case <-conn.Done():
			cancel(ErrRetired)
		case <-ctx.Done():
		}
	}()
	u := conn.BaseURL.JoinPath(path)
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil { // coverage:ignore the URL is the connection's parsed base URL with a path joined, which always parses
		cancel(nil)
		return nil, err
	}
	resp, err := conn.HTTPClient.Do(req)
	if err != nil {
		err = endedBy(ctx, err)
		cancel(nil)
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer cancel(nil)
		defer resp.Body.Close()
		return nil, statusOf(resp)
	}
	return body{resp.Body, ctx, cancel}, nil
}

// endedBy is err, or the claim's reason when the claim ended the request: the
// transport reports either as a plain cancel, which would read as the caller's.
func endedBy(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); errors.Is(cause, ErrRevoked) || errors.Is(cause, ErrRetired) {
		return cause
	}
	return err
}

// isRevoked is whether the claim has been revoked.
func (c *Claim) IsRevoked() bool {
	select {
	case <-c.Revoked:
		return true
	default:
		return false
	}
}

// statusOf is a refusal's code and the message of the Status it carries,
// redacted as every diagnostic is.
func statusOf(resp *http.Response) *StatusError {
	var status struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxStatusBody)).Decode(&status) // a body that is no Status has no message
	return &StatusError{Code: resp.StatusCode, Message: logline.StripControl(safe.String(status.Message))}
}

// body is a response's body that ends its request's context once closed, and
// names the claim's reason when the claim cut it.
type body struct {
	io.ReadCloser
	ctx    context.Context
	Cancel context.CancelCauseFunc
}

func (b body) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		err = endedBy(b.ctx, err)
	}
	return n, err
}

func (b body) Close() error {
	b.Cancel(nil)
	return b.ReadCloser.Close()
}

// close ends the record's watch and releases the lease.
func (c *Claim) Close() {
	c.Stop()
	c.lease.Release()
}
