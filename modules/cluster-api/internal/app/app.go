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

	sharedcfg "github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/ginhelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/middleware"

	clusterapi "github.com/kubetail-org/kubetail/modules/cluster-api"
	"github.com/kubetail-org/kubetail/modules/cluster-api/graph"
	"github.com/kubetail-org/kubetail/modules/cluster-api/pkg/config"
)

type App struct {
	*gin.Engine
	cm             k8shelpers.ConnectionManager
	grpcDispatcher *grpcdispatcher.Dispatcher
	graphqlServer  *graph.Server

	// for testing
	dynamicRoutes *gin.RouterGroup
}

// NotifyShutdown signals active connections to begin closing.
func (a *App) NotifyShutdown() {
	a.graphqlServer.NotifyShutdown()
}

// DrainWithContext waits for all active connections to finish, respecting ctx.
func (a *App) DrainWithContext(ctx context.Context) error {
	return a.graphqlServer.DrainWithContext(ctx)
}

// Close releases app-level resources.
func (a *App) Close() {
	if a.grpcDispatcher != nil {
		a.grpcDispatcher.Shutdown()
	}
	if a.cm != nil {
		a.cm.Close()
	}
}

// Create new gin app
func NewApp(cfg *config.Config) (*App, error) {
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

	// Dynamic routes mount under the Kubernetes API extension group/version
	// so the cluster-api can be aggregated into kube-apiserver.
	dynamicRoutes := root.Group("/apis/api.kubetail.com/v1")
	{
		// https://security.stackexchange.com/questions/147554/security-headers-for-a-web-api
		// https://observatory.mozilla.org/faq/
		dynamicRoutes.Use(secure.New(secure.Config{
			STSSeconds:            63072000,
			FrameDeny:             true,
			ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
			ContentTypeNosniff:    true,
		}))

		// Aggregation-layer authentication. Skipped in test mode because the
		// loader needs a live Kubernetes cluster (kube-system's
		// extension-apiserver-authentication ConfigMap).
		if gin.Mode() != gin.TestMode {
			clientset, err := app.cm.GetOrCreateClientset("")
			if err != nil {
				return nil, err
			}
			authCfg, err := loadAggregationAuthConfig(context.TODO(), clientset)
			if err != nil {
				return nil, err
			}
			dynamicRoutes.Use(newAggregationAuthMiddleware(authCfg))
		}

		// Mirrored under the aggregated path so the dashboard cluster-api proxy
		// can reach it; kube-apiserver only forwards /apis/<group>/<version>/...
		dynamicRoutes.GET("/healthz", healthzHandler)

		// GraphQL endpoint
		app.graphqlServer = graph.NewServer(app.cm, app.grpcDispatcher, cfg.AllowedNamespaces)
		dynamicRoutes.Any("/graphql", gin.WrapH(app.graphqlServer))

		// Log download endpoint
		dl := newDownloadHandlers(app.cm, app.grpcDispatcher, cfg.AllowedNamespaces)
		dynamicRoutes.POST("/download", dl.DownloadPOST)
	}
	app.dynamicRoutes = dynamicRoutes // for unit tests

	// Root endpoint
	root.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "Kubetail Cluster API")
	})

	// Health endpoint
	root.GET("/healthz", healthzHandler)

	// Kubernetes API extension discovery endpoints
	root.GET("/apis", extGroupDiscoveryHandler)
	root.GET("/apis/api.kubetail.com/v1", extVersionDiscoveryHandler)

	// Init staticFS
	sub, err := fs.Sub(clusterapi.StaticEmbedFS, "static")
	if err != nil {
		return nil, err
	}
	staticFS := http.FS(sub)

	// OpenAPI endpoints (kube-apiserver fetches these once the APIService is aggregated)
	root.StaticFileFS("/openapi/v2", "/swagger.json", staticFS)
	root.GET("/openapi/v3", openapiV3IndexHandler)
	root.GET("/openapi/v3/apis/api.kubetail.com/v1", openapiV3GroupVersionHandler)

	// GraphQL Playground
	root.StaticFileFS("/graphiql", "/graphiql.html", staticFS)

	root.StaticFileFS("/favicon.ico", "/favicon.ico", staticFS)
	root.StaticFileFS("/favicon.svg", "/favicon.svg", staticFS)

	return app, nil
}
