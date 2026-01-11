// Copyright 2024-2026 The Kubetail Authors
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
	"net/http"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/shared/config"

	"github.com/kubetail-org/kubetail/modules/dashboard/internal/formerrors"
)

// Represents login form
type loginForm struct {
	Token string `form:"token" binding:"required" errors_required:"Please enter your token"`
}

// Represents auth handlers
type authHandlers struct {
	*App
}

// Login endpoint
func (app *authHandlers) LoginPOST(c *gin.Context) {
	var form loginForm

	// Validate form
	err := c.ShouldBind(&form)
	if err != nil {
		formErrors := formerrors.New(&form, err)
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"errors": gin.H{
				"token": formErrors.Get("Token"),
			},
		})
		return
	}

	// Authenticate
	tokenReview, err := app.queryHelpers.HasAccess(c.Request.Context(), form.Token)
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	// Check result
	if !tokenReview.Status.Authenticated {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"errors": gin.H{
				"token": "Please enter a valid token",
			},
		})
		return
	}

	// Add data to session (for middleware)
	session := sessions.Default(c)
	session.Set(k8sTokenSessionKey, form.Token)

	// Save
	err = session.Save()
	if err != nil {
		c.String(http.StatusInternalServerError, err.Error())
		return
	}

	c.AbortWithStatus(http.StatusNoContent)
}

// Logout endpoint
func (app *authHandlers) LogoutPOST(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()

	c.AbortWithStatus(http.StatusNoContent)
}

// Session endpoint
func (app *authHandlers) SessionGET(c *gin.Context) {
	authMode := app.config.Dashboard.AuthMode

	response := gin.H{
		"auth_mode": authMode,
		"user":      nil,
		"message":   nil,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}

	switch authMode {
	case config.AuthModeAuto:
		response["user"] = string(authMode)
	case config.AuthModeToken:
		token := c.GetString(k8sTokenGinKey)

		// Handle no token found
		if token == "" {
			break
		}

		// Check token
		tokenReview, err := app.queryHelpers.HasAccess(c.Request.Context(), token)
		if err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}

		// Check result
		if tokenReview.Status.Authenticated {
			response["user"] = tokenReview.Status.User.Username
		} else {
			response["message"] = tokenReview.Status.Error
			response["user"] = nil
		}
	default:
		panic("not implemented")
	}

	c.JSON(http.StatusOK, response)
}
