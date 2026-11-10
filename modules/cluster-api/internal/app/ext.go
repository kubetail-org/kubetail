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

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// extGroupDiscoveryHandler serves the Kubernetes API extension group discovery
// document at /apis. The kube-apiserver fetches this when it aggregates the
// APIService registration.
func extGroupDiscoveryHandler(c *gin.Context) {
	c.JSON(http.StatusOK, &metav1.APIGroup{
		Name: "api.kubetail.com",
		Versions: []metav1.GroupVersionForDiscovery{
			{GroupVersion: "api.kubetail.com/v1", Version: "v1"},
		},
		PreferredVersion: metav1.GroupVersionForDiscovery{
			GroupVersion: "api.kubetail.com/v1",
			Version:      "v1",
		},
	})
}

// extVersionDiscoveryHandler serves the Kubernetes API extension version
// discovery document at /apis/api.kubetail.com/v1.
func extVersionDiscoveryHandler(c *gin.Context) {
	c.JSON(http.StatusOK, &metav1.APIResourceList{
		GroupVersion: "api.kubetail.com/v1",
		APIResources: []metav1.APIResource{
			{
				Name:       "graphql",
				Namespaced: false,
				Kind:       "GraphQL",
				Verbs:      []string{"get", "create"},
			},
			{
				Name:       "download",
				Namespaced: false,
				Kind:       "Download",
				Verbs:      []string{"create"},
			},
			{
				Name:       "healthz",
				Namespaced: false,
				Kind:       "Healthz",
				Verbs:      []string{"get", "list"},
			},
		},
	})
}
