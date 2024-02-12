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
	"net/http"
	"os"
	"path"

	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/requestid"
	"github.com/gin-contrib/secure"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/csrf"
	adapter "github.com/gwatts/gin-adapter"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/internal/k8shelpers"
)

type GinApp struct {
	*gin.Engine
	k8sHelperService k8shelpers.Service

	// for testing
	dynamicroutes *gin.RouterGroup
	wraponce      gin.HandlerFunc
}

// Create new kubetail Gin app
func NewGinApp(config Config) (*GinApp, error) {
	// init app
	app := &GinApp{Engine: gin.New()}

	// only if not in test-mode
	var k8sCfg *rest.Config
	if gin.Mode() != gin.TestMode {
		// configure kubernetes
		k8sCfg = mustConfigureK8S(config)

		// init k8s helper service
		app.k8sHelperService = k8shelpers.NewK8sHelperService(k8sCfg, k8shelpers.Mode(config.AuthMode))

		// add recovery middleware
		app.Use(gin.Recovery())
	}

	// for tests
	if gin.Mode() == gin.TestMode {
		app.Use(func(c *gin.Context) {
			if app.wraponce != nil {
				defer func() { app.wraponce = nil }()
				app.wraponce(c)
			} else {
				c.Next()
			}
		})
	}

	// add request-id middleware
	app.Use(requestid.New())

	// add logging middleware
	if config.AccessLogEnabled {
		app.Use(loggingMiddleware())
	}

	// gzip middleware
	app.Use(gzip.Gzip(gzip.DefaultCompression))

	// dynamic routes
	dynamicRoutes := app.Group("/")
	{
		// session middleware
		sessionStore := cookie.NewStore([]byte(config.Session.Secret))
		sessionStore.Options(sessions.Options{
			Path:     config.Session.Cookie.Path,
			Domain:   config.Session.Cookie.Domain,
			MaxAge:   config.Session.Cookie.MaxAge,
			Secure:   config.Session.Cookie.Secure,
			HttpOnly: config.Session.Cookie.HttpOnly,
			SameSite: config.Session.Cookie.SameSite,
		})
		dynamicRoutes.Use(sessions.Sessions("session", sessionStore))

		// https://security.stackexchange.com/questions/147554/security-headers-for-a-web-api
		// https://observatory.mozilla.org/faq/
		dynamicRoutes.Use(secure.New(secure.Config{
			STSSeconds:            63072000,
			FrameDeny:             true,
			ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
			ContentTypeNosniff:    true,
		}))

		// disable csrf protection for graphql endpoint (already rejects simple requests)
		dynamicRoutes.Use(func(c *gin.Context) {
			if c.Request.URL.Path == "/graphql" {
				c.Request = csrf.UnsafeSkipCheck(c.Request)
			}
			c.Next()
		})

		// csrf middleware
		if config.CSRF.Enabled {
			dynamicRoutes.Use(adapter.Wrap(csrf.Protect(
				[]byte(config.CSRF.Secret),
				csrf.FieldName(config.CSRF.FieldName),
				csrf.CookieName(config.CSRF.Cookie.Name),
				csrf.Path(config.CSRF.Cookie.Path),
				csrf.Domain(config.CSRF.Cookie.Domain),
				csrf.MaxAge(config.CSRF.Cookie.MaxAge),
				csrf.Secure(config.CSRF.Cookie.Secure),
				csrf.HttpOnly(config.CSRF.Cookie.HttpOnly),
				csrf.SameSite(config.CSRF.Cookie.SameSite),
			)))

			dynamicRoutes.GET("/csrf-token", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"value": csrf.Token(c.Request)})
			})
		}

		// authentication middleware
		dynamicRoutes.Use(authenticationMiddleware(config.AuthMode))

		// auth routes
		auth := dynamicRoutes.Group("/api/auth")
		{
			h := &AuthHandlers{GinApp: app, mode: config.AuthMode}
			auth.POST("/login", h.LoginPOST)
			auth.POST("/logout", h.LogoutPOST)
			auth.GET("/session", h.SessionGET)
		}

		// graphql routes
		graphql := dynamicRoutes.Group("/graphql")
		{
			// require token
			if config.AuthMode == AuthModeToken {
				graphql.Use(k8sTokenRequiredMiddleware)
			}

			// graphql handler
			h := &GraphQLHandlers{app}
			endpointHandler := h.EndpointHandler(k8sCfg, config.Namespace)
			graphql.GET("", endpointHandler)
			graphql.POST("", endpointHandler)
		}
	}
	app.dynamicroutes = dynamicRoutes // for unit tests

	// graphiql
	h := playground.Handler("GraphQL Playground", "/graphql")
	app.GET("/graphiql", gin.WrapH(h))

	// healthz
	app.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// static files (react app)
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	websiteDir := path.Join(cwd, "/website")

	app.StaticFile("/", path.Join(websiteDir, "/index.html"))
	app.StaticFile("/favicon.ico", path.Join(websiteDir, "/favicon.ico"))
	app.Static("/assets", path.Join(websiteDir, "/assets"))

	// use react app for unknown routes
	app.NoRoute(func(c *gin.Context) {
		c.File(path.Join(websiteDir, "/index.html"))
	})

	return app, nil
}
