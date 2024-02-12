// Copyright 2024 Andres Morey
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

package ginapp

import (
	"context"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/graph"
)

type GraphQLHandlers struct {
	*GinApp
}

// GET|POST "/graphql": GraphQL query endpoint
func (app *GraphQLHandlers) EndpointHandler(cfg *rest.Config, namespace string) gin.HandlerFunc {
	// init resolver
	r, err := graph.NewResolver(cfg, namespace)
	if err != nil {
		panic(err)
	}

	// init handler options
	opts := graph.NewDefaultHandlerOptions()

	opts.WSInitFunc = func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
		token := initPayload.Authorization()

		// add token to context
		if token != "" {
			ctx = context.WithValue(ctx, graph.K8STokenCtxKey, token)
		}

		// return
		return ctx, &initPayload, nil
	}

	// init handler
	h := graph.NewHandler(r, opts)

	// return gin handler func
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
