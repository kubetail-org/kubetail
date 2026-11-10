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

package graph

import (
	"context"
	"errors"
	"io"
	"sync"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	zlog "github.com/rs/zerolog/log"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
)

// fanoutSubscribeFunc runs fn against every agent, current and future, until
// the returned unsubscribe function is called.
type fanoutSubscribeFunc func(ctx context.Context, fn grpcdispatcher.DispatchHandler) (unsubscribe func(), err error)

// watchLogMetadata forwards every agent's LogMetadataService.Watch events to
// the returned channel. The channel closes when ctx is done or an agent fails.
// The first failure goes to reportError just before the channel closes, which
// is when gqlgen's WebSocket transport reads it.
//
// An agent stream that ends cleanly or because the agent is unavailable ends
// quietly: the agent may be restarting, and subscribe picks up its replacement.
func watchLogMetadata(
	ctx context.Context,
	req *clusteragentpb.LogMetadataWatchRequest,
	subscribe fanoutSubscribeFunc,
	reportError func(context.Context, *gqlerror.Error),
) (<-chan *clusteragentpb.LogMetadataWatchEvent, error) {
	ctx, cancel := context.WithCancel(ctx)

	outCh := make(chan *clusteragentpb.LogMetadataWatchEvent)

	// A sender holds closeMu's read lock, so outCh cannot close mid-send.
	var (
		closeMu sync.RWMutex
		closed  bool
	)

	send := func(ev *clusteragentpb.LogMetadataWatchEvent) bool {
		closeMu.RLock()
		defer closeMu.RUnlock()
		if closed {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case outCh <- ev:
			return true
		}
	}

	var (
		errMu     sync.Mutex
		errClosed bool
		watchErr  *gqlerror.Error
	)

	// fail records err before it cancels ctx, so the closer, which waits on
	// ctx, always sees it.
	fail := func(err *gqlerror.Error) {
		errMu.Lock()
		if !errClosed && watchErr == nil {
			watchErr = err
		}
		errMu.Unlock()
		cancel()
	}

	watchAgent := func(ctx context.Context, conn *grpc.ClientConn) error {
		stream, err := clusteragentpb.NewLogMetadataServiceClient(conn).Watch(ctx, req)
		if err != nil {
			return err
		}
		for {
			ev, err := stream.Recv()
			if err != nil {
				return err
			}
			if !send(ev) {
				return nil
			}
		}
	}

	unsubscribe, err := subscribe(ctx, func(ctx context.Context, conn *grpc.ClientConn) {
		err := watchAgent(ctx, conn)
		switch {
		case err == nil, errors.Is(err, io.EOF), ctx.Err() != nil:
			return
		case status.Code(err) == codes.Unavailable, status.Code(err) == codes.Canceled:
			return
		}
		zlog.Error().Caller().Err(err).Msg("Unexpected error from cluster agent")
		fail(NewGrpcError(conn, err))
	})
	if err != nil {
		cancel()
		return nil, err
	}

	go func() {
		<-ctx.Done()
		unsubscribe()

		errMu.Lock()
		errClosed = true
		err := watchErr
		errMu.Unlock()

		closeMu.Lock()
		defer closeMu.Unlock()
		closed = true
		if err != nil {
			reportError(ctx, err)
		}
		close(outCh)
	}()

	return outCh, nil
}
