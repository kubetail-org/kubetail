package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Server is the GraphQL surface: the gqlgen handler, with every SSE subscription
// ended when streamsCtx ends.
//
// **Never cancel via http.Server.BaseContext**: that would tear down the shared h2c
// connection carrying gRPC mid-stream, so SSE cancellation is per-request instead.
// See docs/adr/2026-10-26-single-socket-h2c-from-the-standard-library.md.
type Server struct {
	h          http.Handler
	streamsCtx context.Context
}

// NewServer builds the GraphQL server around a fully-wired Resolver (see Resolver on the
// non-nil requirement). Ending streamsCtx ends every SSE subscription.
func NewServer(streamsCtx context.Context, r *Resolver) *Server {
	srv := handler.New(NewExecutableSchema(Config{Resolvers: r}))
	// Subscriptions ride SSE: one `POST /graphql` with `Accept: text/event-stream`
	// per subscription, the keep-alive ping holding the idle UDS connection warm.
	//
	// SSE MUST be registered before POST: gqlgen takes the first transport that
	// Supports() a request, and POST greedily matches any application/json POST
	// regardless of Accept.
	srv.AddTransport(transport.SSE{KeepAlivePingInterval: 10 * time.Second})
	srv.AddTransport(transport.POST{})
	srv.AddTransport(transport.GET{})

	// Lets a subscription report WHY it ended; see SubscriptionFailureExtension.
	srv.Use(SubscriptionFailureExtension{})

	// Request text, names, aliases and error messages can all contain credentials.
	// Log only server-selected metadata; return the detailed error over authenticated IPC.
	srv.SetErrorPresenter(func(ctx context.Context, e error) *gqlerror.Error {
		err := graphql.DefaultErrorPresenter(ctx, e)
		op := "unknown"
		if oc := graphql.GetOperationContext(ctx); oc != nil && oc.Operation != nil {
			op = string(oc.Operation.Operation)
		}
		// The code is server-chosen vocabulary, so it is safe to log and it is the
		// only thing that says which refusal this was: every mapped error is the
		// same Go type.
		attrs := []any{"operation_type", op, "error", fmt.Sprintf("%T", e)}
		// gqlgen wraps every plain resolver error in a *gqlerror.Error, so the cause's
		// type is what says where it came from. context.Canceled is an errorString
		// like any errors.New, so a cancel is named apart.
		if cause := errors.Unwrap(e); cause != nil {
			attrs = append(attrs, "cause", fmt.Sprintf("%T", cause))
		}
		attrs = append(attrs, "canceled", errors.Is(e, context.Canceled), "ctx_err", ctx.Err() != nil)
		if code, ok := err.Extensions["code"].(string); ok {
			attrs = append(attrs, "code", code)
		}
		slog.ErrorContext(ctx, "graphql error", attrs...)
		return err
	})

	return &Server{h: srv, streamsCtx: streamsCtx}
}

// ServeHTTP runs the handler. SSE subscriptions — the only long-lived requests — get a
// context streamsCtx also ends, so gqlgen flushes `event: complete` instead of
// the stream being cut mid-frame.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer context.AfterFunc(s.streamsCtx, cancel)()
		s.h.ServeHTTP(w, r.WithContext(ctx))
		return
	}

	s.h.ServeHTTP(w, r)
}
