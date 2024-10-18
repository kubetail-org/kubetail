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
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	zlog "github.com/rs/zerolog/log"

	"github.com/kubetail-org/kubetail/backend/common/config"
)

type WebsiteHandlers struct {
	*GinApp
	//embedFS *embed.FS
	websiteFS fs.FS
}

func (app *WebsiteHandlers) InitStaticHandlers(root *gin.RouterGroup) {
	// add top-level files
	httpFS := http.FS(app.websiteFS)
	root.StaticFileFS("/favicon.ico", "/favicon.ico", httpFS)
	root.StaticFileFS("/graphiql", "/graphiql.html", httpFS)

	// add assets directory
	if assetsFS, err := fs.Sub(app.websiteFS, "assets"); err == nil {
		root.StaticFS("/assets", http.FS(assetsFS))
	}
}

func (app *WebsiteHandlers) EndpointHandler(cfg *config.Config) gin.HandlerFunc {
	// read manifest file
	manifestFile, err := app.websiteFS.Open(".vite/manifest.json")
	if err != nil {
		return func(c *gin.Context) {
			c.JSON(http.StatusNotFound, gin.H{
				"status": "website not found",
			})
		}
	}
	defer manifestFile.Close()

	// parse manifest json
	manifest := gin.H{}
	decoder := json.NewDecoder(manifestFile)
	err = decoder.Decode(&manifest)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}

	// define runtime config for react app
	runtimeConfig := map[string]interface{}{
		"basePath":          cfg.Server.BasePath,
		"extensionsEnabled": cfg.Server.ExtensionsEnabled,
	}

	runtimeConfigBytes, err := json.Marshal(runtimeConfig)
	if err != nil {
		zlog.Fatal().Err(err).Send()
	}
	runtimeConfigJS := template.JS(string(runtimeConfigBytes))

	return func(c *gin.Context) {
		// reject non-GET/HEAD requests
		if slices.Contains([]string{"GET", "HEAD"}, c.Request.Method) {
			c.HTML(http.StatusOK, "index.tmpl", gin.H{
				"config":        cfg,
				"manifest":      manifest,
				"runtimeConfig": template.JS(runtimeConfigJS),
			})
		} else {
			c.JSON(http.StatusNotFound, gin.H{
				"message": "Resource not found",
			})
		}
	}
}
