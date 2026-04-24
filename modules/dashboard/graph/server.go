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
	"net/http"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/lru"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/directives"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/versioncheck"

	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/preferences"
)

// Represents Server
type Server struct {
	r          *Resolver
	h          http.Handler
	hm         clusterapi.HealthMonitor
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// Create new Server instance
func NewServer(cfg *config.Config, cm k8shelpers.ConnectionManager) *Server {
	// Init health monitor
	hm := clusterapi.NewHealthMonitor(cfg, cm)

	// Init resolver
	r := &Resolver{
		cfg:               cfg,
		cm:                cm,
		hm:                hm,
		environment:       cfg.Environment,
		allowedNamespaces: cfg.AllowedNamespaces,
		versionChecker:    versioncheck.NewChecker(),
		helmReleaseGetter: &defaultHelmReleaseGetter{kubeconfigPath: cfg.KubeconfigPath},
	}

	if path := cfg.PreferencesPath(); path != "" {
		r.preferencesStore = preferences.NewStore(path)
	}

	// Init config
	gqlCfg := Config{Resolvers: r}
	gqlCfg.Directives.Validate = directives.ValidateDirective
	gqlCfg.Directives.NullIfValidationFailed = directives.NullIfValidationFailedDirective

	// Init schema
	schema := NewExecutableSchema(gqlCfg)

	// Init handler
	h := handler.New(schema)

	// Add transports from NewDefaultServer()
	h.AddTransport(transport.GET{})
	h.AddTransport(transport.POST{})

	h.SetQueryCache(lru.New[*ast.QueryDocument](1000))

	// Configure WebSocket. CSRF / CSWSH protection is enforced by the app-level
	// Sec-Fetch-Site middleware before the request reaches this handler, so we
	// allow all origins here.
	h.AddTransport(&transport.Websocket{
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
			ReadBufferSize:    1024,
			WriteBufferSize:   1024,
			EnableCompression: true,
		},
		KeepAlivePingInterval: 10 * time.Second,
	})

	h.Use(extension.Introspection{})
	h.Use(extension.AutomaticPersistedQuery{
		Cache: lru.New[string](100),
	})

	return &Server{r: r, h: h, hm: hm, shutdownCh: make(chan struct{})}
}

// NotifyShutdown signals active WebSocket connections to begin closing.
func (s *Server) NotifyShutdown() {
	close(s.shutdownCh)
}

// DrainWithContext waits for all active WebSocket connections to finish, respecting ctx.
func (s *Server) DrainWithContext(ctx context.Context) error {
	doneCh := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close releases any server-level resources.
func (s *Server) Close() error {
	if s.hm != nil {
		s.hm.Shutdown()
	}
	return nil
}

// ServeHTTP delegates to the underlying handler, tracking all active
// requests so DrainWithContext can wait for them to finish. For WebSocket
// upgrades the request context is also cancelled on shutdown, which
// triggers gqlgen's closeOnCancel to cleanly close the connection.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.wg.Add(1)
	defer s.wg.Done()

	if r.Header.Get("Upgrade") != "" {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-ctx.Done():
			case <-s.shutdownCh:
				cancel()
			}
		}()
		r = r.WithContext(ctx)
	}

	s.h.ServeHTTP(w, r)
}
