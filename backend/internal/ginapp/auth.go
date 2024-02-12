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
	"errors"
	"net/http"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/kubetail-org/kubetail/internal/formerrors"
)

type AuthMode string

const (
	AuthModeCluster = "cluster"
	AuthModeToken   = "token"
	AuthModeLocal   = "local"
)

type LoginForm struct {
	Token string `form:"token" binding:"required" errors_required:"Please enter your token"`
}

type AuthHandlers struct {
	*GinApp
	mode AuthMode
}

// Login endpoint
func (app *AuthHandlers) LoginPOST(c *gin.Context) {
	var form LoginForm

	// validate form
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

	// authenticate
	_, err = app.k8sHelperService.HasAccess(form.Token)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"errors": gin.H{
				"token": "Please enter a valid token",
			},
		})
		return
	}

	// add data to session (for middleware)
	session := sessions.Default(c)
	session.Set(k8sTokenSessionKey, form.Token)
	session.Save()

	c.Status(http.StatusNoContent)
}

// Logout endpoint
func (app *AuthHandlers) LogoutPOST(c *gin.Context) {
	session := sessions.Default(c)
	session.Clear()
	session.Save()

	c.Status(http.StatusNoContent)
}

// Session endpoint
func (app *AuthHandlers) SessionGET(c *gin.Context) {
	response := gin.H{
		"auth_mode": app.mode,
		"user":      nil,
		"message":   nil,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}

	switch app.mode {
	case AuthModeCluster, AuthModeLocal:
		response["user"] = string(app.mode)
	case AuthModeToken:
		token := c.GetString(k8sTokenCtxKey)

		// no token found
		if token == "" {
			break
		}

		// check token
		user, err := app.k8sHelperService.HasAccess(token)
		if err == nil {
			response["user"] = user
		} else {
			response["message"] = err.Error()
			response["user"] = nil
		}
	default:
		panic(errors.New("not implemented"))
	}

	c.JSON(http.StatusOK, response)
}
