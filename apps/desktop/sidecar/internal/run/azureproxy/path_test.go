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
	"testing"

	"github.com/stretchr/testify/assert"
)

const sub = "00000000-0000-0000-0000-000000000001"

// Each path is read by Resource Manager's grammar, in any case, or is one
// of the polling paths; anything else is refused.
func TestThePathIsReadByResourceManagersGrammar(t *testing.T) {
	for _, c := range []struct {
		method, path string
		want         *armPath // nil: refused
	}{
		{"GET", "/subscriptions", &armPath{last: "subscriptions"}},
		{"GET", "/tenants", &armPath{last: "tenants"}},
		{"GET", "/subscriptions/" + sub, &armPath{subscription: sub}},
		{"GET", "/subscriptions/" + sub + "/resourceGroups", &armPath{subscription: sub, last: "resourceGroups"}},
		{"DELETE", "/subscriptions/" + sub + "/resourcegroups/rg", &armPath{subscription: sub, group: "rg"}},
		{"GET", "/subscriptions/" + sub + "/providers", &armPath{subscription: sub, last: "providers"}},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Compute/virtualMachines", &armPath{
			subscription: sub, scopes: []scope{{namespace: "Microsoft.Compute"}}, last: "virtualMachines",
		}},
		{"POST", "/SUBSCRIPTIONS/" + sub + "/RESOURCEGROUPS/rg/PROVIDERS/Microsoft.Compute/virtualMachines/vm-1/start", &armPath{
			subscription: sub, group: "rg",
			scopes: []scope{{namespace: "Microsoft.Compute", pairs: []pair{{"virtualMachines", "vm-1"}}}}, last: "start",
		}},
		{"PUT", "/subscriptions/" + sub + "/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm/providers/Microsoft.Authorization/roleAssignments/ra", &armPath{
			subscription: sub, group: "rg", scopes: []scope{
				{namespace: "Microsoft.Compute", pairs: []pair{{"virtualMachines", "vm"}}},
				{namespace: "Microsoft.Authorization", pairs: []pair{{"roleAssignments", "ra"}}},
			},
		}},
		{"GET", "/providers/Microsoft.Management/managementGroups/mg", &armPath{
			scopes: []scope{{namespace: "Microsoft.Management", pairs: []pair{{"managementGroups", "mg"}}}},
		}},
		{"GET", "/subscriptions/" + sub + "/operationresults/abc", &armPath{subscription: sub}},
		{"GET", "/subscriptions/" + sub + "/resourceGroups/rg/operationResults/abc", &armPath{subscription: sub, group: "rg"}},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Compute/locations/westus/operations/op", &armPath{
			subscription: sub, scopes: []scope{{namespace: "Microsoft.Compute", pairs: []pair{{"locations", "westus"}, {"operations", "op"}}}},
		}},
		{"DELETE", "/subscriptions/" + sub + "/operationresults/abc", nil},
		{"POST", "/subscriptions/" + sub + "/resourceGroups/rg/operationResults/abc", nil},
		{"GET", "/batch", nil},
		{"GET", "/", nil},
		{"GET", "/providers", nil},
		{"GET", "/subscriptions/" + sub + "/a/b/c", nil},
		{"GET", "/subscriptions/" + sub + "/resourceGroups/rg/x/y", nil},
		{"GET", "/subscriptions//resourceGroups", nil},
		{"GET", "/subscriptions/" + sub + "/", nil},
		{"GET", "/subscriptions/" + sub + "/../x", nil},
		{"GET", "/subscriptions/" + sub + "/./x", nil},
		{"GET", "subscriptions", nil},
	} {
		got, ok := parsePath(c.method, c.path)
		if c.want == nil {
			assert.False(t, ok, c.method+" "+c.path)
			continue
		}
		if assert.True(t, ok, c.method+" "+c.path) {
			assert.Equal(t, *c.want, got, c.method+" "+c.path)
		}
	}
}

func TestAPathNamesItsResource(t *testing.T) {
	p, _ := parsePath("GET", "/subscriptions/"+sub+"/resourceGroups/rg/providers/Microsoft.Web/sites/shop/config/appsettings/list")
	assert.Equal(t, "Microsoft.Web/sites/shop/config/appsettings", p.resource())
	assert.Equal(t, "list", p.final())
	p, _ = parsePath("GET", "/subscriptions/"+sub+"/resourceGroups/rg")
	assert.Empty(t, p.resource())
	assert.Equal(t, "rg", p.final())
	p, _ = parsePath("GET", "/subscriptions/"+sub+"/providers/Microsoft.Compute")
	assert.Equal(t, "Microsoft.Compute", p.resource())
	assert.Equal(t, "Microsoft.Compute", p.final())
}

// A namespace, type or action outside ASCII is refused: Resource Manager
// folds case as .NET does, where ſ is s, so publiſhxml would reach
// publishxml past the classifier. A resource's own name may be any text.
func TestAWordOutsideASCIIIsRefused(t *testing.T) {
	for _, p := range []string{
		"/subscriptions/" + sub + "/resourceGroups/rg/providers/Microsoft.Web/sites/s/publi\u017fhxml",
		"/subscriptions/" + sub + "/resourceGroups/rg/providers/Micro\u017foft.Web/sites/s",
		"/subscriptions/" + sub + "/resourceGroups/rg/providers/Microsoft.Web/\u017fites/s",
		"/\u017fubscriptions/" + sub,
	} {
		_, ok := parsePath("POST", p)
		assert.False(t, ok, p)
	}
	_, ok := parsePath("GET", "/subscriptions/"+sub+"/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm-\u00fc")
	assert.True(t, ok, "a resource's name may be any text")
}
