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
	grpcdispatcher "github.com/kubetail-org/grpc-dispatcher-go"
	zlog "github.com/rs/zerolog/log"
	"k8s.io/client-go/rest"

	"github.com/kubetail-org/kubetail/backend/common/config"
	"github.com/kubetail-org/kubetail/backend/server/internal/k8shelpers"
)

type GinApp struct {
	*gin.Engine
	k8sHelperService k8shelpers.Service
	grpcDispatcher   *grpcdispatcher.Dispatcher
	shutdownCh       chan struct{}

	// for testing
	dynamicroutes *gin.RouterGroup
	wraponce      gin.HandlerFunc
}

func (app *GinApp) Shutdown() {
	// stop grpc dispatcher
	if app.grpcDispatcher != nil {
		// TODO: log dispatcher shutdown errors
		app.grpcDispatcher.Shutdown()
	}

	// send shutdown signal to internal processes
	if app.shutdownCh != nil {
		close(app.shutdownCh)
	}
}

// Create new kubetail Gin app
func NewGinApp(cfg *config.Config) (*GinApp, error) {
	// init app
	app := &GinApp{Engine: gin.New()}

	// only if not in test-mode
	var k8sCfg *rest.Config
	if gin.Mode() != gin.TestMode {
		// configure kubernetes
		k8sCfg = mustConfigureK8S(cfg)

		// init k8s helper service
		app.k8sHelperService = k8shelpers.NewK8sHelperService(k8sCfg, k8shelpers.Mode(cfg.AuthMode))

		// init grpc dispatcher
		app.grpcDispatcher = mustNewGrpcDispatcher(cfg)

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
			zlog.Fatal().Err(err).Send()
		}
		basepath = basepathTmp
	}

	// register templates
	app.SetHTMLTemplate(mustLoadTemplatesWithFuncs(path.Join(basepath, "templates/*")))

	// add request-id middleware
	app.Use(requestid.New())

	// add logging middleware
	if cfg.Server.Logging.AccessLog.Enabled {
		app.Use(loggingMiddleware(cfg.Server.Logging.AccessLog.HideHealthChecks))
	}

	// gzip middleware
	app.Use(gzip.Gzip(gzip.DefaultCompression))

	// root route
	root := app.Group(cfg.Server.BasePath)

	// dynamic routes
	dynamicRoutes := root.Group("/")
	{
		// session middleware
		sessionStore := cookie.NewStore([]byte(cfg.Server.Session.Secret))
		sessionStore.Options(sessions.Options{
			Path:     cfg.Server.Session.Cookie.Path,
			Domain:   cfg.Server.Session.Cookie.Domain,
			MaxAge:   cfg.Server.Session.Cookie.MaxAge,
			Secure:   cfg.Server.Session.Cookie.Secure,
			HttpOnly: cfg.Server.Session.Cookie.HttpOnly,
			SameSite: cfg.Server.Session.Cookie.SameSite,
		})
		dynamicRoutes.Use(sessions.Sessions(cfg.Server.Session.Cookie.Name, sessionStore))

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
			if c.Request.URL.Path == path.Join(cfg.Server.BasePath, "/graphql") {
				c.Request = csrf.UnsafeSkipCheck(c.Request)
			}
			c.Next()
		})

		var csrfProtect func(http.Handler) http.Handler

		// csrf middleware
		if cfg.Server.CSRF.Enabled {
			csrfProtect = csrf.Protect(
				[]byte(cfg.Server.CSRF.Secret),
				csrf.FieldName(cfg.Server.CSRF.FieldName),
				csrf.CookieName(cfg.Server.CSRF.Cookie.Name),
				csrf.Path(cfg.Server.CSRF.Cookie.Path),
				csrf.Domain(cfg.Server.CSRF.Cookie.Domain),
				csrf.MaxAge(cfg.Server.CSRF.Cookie.MaxAge),
				csrf.Secure(cfg.Server.CSRF.Cookie.Secure),
				csrf.HttpOnly(cfg.Server.CSRF.Cookie.HttpOnly),
				csrf.SameSite(cfg.Server.CSRF.Cookie.SameSite),
			)

			// add to gin middleware
			dynamicRoutes.Use(adapter.Wrap(csrfProtect))

			// token fetcher helper
			dynamicRoutes.GET("/csrf-token", func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"value": csrf.Token(c.Request)})
			})
		}

		// authentication middleware
		dynamicRoutes.Use(authenticationMiddleware(cfg.AuthMode))

		// auth routes
		auth := dynamicRoutes.Group("/api/auth")
		{
			h := &AuthHandlers{GinApp: app, mode: cfg.AuthMode}
			auth.POST("/login", h.LoginPOST)
			auth.POST("/logout", h.LogoutPOST)
			auth.GET("/session", h.SessionGET)
		}

		// graphql routes
		graphql := dynamicRoutes.Group("/graphql")
		{
			// require token
			if cfg.AuthMode == config.AuthModeToken {
				graphql.Use(k8sTokenRequiredMiddleware)
			}

			// graphql handler
			h := &GraphQLHandlers{app}
			endpointHandler := h.EndpointHandler(k8sCfg, app.grpcDispatcher, cfg.AllowedNamespaces, csrfProtect)
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

	endpointHandler := h.EndpointHandler(cfg)
	root.GET("/", endpointHandler)
	app.NoRoute(endpointHandler)

	return app, nil
}
