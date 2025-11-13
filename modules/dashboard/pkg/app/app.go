// Copyright 2024-2025 Andres Morey
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
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/requestid"
	"github.com/gin-contrib/secure"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/shared/config"
	"github.com/kubetail-org/kubetail/modules/shared/ginhelpers"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
	"github.com/kubetail-org/kubetail/modules/shared/middleware"

	"github.com/kubetail-org/kubetail/modules/dashboard"
	"github.com/kubetail-org/kubetail/modules/dashboard/graph"
	clusterapi "github.com/kubetail-org/kubetail/modules/dashboard/internal/cluster-api"
)

type App struct {
	*gin.Engine
	config          *config.Config
	cm              k8shelpers.ConnectionManager
	graphqlServer   *graph.Server
	clusterAPIProxy clusterapi.Proxy
	queryHelpers    queryHelpers

	// for testing
	dynamicRoutes *gin.RouterGroup
}

// Shutdown
func (a *App) Shutdown(ctx context.Context) error {
	// Shutdown GraphQL server
	a.graphqlServer.Shutdown()

	// Shudown Cluster API proxy
	a.clusterAPIProxy.Shutdown()

	// Shutdown connection manager
	return a.cm.Shutdown(ctx)
}

// Create new gin app
func NewApp(cfg *config.Config) (*App, error) {
	// Init app
	app := &App{Engine: gin.New(), config: cfg}

	// If not in test-mode
	if gin.Mode() != gin.TestMode {
		app.Use(gin.Recovery())

		// Init connection manager
		cm, err := k8shelpers.NewConnectionManager(cfg.Dashboard.Environment, k8shelpers.WithKubeconfigPath(cfg.KubeconfigPath))
		if err != nil {
			return nil, err
		}
		app.cm = cm

		// Init Cluster API proxy
		clusterAPIProxy, err := newClusterAPIProxy(cfg, app.cm, path.Join(cfg.Dashboard.BasePath, "/cluster-api-proxy"))
		if err != nil {
			return nil, err
		}
		app.clusterAPIProxy = clusterAPIProxy

		// Init queryHelpers
		app.queryHelpers = newRealQueryHelpers(app.cm)
	}

	// Register templates
	tmpl := template.Must(template.New("").
		Funcs(template.FuncMap{
			"pathJoin":  path.Join,
			"hasSuffix": strings.HasSuffix,
		}).
		ParseFS(dashboard.TemplatesEmbedFS, "templates/*"),
	)
	app.SetHTMLTemplate(tmpl)

	// Add request-id middleware
	app.Use(requestid.New())

	// Add logging middleware
	if cfg.Dashboard.Logging.AccessLog.Enabled {
		app.Use(middleware.LoggingMiddleware(cfg.Dashboard.Logging.AccessLog.HideHealthChecks))
	}

	// Add gzip middleware
	clusterAPIProxyPath := path.Join(cfg.Dashboard.BasePath, "/cluster-api-proxy/")
	app.Use(gzip.Gzip(gzip.DefaultCompression,
		gzip.WithCustomShouldCompressFn(func(c *gin.Context) bool {
			ae := c.GetHeader("Accept-Encoding")
			if !strings.Contains(ae, "gzip") {
				return false
			}

			requestPath := c.Request.URL.Path
			if strings.HasPrefix(requestPath, clusterAPIProxyPath) {
				return false
			}
			if strings.HasSuffix(requestPath, ".woff2") {
				return false
			}

			return !ginhelpers.IsWebSocketRequest(c)
		}),
	))

	// Root route
	root := app.Group(cfg.Dashboard.BasePath)

	// Dynamic routes
	dynamicRoutes := root.Group("/")
	{
		// Add session middleware
		sessionStore := cookie.NewStore([]byte(cfg.Dashboard.Session.Secret))
		sessionStore.Options(sessions.Options{
			Path:     cfg.Dashboard.Session.Cookie.Path,
			Domain:   cfg.Dashboard.Session.Cookie.Domain,
			MaxAge:   cfg.Dashboard.Session.Cookie.MaxAge,
			Secure:   cfg.Dashboard.Session.Cookie.Secure,
			HttpOnly: cfg.Dashboard.Session.Cookie.HttpOnly,
			SameSite: cfg.Dashboard.Session.Cookie.SameSite,
		})
		dynamicRoutes.Use(sessions.Sessions(cfg.Dashboard.Session.Cookie.Name, sessionStore))

		// https://security.stackexchange.com/questions/147554/security-headers-for-a-web-api
		// https://observatory.mozilla.org/faq/
		dynamicRoutes.Use(secure.New(secure.Config{
			STSSeconds:            63072000,
			FrameDeny:             true,
			ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
			ContentTypeNosniff:    true,
		}))

		// Add authentication middleware
		dynamicRoutes.Use(authenticationMiddleware(cfg.Dashboard.AuthMode))

		// Auth routes
		auth := dynamicRoutes.Group("/api/auth")
		{
			h := authHandlers{app}
			auth.POST("/login", h.LoginPOST)
			auth.POST("/logout", h.LogoutPOST)
			auth.GET("/session", h.SessionGET)
		}

		// Protected routes
		protectedRoutes := dynamicRoutes.Group("")
		{
			// Add K8S auth middleware
			protectedRoutes.Use(k8sAuthenticationMiddleware(cfg.Dashboard.AuthMode))

			// GraphQL endpoint
			app.graphqlServer = graph.NewServer(cfg, app.cm)
			protectedRoutes.Any("/graphql", gin.WrapH(app.graphqlServer))

			// Cluster API proxy routes
			protectedRoutes.Any("/cluster-api-proxy/*path", gin.WrapH(app.clusterAPIProxy))
		}
	}
	app.dynamicRoutes = dynamicRoutes

	// Init staticFS
	staticFS, err := fs.Sub(dashboard.StaticEmbedFS, "static")
	if err != nil {
		return nil, err
	}
	staticHttpFS := http.FS(staticFS)

	// GraphQL Playground
	root.StaticFileFS("/graphiql", "/graphiql.html", staticHttpFS)

	// Robots.txt
	root.StaticFileFS("/robots.txt", "/robots.txt", staticHttpFS)

	// Health endpoint
	root.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Serve website from "/" and also unknown routes
	websiteFS, err := fs.Sub(dashboard.WebsiteEmbedFS, "website")
	if err != nil {
		return nil, err
	}

	h := websiteHandlers{app, websiteFS}
	h.InitStaticHandlers(root)

	endpointHandler := h.EndpointHandler(cfg)
	root.GET("/", endpointHandler)
	app.NoRoute(endpointHandler)

	return app, nil
}
