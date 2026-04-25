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
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	zlog "github.com/rs/zerolog/log"

	"github.com/kubetail-org/kubetail/modules/shared/logs"
)

// downloadHandlers wires the production log stream factory against the app's
// connection manager and threads allowedNamespaces through from config.
type downloadHandlers struct {
	*App
	newLogStream      logs.NewDownloadStreamFn
	allowedNamespaces []string
}

func newDownloadHandlers(app *App) *downloadHandlers {
	return &downloadHandlers{
		App: app,
		newLogStream: func(ctx context.Context, sources []string, opts ...logs.Option) (logs.DownloadStreamer, error) {
			return logs.NewStream(ctx, app.cm, sources, opts...)
		},
		allowedNamespaces: app.config.AllowedNamespaces,
	}
}

// Log download endpoint
func (h *downloadHandlers) DownloadPOST(c *gin.Context) {
	var form logs.DownloadForm
	if err := c.ShouldBind(&form); err != nil {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
			"error": err.Error(),
		})
		return
	}

	req, verr := form.Validate()
	if verr != nil {
		c.AbortWithStatusJSON(http.StatusUnprocessableEntity, gin.H{
			"field":   verr.Field,
			"message": verr.Message,
		})
		return
	}

	opts := logs.BuildDownloadStreamOptions(req, c.GetString(k8sTokenGinKey), h.allowedNamespaces)

	ctx := c.Request.Context()
	stream, err := h.newLogStream(ctx, req.Raw.Sources, opts...)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer stream.Close()

	if err := stream.Start(ctx); err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", logs.DownloadContentType(req.Raw.OutputFormat))
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, logs.DownloadFilename(req.Raw.OutputFormat, time.Now())))
	c.Status(http.StatusOK)

	if err := logs.WriteDownloadStream(ctx, c.Writer, req, stream); err != nil && ctx.Err() == nil {
		// Status + headers were already sent so we can't change the response
		// code; clients will see a truncated file. Log so operators can
		// investigate. Skip when ctx is already cancelled (client-side abort).
		zlog.Error().Err(err).Msg("download stream ended with error")
	}
}
