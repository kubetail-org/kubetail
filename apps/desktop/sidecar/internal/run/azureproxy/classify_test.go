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
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

const (
	rg      = "/subscriptions/" + sub + "/resourceGroups/rg"
	compute = rg + "/providers/Microsoft.Compute/virtualMachines/vm-1"
	aks     = rg + "/providers/Microsoft.ContainerService/managedClusters/aks-1"
	site    = rg + "/providers/Microsoft.Web/sites/shop"
)

// The expected reading of a request: a refusal, or its access level.
const (
	isRefused     = "refused"
	isRead        = string(permissions.Read)
	isWrite       = string(permissions.Write)
	isDestructive = string(permissions.Destructive)
	isPermissions = string(permissions.AccessControl)
	isDataRead    = string(permissions.DataRead)
)

// readingOf classifies method and target, a path with its query, as the
// handler does: the path policy first.
func readingOf(t *testing.T, method, target string) string {
	t.Helper()
	p, query, _ := strings.Cut(target, "?")
	a, ok := parsePath(method, p)
	if !ok {
		return isRefused
	}
	q, err := url.ParseQuery(query)
	require.NoError(t, err)
	v := classify(method, a, q)
	if v.refusal != "" {
		return isRefused
	}
	return string(v.level)
}

// One table over the path policy and the classifier's rows. Each request is
// also tried in upper and lower case, which Resource Manager reads alike.
func TestEveryRequestIsClassified(t *testing.T) {
	for _, c := range []struct{ method, target, want string }{
		// The path policy.
		{"GET", "/batch", isRefused},
		{"POST", "/batch", isRefused},
		{"GET", "/subscriptions/" + sub + "/a/b/c", isRefused},
		{"OPTIONS", compute, isRefused},
		{"TRACE", compute, isRefused},
		// Row 1: AKS run-command.
		{"POST", aks + "/runCommand", isRefused},
		{"GET", aks + "/commandResults/c1", isRefused},
		// Row 2: AKS access profiles.
		{"GET", aks + "/accessProfiles/clusterAdmin", isRefused},
		{"POST", aks + "/accessProfiles/clusterUser/listCredential", isRefused},
		// Row 3: secret metadata, a data read.
		{"POST", site + "/config/appsettings/list", isDataRead},
		{"POST", site + "/slots/staging/config/appsettings/list", isDataRead},
		{"POST", site + "/config/connectionstrings/list", isDataRead},
		{"GET", compute + "?$expand=userData", isDataRead},
		{"GET", compute + "?$Expand=userData", isDataRead},
		{"GET", compute + "?$EXPAND=UserData", isDataRead},
		{"GET", rg + "/providers/Microsoft.ApiManagement/service/apim/subscriptions", isDataRead},
		{"GET", rg + "/providers/Microsoft.ApiManagement/service/apim/subscriptions/s1", isDataRead},
		{"GET", rg + "/providers/Microsoft.ApiManagement/service/apim/users/u/subscriptions", isDataRead},
		{"GET", rg + "/providers/Microsoft.ApiManagement/service/apim", isRead},
		{"GET", site + "/hybridConnectionNamespaces/ns/relays/r", isDataRead},
		{"GET", site + "/slots/staging/hybridConnectionNamespaces/ns/relays/r", isDataRead},
		{"GET", rg + "/providers/Microsoft.Web/serverfarms/plan/hybridConnectionNamespaces/ns/relays/r", isDataRead},
		{"GET", rg + "/providers/Microsoft.Compute/virtualMachineScaleSets/ss?$expand=instanceView,userData", isDataRead},
		{"GET", rg + "/providers/Microsoft.Resources/deployments", isDataRead},
		{"GET", rg + "/providers/Microsoft.Resources/deployments/d1", isDataRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Resources/deployments/d1", isDataRead},
		{"POST", rg + "/providers/Microsoft.Resources/deployments/d1/exportTemplate", isDataRead},
		{"GET", rg + "/providers/Microsoft.Automation/automationAccounts/a/variables", isDataRead},
		{"GET", rg + "/providers/Microsoft.Automation/automationAccounts/a/variables/v", isDataRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.App/containerApps", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/containerApps/web", isDataRead},
		{"GET", rg + "/providers/Microsoft.ContainerInstance/containerGroups", isDataRead},
		{"GET", rg + "/providers/Microsoft.ContainerInstance/containerGroups/cg", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/containerApps/web/revisions", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/containerApps/web/revisions/web--r1", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/jobs", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/jobs/nightly", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/jobs/nightly/executions", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/jobs/nightly/executions/e1", isDataRead},
		// Row 4: an action that answers a credential.
		{"POST", rg + "/providers/Microsoft.Storage/storageAccounts/sa/listKeys", isRefused},
		{"POST", rg + "/providers/Microsoft.Storage/storageAccounts/sa/regenerateKey", isRefused},
		{"POST", rg + "/providers/Microsoft.Storage/storageAccounts/sa/listAccountSas", isRefused},
		{"POST", aks + "/listClusterAdminCredential", isRefused},
		{"POST", rg + "/providers/Microsoft.ContainerRegistry/registries/r/generateCredentials", isRefused},
		{"POST", rg + "/providers/Microsoft.Compute/sshPublicKeys/k/generateKeyPair", isRefused},
		{"POST", rg + "/providers/Microsoft.Sql/servers/s/resetPassword", isRefused},
		{"POST", rg + "/providers/Microsoft.Web/sites/shop/functions/f/listKeys", isRefused},
		{"POST", site + "/publishxml", isRefused},
		{"POST", site + "/slots/staging/publishxml", isRefused},
		{"GET", site + "/publishxml", isRefused},
		{"GET", rg + "/providers/Microsoft.DocumentDB/databaseAccounts/db/readonlykeys", isRefused},
		{"POST", rg + "/providers/Microsoft.DocumentDB/databaseAccounts/db/readonlykeys", isRefused},
		{"GET", rg + "/providers/Microsoft.DocumentDB/databaseAccounts/db", isRead},
		// Row 5: any other list or get action.
		{"POST", "/subscriptions/" + sub + "/providers/Microsoft.Compute/locations/westus/listUsages", isRefused},
		{"POST", site + "/config/publishingcredentials/list", isRefused},
		{"POST", rg + "/providers/Microsoft.Logic/workflows/w/triggers/t/listCallbackUrl", isRefused},
		{"POST", rg + "/providers/Microsoft.Network/networkWatchers/nw/getTopology", isRefused},
		// Row 6: reads.
		{"GET", "/subscriptions", isRead},
		{"GET", "/tenants", isRead},
		{"GET", "/subscriptions/" + sub + "/resourcegroups", isRead},
		{"HEAD", rg, isRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.ContainerService/managedClusters", isRead},
		{"GET", compute, isRead},
		{"GET", compute + "?$expand=instanceView", isRead},
		{"GET", rg + "/providers/Microsoft.KeyVault/vaults/kv/secrets/s", isRead},
		{"GET", rg + "/providers/Microsoft.Resources/deployments/d1/operationStatuses/op", isRead},
		{"GET", "/subscriptions/" + sub + "/operationresults/abc", isRead},
		{"GET", rg + "/operationresults/abc", isRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Compute/locations/westus/operations/op", isRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Compute/locations/westus/operationResults/op", isRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.Network/locations/westus/operationStatuses/op", isRead},
		{"GET", compute + "/operationResults/op", isRead},
		{"GET", rg + "/providers/Microsoft.App/containerApps/web/operationResults/op", isDataRead},
		{"GET", "/subscriptions/" + sub + "/providers/Microsoft.App/locations/westus/containerappOperationResults/op", isDataRead},
		{"GET", site + "/operationresults/op", isDataRead},
		{"GET", rg + "/providers/Microsoft.ContainerInstance/locations/westus/operationResults/op", isDataRead},
		{"GET", rg + "/providers/Microsoft.App/containerApps/web/operationStatuses/op", isRead},
		{"GET", compute + "/operationStatuses/op", isRead},
		{"DELETE", "/subscriptions/" + sub + "/operationresults/abc", isRefused},
		{"GET", "/subscriptions/" + sub + "/resourcegroups/rg/providers/Microsoft.Compute/virtualMachines/vm-1", isRead},
		// Row 7: access control at any scope, then destructive.
		{"DELETE", rg, isDestructive},
		{"DELETE", "/subscriptions/" + sub + "/providers/Microsoft.Authorization/roleAssignments/ra", isPermissions},
		{"DELETE", "/subscriptions/" + sub + "/providers/Microsoft.Authorization/locks/l", isPermissions},
		{"DELETE", "/providers/Microsoft.Management/managementGroups/mg", isDestructive},
		{"PUT", rg + "/providers/Microsoft.Authorization/roleAssignments/ra", isPermissions},
		{"PUT", compute + "/providers/Microsoft.Authorization/roleAssignments/ra", isPermissions},
		{"PUT", rg + "/providers/Microsoft.Authorization/locks/l", isPermissions},
		{"DELETE", aks, isDestructive},
		{"POST", compute + "/runCommand", isDestructive},
		{"PUT", compute + "/runCommands/rc", isDestructive},
		// Row 8: writes.
		{"PUT", rg, isWrite},
		{"PUT", compute, isWrite},
		{"PATCH", compute, isWrite},
		{"POST", compute + "/start", isWrite},
		{"DELETE", compute, isWrite},
		{"POST", rg + "/exportTemplate", isWrite},
		{"POST", "/subscriptions/" + sub + "/providers/Microsoft.Compute/register", isWrite},
		{"PUT", compute + "/extensions/script", isWrite},
	} {
		for _, target := range []string{c.target, upper(c.target), lower(c.target)} {
			assert.Equal(t, c.want, readingOf(t, c.method, target), c.method+" "+target)
		}
	}
}

// upper and lower change the case of a target's path, which Resource
// Manager folds, and keep its query's.
func upper(target string) string {
	p, q, found := strings.Cut(target, "?")
	if found {
		return strings.ToUpper(p) + "?" + q
	}
	return strings.ToUpper(p)
}

func lower(target string) string {
	p, q, found := strings.Cut(target, "?")
	if found {
		return strings.ToLower(p) + "?" + q
	}
	return strings.ToLower(p)
}

// actionOf is the action the handler decides for method on target with body,
// in the subscription the test account holds.
func actionOf(t *testing.T, method, target, body string) permissions.Action {
	t.Helper()
	p, query, _ := strings.Cut(target, "?")
	a, ok := parsePath(method, p)
	require.True(t, ok, target)
	q, err := url.ParseQuery(query)
	require.NoError(t, err)
	v := classify(method, a, q)
	require.Empty(t, v.refusal, target)
	return action(method, a, v, q, []byte(body), Subscription{ID: sub, Name: "Pay-As-You-Go"})
}

func TestEveryFieldIsFilled(t *testing.T) {
	for _, c := range []struct {
		method, target                     string
		scope, service, op, resource, name string
		level                              permissions.AccessLevel
	}{
		{"PUT", compute, sub + "/rg", "microsoft.compute", "write", "virtualmachines", "vm-1", permissions.Write},
		{"PATCH", compute, sub + "/rg", "microsoft.compute", "write", "virtualmachines", "vm-1", permissions.Write},
		{"DELETE", compute, sub + "/rg", "microsoft.compute", "delete", "virtualmachines", "vm-1", permissions.Write},
		{"POST", compute + "/start", sub + "/rg", "microsoft.compute", "start", "virtualmachines", "vm-1", permissions.Write},
		{"PUT", compute + "/extensions/script", sub + "/rg", "microsoft.compute", "write", "virtualmachines/extensions", "script", permissions.Write},
		{"PUT", rg, sub + "/rg", "", "write", "", "", permissions.Write},
		{"POST", "/subscriptions/" + sub + "/providers/Microsoft.Compute/register", sub, "microsoft.compute", "register", "", "", permissions.Write},
		{"DELETE", rg, sub + "/rg", "", "delete", "", "", permissions.Destructive},
		{"PUT", rg + "/providers/Microsoft.Authorization/roleAssignments/ra", sub + "/rg", "microsoft.authorization", "write", "roleassignments", "ra", permissions.AccessControl},
		{"GET", compute + "?$expand=userData", sub + "/rg", "microsoft.compute", "read", "virtualmachines", "vm-1", permissions.DataRead},
		{"POST", site + "/config/appsettings/list", sub + "/rg", "microsoft.web", "list", "sites/config", "appsettings", permissions.DataRead},
	} {
		act := actionOf(t, c.method, c.target, "")
		name := c.method + " " + c.target
		assert.Equal(t, permissions.Azure, act.Target, name)
		assert.Equal(t, c.scope, act.Scope, name)
		assert.Equal(t, sub, act.AccountName, name)
		assert.Equal(t, c.service, act.Service, name)
		assert.Equal(t, c.op, act.Operation, name)
		assert.Equal(t, c.resource, act.Resource, name)
		assert.Equal(t, c.name, act.Name, name)
		assert.Equal(t, c.level, act.AccessLevel, name)
		assert.NotEmpty(t, act.Summary, name)
	}
}

func TestAScopeIsLowerCase(t *testing.T) {
	upperSub := strings.ToUpper(sub)
	act := actionOf(t, "PUT", "/subscriptions/"+upperSub+"/resourceGroups/RG-Prod/providers/Microsoft.Compute/virtualMachines/VM-1", "{}")
	assert.Equal(t, sub+"/rg-prod", act.Scope)
	assert.Equal(t, sub, act.AccountName)
	assert.Equal(t, "microsoft.compute", act.Service)
	assert.Equal(t, "virtualmachines", act.Resource)
}

// A request whose account Kubetail cannot trust has scope "": a path that
// names no subscription, whatever default the proxy forwards with. A body or
// query naming another subscription, or a body that is not JSON, keeps the
// path's scope, so its mode and rules still decide it, and asks each time.
func TestAForeignRequestKeepsItsScopeAndAsksOnce(t *testing.T) {
	const other = "00000000-0000-0000-0000-00000000000f"
	for name, c := range map[string]struct{ method, target, body string }{
		"another subscription in a body": {"PUT", rg + "/providers/Microsoft.Authorization/roleAssignments/ra",
			`{"properties":{"scope":"/subscriptions/` + other + `/resourceGroups/x"}}`},
		"another subscription in a key":   {"PUT", compute, `{"/SUBSCRIPTIONS/` + strings.ToUpper(other) + `":1}`},
		"another subscription escaped":    {"PUT", compute, `{"id":"\/subscriptions\/` + other + `"}`},
		"another subscription in a query": {"POST", compute + "/start?target=/subscriptions/" + other, ""},
		"a body that is not JSON":         {"PUT", compute, `<xml/>`},
		"a body cut short":                {"PUT", compute, `{"a":`},
	} {
		p, query, _ := strings.Cut(c.target, "?")
		a, ok := parsePath(c.method, p)
		require.True(t, ok, name)
		q, _ := url.ParseQuery(query)
		act := action(c.method, a, classify(c.method, a, q), q, []byte(c.body), Subscription{ID: sub})
		assert.Equal(t, sub+"/rg", act.Scope, name)
		assert.True(t, act.Once, name)
	}
	a, ok := parsePath("GET", "/providers/Microsoft.Web/sites")
	require.True(t, ok)
	assert.Empty(t, action("GET", a, classify("GET", a, nil), nil, nil, Subscription{ID: sub}).Scope, "no subscription in the path")
	for name, body := range map[string]string{
		"its own subscription": `{"id":"/subscriptions/` + strings.ToUpper(sub) + `/resourceGroups/rg"}`,
		"no body":              ``,
		"no subscription":      `{"location":"westus","tags":{"a":"subscriptions"}}`,
	} {
		assert.Equal(t, sub+"/rg", actionOf(t, "PUT", compute, body).Scope, name)
	}
}

// A data read in no resource group lists every group's, so a rule for one
// group decides it; one in a group, or a subscription's own deployments, does
// not span.
func TestASubscriptionWideDataReadSpansItsGroups(t *testing.T) {
	subscription := "/subscriptions/" + sub
	assert.True(t, actionOf(t, "GET", subscription+"/providers/Microsoft.ContainerInstance/containerGroups", "").Spans)
	assert.False(t, actionOf(t, "GET", rg+"/providers/Microsoft.ContainerInstance/containerGroups", "").Spans)
	assert.False(t, actionOf(t, "GET", subscription+"/providers/Microsoft.Resources/deployments", "").Spans)
	assert.False(t, actionOf(t, "GET", subscription+"/providers/Microsoft.Compute/virtualMachines", "").Spans, "a read")
}

func TestAccessControlChanges(t *testing.T) {
	for _, target := range []string{
		rg + "/providers/Microsoft.Authorization/roleAssignments/ra",
		compute + "/providers/Microsoft.Authorization/roleAssignments/ra",
		rg + "/providers/Microsoft.Authorization/locks/l",
		"/subscriptions/" + sub + "/providers/Microsoft.Authorization/roleDefinitions/rd",
		"/subscriptions/" + sub + "/providers/Microsoft.Authorization/policyAssignments/pa",
	} {
		for _, method := range []string{"PUT", "PATCH"} {
			assert.Equal(t, permissions.AccessControl, actionOf(t, method, target, "{}").AccessLevel, method+" "+target)
		}
	}
	assert.Equal(t, permissions.AccessControl, actionOf(t, "DELETE", rg+"/providers/Microsoft.Authorization/roleAssignments/ra", "").AccessLevel)
	assert.Equal(t, permissions.Read, actionOf(t, "GET", rg+"/providers/Microsoft.Authorization/roleAssignments/ra", "").AccessLevel, "a read is a read")
}

func TestOnlyARoleAssignmentInAGroupIsConfined(t *testing.T) {
	for target, want := range map[string]bool{
		rg + "/providers/Microsoft.Authorization/roleAssignments/ra":                      true,
		compute + "/providers/Microsoft.Authorization/roleAssignments/ra":                 true,
		"/subscriptions/" + sub + "/providers/Microsoft.Authorization/roleAssignments/ra": false,
		rg + "/providers/Microsoft.Authorization/roleDefinitions/rd":                      false,
		rg + "/providers/Microsoft.Authorization/locks/l":                                 false,
		rg + "/providers/Microsoft.Authorization/policyAssignments/pa":                    false,
	} {
		act := actionOf(t, "PUT", target, "{}")
		assert.Equal(t, want, act.Confined, target)
	}
	assert.False(t, actionOf(t, "PUT", compute, "{}").Confined, "a write is never confined")
}

func TestOrdinaryIsExactlyTheTable(t *testing.T) {
	assert.ElementsMatch(t, []ordinaryRow{
		{"microsoft.compute", "virtualmachines", "start"},
		{"microsoft.compute", "virtualmachines", "restart"},
		{"microsoft.resources", "tags", "write"},
	}, ordinary)
	for _, c := range []struct{ method, target string }{
		{"POST", compute + "/start"},
		{"POST", compute + "/restart"},
		{"PUT", rg + "/providers/Microsoft.Resources/tags/default"},
		{"PATCH", compute + "/providers/Microsoft.Resources/tags/default"},
		{"PATCH", "/subscriptions/" + sub + "/providers/Microsoft.Resources/tags/default"},
	} {
		act := actionOf(t, c.method, c.target, "{}")
		assert.Equal(t, permissions.Write, act.AccessLevel, c.target)
		assert.False(t, act.Once, c.method+" "+c.target)
	}
}

func TestAChangeOffTheOrdinaryTableAsksOnce(t *testing.T) {
	for _, c := range []struct{ method, target string }{
		{"PUT", rg + "/providers/Microsoft.Resources/deployments/d1"},
		{"POST", compute + "/deallocate"},
		{"POST", compute + "/powerOff"},
		{"PATCH", rg + "/providers/Microsoft.Compute/virtualMachineScaleSets/ss"},
		{"POST", rg + "/providers/Microsoft.Compute/virtualMachineScaleSets/ss/scale"},
		{"PUT", compute},
		{"DELETE", compute},
		{"DELETE", rg + "/providers/Microsoft.Resources/tags/default"},
		{"PUT", rg + "/tags"},
	} {
		act := actionOf(t, c.method, c.target, "{}")
		act.Principal = permissions.Chat
		assert.True(t, act.Once, c.method+" "+c.target)
		v, why := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{permissions.GrantRule(act)}}.Authorize(act)
		assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, c.target)
		assert.Equal(t, "it always asks", why)
	}
}

// An operation no table names asks each time under every mode and rule, no
// answer to it writes a rule, and read-only and a deny still stop it.
func TestAnUnnamedOperationAsksOnceAndIsNotGranted(t *testing.T) {
	act := actionOf(t, "POST", rg+"/providers/Microsoft.Frob/widgets/w/frobnicate", "{}")
	act.Principal = permissions.Chat
	require.True(t, act.Once)
	for _, mode := range []permissions.Mode{permissions.ModeAsk, permissions.ModeAuto} {
		for _, rules := range [][]permissions.Rule{nil, {permissions.GrantRule(act)}, {permissions.CommandRule(act)}} {
			v, _ := permissions.Policy{Mode: mode, Rules: rules}.Authorize(act)
			assert.Equal(t, permissions.Verdict{Effect: permissions.Ask}, v, mode)
		}
	}
	v, _ := permissions.Policy{Mode: permissions.ModeReadOnly}.Authorize(act)
	assert.Equal(t, permissions.Deny, v.Effect)
}

// Each request refused today for answering a credential or reaching a
// machine is the reach level, which no mode or rule allows; any other
// refusal has no level.
func TestEachCredentialRefusalIsReach(t *testing.T) {
	for _, c := range []struct{ method, target, refusal string }{
		{"POST", aks + "/runCommand", refusedRunCommand},
		{"GET", aks + "/commandResults/c1", refusedRunCommand},
		{"GET", aks + "/accessProfiles/clusterAdmin", refusedCredential},
		{"POST", rg + "/providers/Microsoft.Storage/storageAccounts/sa/listKeys", refusedCredential},
		{"POST", site + "/publishxml", refusedCredential},
		{"GET", rg + "/providers/Microsoft.DocumentDB/databaseAccounts/db/readonlykeys", refusedCredential},
	} {
		a, ok := parsePath(c.method, c.target)
		require.True(t, ok)
		v := classify(c.method, a, nil)
		assert.Equal(t, c.refusal, v.refusal, c.target)
		assert.Equal(t, permissions.Reach, v.level, c.target)
		allow := permissions.Rule{Effect: permissions.Allow, AccessLevel: permissions.Reach, Principal: "*", Target: "*", Scope: "*", Service: "*", Operation: "*", Resource: "*"}
		act := action(c.method, a, v, nil, nil, Subscription{ID: sub})
		act.Principal = permissions.Chat
		got, _ := permissions.Policy{Mode: permissions.ModeAuto, Rules: []permissions.Rule{allow}}.Authorize(act)
		assert.Equal(t, permissions.Deny, got.Effect, c.target)
	}
	a, _ := parsePath("POST", rg+"/providers/Microsoft.Network/networkWatchers/nw/getTopology")
	v := classify("POST", a, nil)
	assert.Equal(t, refusedUnnamed, v.refusal)
	assert.Empty(t, v.level, "an unnamed list or get has no level")
}
