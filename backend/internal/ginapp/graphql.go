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
	"errors"
	"net/http"
	"net/http/httptest"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gin-gonic/gin"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/graph"
)

type key int

const graphQLCookiesCtxKey key = iota

type GraphQLHandlers struct {
	*GinApp
}

// GET|POST "/graphql": GraphQL query endpoint
func (app *GraphQLHandlers) EndpointHandler(cfg *rest.Config, namespace string, csrfProtect func(http.Handler) http.Handler) gin.HandlerFunc {
	// init resolver
	r, err := graph.NewResolver(cfg, namespace)
	if err != nil {
		panic(err)
	}

	csrfTestServer := http.NewServeMux()
	csrfTestServer.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	// init handler options
	opts := graph.NewDefaultHandlerOptions()

	// use CSRF token validation to ensure that connection requests come from same site
	opts.WSInitFunc = func(ctx context.Context, initPayload transport.InitPayload) (context.Context, *transport.InitPayload, error) {
		// check if csrf protection is disabled
		if csrfProtect == nil {
			return ctx, &initPayload, nil
		}

		csrfToken := initPayload.Authorization()

		cookies, ok := ctx.Value(graphQLCookiesCtxKey).([]*http.Cookie)
		if !ok {
			return ctx, nil, errors.New("AUTHORIZATION_REQUIRED")
		}

		// make mock request
		r, _ := http.NewRequest("POST", "/", nil)
		for _, cookie := range cookies {
			r.AddCookie(cookie)
		}
		r.Header.Set("X-CSRF-Token", csrfToken)

		// run request through csrf protect function
		rr := httptest.NewRecorder()
		p := csrfProtect(csrfTestServer)
		p.ServeHTTP(rr, r)

		if rr.Code != 200 {
			return ctx, nil, errors.New("AUTHORIZATION_REQUIRED")
		}

		return ctx, &initPayload, nil
	}

	// init handler
	h := graph.NewHandler(r, opts)

	// return gin handler func
	return func(c *gin.Context) {
		// save cookies for use in WSInitFunc
		ctx := context.WithValue(c.Request.Context(), graphQLCookiesCtxKey, c.Request.Cookies())
		c.Request = c.Request.WithContext(ctx)

		// execute
		h.ServeHTTP(c.Writer, c.Request)
	}
}
