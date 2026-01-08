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

package app

import (
	"context"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/requestid"
	"github.com/gin-contrib/secure"
	"github.com/gin-gonic/gin"

	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"

	capicfg "github.com/kubetail-org/kubetail/modules/cluster-api/pkg/config"
	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/ginhelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/middleware"

	clusterapi "github.com/kubetail-org/kubetail/modules/cluster-api"
	"github.com/kubetail-org/kubetail/modules/cluster-api/graph"
)

type App struct {
	*gin.Engine
	cm             k8shelpers.ConnectionManager
	grpcDispatcher *grpcdispatcher.Dispatcher
	graphqlServer  *graph.Server

	// for testing
	dynamicRoutes *gin.RouterGroup
}

// Shutdown
func (a *App) Shutdown(ctx context.Context) error {
	// Stop grpc dispatcher
	if a.grpcDispatcher != nil {
		// TODO: log dispatcher shutdown errors
		a.grpcDispatcher.Shutdown()
	}

	// Shutdown GraphQL server
	a.graphqlServer.Shutdown()

	// Shutdown connection manager
	return a.cm.Shutdown(ctx)
}

// Create new gin app
func NewApp(cfg *capicfg.Config) (*App, error) {
	// Init app
	app := &App{Engine: gin.New()}

	// If not in test-mode
	if gin.Mode() != gin.TestMode {
		app.Use(gin.Recovery())

		// Init connection manager
		cm, err := k8shelpers.NewConnectionManager(sharedcfg.EnvironmentCluster)
		if err != nil {
			return nil, err
		}
		app.cm = cm

		// init grpc dispatcher
		app.grpcDispatcher = mustNewGrpcDispatcher(cfg)
	}

	// Add request-id middleware
	app.Use(requestid.New())

	// Add logging middleware
	if cfg.Logging.AccessLog.Enabled {
		app.Use(middleware.LoggingMiddleware(cfg.Logging.AccessLog.HideHealthChecks))
	}

	// Gzip middleware
	app.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithCustomShouldCompressFn(func(c *gin.Context) bool {
			ae := c.GetHeader("Accept-Encoding")
			if !strings.Contains(ae, "gzip") {
				return false
			}
			return !ginhelpers.IsWebSocketRequest(c)
		}),
	))

	// Routes
	root := app.Group(cfg.BasePath)

	// Dynamic routes
	dynamicRoutes := root.Group("/")
	{
		// https://security.stackexchange.com/questions/147554/security-headers-for-a-web-api
		// https://observatory.mozilla.org/faq/
		dynamicRoutes.Use(secure.New(secure.Config{
			STSSeconds:            63072000,
			FrameDeny:             true,
			ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
			ContentTypeNosniff:    true,
		}))

		// authentication middleware
		dynamicRoutes.Use(authenticationMiddleware)

		// GraphQL endpoint
		app.graphqlServer = graph.NewServer(cfg, app.cm, app.grpcDispatcher, cfg.AllowedNamespaces)
		dynamicRoutes.Any("/graphql", gin.WrapH(app.graphqlServer))
	}
	app.dynamicRoutes = dynamicRoutes // for unit tests

	// Root endpoint
	root.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Kubetail Cluster API")
	})

	// Health endpoint
	root.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// Init staticFS
	sub, err := fs.Sub(clusterapi.StaticEmbedFS, "static")
	if err != nil {
		return nil, err
	}
	staticFS := http.FS(sub)

	// GraphQL Playground
	root.StaticFileFS("/graphiql", "/graphiql.html", staticFS)

	root.StaticFileFS("/favicon.ico", "/favicon.ico", staticFS)
	root.StaticFileFS("/favicon.svg", "/favicon.svg", staticFS)

	return app, nil
}
