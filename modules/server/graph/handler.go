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

package graph

import (
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"
)

type HandlerOptions struct {
	WSInitFunc transport.WebsocketInitFunc
}

func NewDefaultHandlerOptions() *HandlerOptions {
	return &HandlerOptions{}
}

func NewHandler(r *Resolver, options *HandlerOptions) *handler.Server {
	if options == nil {
		options = NewDefaultHandlerOptions()
	}

	c := Config{Resolvers: r}
	c.Directives.Validate = ValidateDirective
	c.Directives.NullIfValidationFailed = NullIfValidationFailedDirective

	// init handler
	h := handler.New(NewExecutableSchema(c))

	// add options from from NewDefaultServer
	h.AddTransport(transport.GET{})
	h.AddTransport(transport.POST{})

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// configure WebSocket (without CORS)
	h.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// We have to return true here because `kubectl proxy` modifies the Host header
				// so requests will fail same-origin tests and unfortunately not all browsers
				// have implemented `sec-fetch-site` header. Instead, we will use CSRF token
				// validation to ensure requests are coming from the same site.
				return true
			},
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		InitFunc:              options.WSInitFunc,
		KeepAlivePingInterval: 10 * time.Second,
	})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return h
}
