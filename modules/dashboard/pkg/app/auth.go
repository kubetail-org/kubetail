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
	"net/http"
	"slices"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/modules/dashboard/internal/formerrors"
	"github.com/kubetail-org/kubetail/modules/dashboard/pkg/config"
	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

// Represents login form
type loginForm struct {
	Token      string   `form:"token" binding:"required" errors_required:"Please enter your token"`
	Namespaces []string `form:"namespaces" binding:"omitempty,dive,required" errors_required:"Namespace names must not be empty"`
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
				"token":      formErrors.Get("Token"),
				"namespaces": formErrors.Get("Namespaces"),
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

	// Optional login-time namespace narrowing (behind allow-namespace-override)
	if !app.config.AllowNamespaceOverride {
		form.Namespaces = nil
	} else if len(app.config.AllowedNamespaces) > 0 {
		// The requested namespaces may only shrink the operator's static
		// allow-list, never widen it
		for _, ns := range form.Namespaces {
			if !slices.Contains(app.config.AllowedNamespaces, ns) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{
					"errors": gin.H{
						"namespaces": "One or more requested namespaces are not in the allowed-namespaces list",
					},
				})
				return
			}
		}
	}

	// Add data to session (for middleware)
	session := sessions.Default(c)
	session.Set(k8sTokenSessionKey, form.Token)
	if len(form.Namespaces) > 0 {
		session.Set(k8sNamespacesSessionKey, form.Namespaces)
	}

	// Rotate CSRF token on auth state change to prevent fixation.
	session.Delete(csrfTokenSessionKey)

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
	// Clear() also drops csrfTokenSessionKey, rotating the CSRF token.
	session.Clear()
	session.Save()

	c.AbortWithStatus(http.StatusNoContent)
}

// Session endpoint
func (app *authHandlers) SessionGET(c *gin.Context) {
	authMode := app.config.AuthMode

	session := sessions.Default(c)
	token, isNew := getOrCreateCSRFToken(session)
	if isNew {
		if err := session.Save(); err != nil {
			c.String(http.StatusInternalServerError, err.Error())
			return
		}
	}
	c.Header("X-CSRF-Token", token)

	response := gin.H{
		"auth_mode":      authMode,
		"user":           nil,
		"message":        nil,
		"namespace_lock": nil,
		"timestamp":      time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Report the effective login-time namespace lock, if any, so the UI can
	// reflect it. Omitted (null) when the override feature is disabled, the
	// session set no override, or the override no longer narrows anything.
	if app.config.AllowNamespaceOverride {
		if override, ok := session.Get(k8sNamespacesSessionKey).([]string); ok && len(override) > 0 {
			effective := k8shelpers.NarrowAllowedNamespaces(app.config.AllowedNamespaces, override)
			if len(app.config.AllowedNamespaces) == 0 || !slices.Equal(effective, app.config.AllowedNamespaces) {
				response["namespace_lock"] = effective
			}
		}
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
