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
	"path/filepath"
	"runtime"

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

	var basepath string

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

		// set basepath relative to this file
		_, b, _, _ := runtime.Caller(0)
		basepath = path.Join(filepath.Dir(b), "../../")
	} else {
		// set basepath to cwd
		basepathTmp, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		basepath = basepathTmp
	}

	// register templates
	app.SetHTMLTemplate(mustLoadTemplatesWithFuncs(path.Join(basepath, "templates/*")))

	// add request-id middleware
	app.Use(requestid.New())

	// add logging middleware
	if config.AccessLog.Enabled {
		app.Use(loggingMiddleware(config.AccessLog.HideHealthChecks))
	}

	// gzip middleware
	app.Use(gzip.Gzip(gzip.DefaultCompression))

	// root route
	root := app.Group(config.BasePath)

	// dynamic routes
	dynamicRoutes := root.Group("/")
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
		dynamicRoutes.Use(sessions.Sessions(config.Session.Cookie.Name, sessionStore))

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
			if c.Request.URL.Path == path.Join(config.BasePath, "/graphql") {
				c.Request = csrf.UnsafeSkipCheck(c.Request)
			}
			c.Next()
		})

		var csrfProtect func(http.Handler) http.Handler

		// csrf middleware
		if config.CSRF.Enabled {
			csrfProtect = csrf.Protect(
				[]byte(config.CSRF.Secret),
				csrf.FieldName(config.CSRF.FieldName),
				csrf.CookieName(config.CSRF.Cookie.Name),
				csrf.Path(config.CSRF.Cookie.Path),
				csrf.Domain(config.CSRF.Cookie.Domain),
				csrf.MaxAge(config.CSRF.Cookie.MaxAge),
				csrf.Secure(config.CSRF.Cookie.Secure),
				csrf.HttpOnly(config.CSRF.Cookie.HttpOnly),
				csrf.SameSite(config.CSRF.Cookie.SameSite),
			)

			// add to gin middleware
			dynamicRoutes.Use(adapter.Wrap(csrfProtect))

			// token fetcher helper
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
			endpointHandler := h.EndpointHandler(k8sCfg, config.Namespace, csrfProtect)
			graphql.GET("", endpointHandler)
			graphql.POST("", endpointHandler)
		}
	}
	app.dynamicroutes = dynamicRoutes // for unit tests

	// health routes
	root.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
		})
	})

	// serve website from "/" and also unknown routes
	h := &WebsiteHandlers{app, path.Join(basepath, "/website")}
	h.InitStaticHandlers(root)

	endpointHandler := h.EndpointHandler(config)
	root.GET("/", endpointHandler)
	app.NoRoute(endpointHandler)

	return app, nil
}
