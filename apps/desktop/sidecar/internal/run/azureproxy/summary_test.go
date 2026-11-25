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

package azureproxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// Each summary is one line naming the cloud platform and where the request
// reaches.
func TestASummaryNamesTheCloudPlatformAndWhere(t *testing.T) {
	payg := Subscription{ID: sub, Name: "Pay-As-You-Go"}
	for target, want := range map[string]string{
		"DELETE " + rg:               "Azure: delete resource group rg in Pay-As-You-Go",
		"PUT " + compute:             "Azure: PUT Microsoft.Compute/virtualMachines/vm-1 in rg",
		"POST " + compute + "/start": "Azure: POST Microsoft.Compute/virtualMachines/vm-1/start in rg",
		"POST /subscriptions/" + sub + "/providers/Microsoft.Compute/register": "Azure: POST Microsoft.Compute/register in Pay-As-You-Go",
		"PUT " + rg + "/tags":         "Azure: PUT tags in rg",
		"PATCH /subscriptions/" + sub: "Azure: PATCH " + sub + " in Pay-As-You-Go",
	} {
		method, path, _ := strings.Cut(target, " ")
		a, ok := parsePath(method, path)
		require.True(t, ok, target)
		assert.Equal(t, want, summary(method, a, verdict{level: permissions.Write}, payg), target)
	}
}

// A path with no group or resource draws the subscription alone.
func TestDetailsDrawWhatThePathNames(t *testing.T) {
	a, _ := parsePath(http.MethodPost, "/subscriptions/"+sub+"/providers/Microsoft.Compute/register")
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	assert.Equal(t, []runproxy.Detail{
		{Label: "Subscription", Value: "Dev (" + sub + ")"},
		{Label: "Resource", Value: "Microsoft.Compute"},
	}, details(r, a, Subscription{ID: sub, Name: "Dev"}))
}
