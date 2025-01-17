package graph

// This file will be automatically regenerated based on the schema, any resolver implementations
// will be copied through when generating and any unknown code will be moved to the end.
// Code generated by github.com/99designs/gqlgen version v0.17.63

import (
	"context"
	"io"
	"sync"

	"github.com/kubetail-org/kubetail/modules/shared/clusteragentpb"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	zlog "github.com/rs/zerolog/log"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LogMetadataList is the resolver for the logMetadataList field.
func (r *queryResolver) LogMetadataList(ctx context.Context, namespace *string) (*clusteragentpb.LogMetadataList, error) {
	// Deref namespace
	nsList, err := k8shelpers.DerefNamespaceToList(r.allowedNamespaces, namespace, metav1.NamespaceDefault)
	if err != nil {
		return nil, err
	}

	outList := &clusteragentpb.LogMetadataList{}
	req := &clusteragentpb.LogMetadataListRequest{Namespaces: nsList}

	// Execute
	var mu sync.Mutex
	errs := gqlerror.List{}

	r.grpcDispatcher.Fanout(ctx, func(ctx context.Context, conn *grpc.ClientConn) {
		// init client
		c := clusteragentpb.NewLogMetadataServiceClient(conn)

		// execute
		resp, err := c.List(ctx, req)

		// aquire lock
		mu.Lock()
		defer mu.Unlock()

		// update vars
		if err != nil {
			errs = append(errs, NewGrpcError(conn, err))
		} else {
			// update items
			outList.Items = append(outList.Items, resp.GetItems()...)
		}
	})

	// throw error if response is missing
	if len(errs) != 0 {
		return nil, errs
	}

	return outList, nil
}

// LogMetadataWatch is the resolver for the logMetadataWatch field.
func (r *subscriptionResolver) LogMetadataWatch(ctx context.Context, namespace *string) (<-chan *clusteragentpb.LogMetadataWatchEvent, error) {
	// Deref namespaces
	nsList, err := k8shelpers.DerefNamespaceToList(r.allowedNamespaces, namespace, metav1.NamespaceDefault)
	if err != nil {
		return nil, err
	}

	outCh := make(chan *clusteragentpb.LogMetadataWatchEvent)

	sub, err := r.grpcDispatcher.FanoutSubscribe(ctx, func(ctx context.Context, conn *grpc.ClientConn) {
		// init client
		c := clusteragentpb.NewLogMetadataServiceClient(conn)

		// init request
		req := &clusteragentpb.LogMetadataWatchRequest{Namespaces: nsList}

		// execute
		stream, err := c.Watch(ctx, req)
		if err != nil {
			return
		}

		for {
			ev, err := stream.Recv()

			// handle errors
			if err != nil {
				// ignore normal errors
				if err == io.EOF {
					break
				} else if err == context.Canceled {
					break
				}

				// check for grpc status error
				if s, ok := status.FromError(err); ok {
					switch s.Code() {
					case codes.Unavailable:
						// server down (probably restarting)
					case codes.Canceled:
						// connection closed client-side
					default:
						zlog.Error().Caller().Err(err).Msgf("Unexpected gRPC error: %v\n", s.Message())
					}
					break
				}

				zlog.Error().Caller().Err(err).Msg("Unexpected error")

				break
			}

			// forward event
			outCh <- ev
		}
	})
	if err != nil {
		return nil, err
	}

	// unsubscribe when client disconnects
	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()

	return outCh, nil
}

// Query returns QueryResolver implementation.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

// Subscription returns SubscriptionResolver implementation.
func (r *Resolver) Subscription() SubscriptionResolver { return &subscriptionResolver{r} }

type queryResolver struct{ *Resolver }
type subscriptionResolver struct{ *Resolver }