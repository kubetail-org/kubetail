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
	"bytes"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// What the classifier refuses unasked.
const (
	refusedRunCommand = "kubetail: AKS run-command runs commands in the cluster around its proxy. Use kubectl."
	refusedCredential = "kubetail: this Azure action answers a credential, which the sandbox never reaches."
	refusedUnnamed    = "kubetail: this Azure list or get action is not known to answer no secret, so it is refused."
)

// verdict is one request's reading: its access level, and a refusal the
// proxy answers itself. A refusal of a credential or of reaching a machine is
// the reach level; any other refusal has none.
type verdict struct {
	refusal string
	level   permissions.AccessLevel
	// reads is what a data read answers, in the user's words.
	reads string
}

// credentialWords are the words in an action's name that say its answer is
// a credential.
var credentialWords = []string{"key", "token", "sas", "credential", "password", "connectionstring"}

// credentialActions are the actions, by namespace, whose answer is a
// credential whatever their method: a GET is never read for credential words,
// and a name may say nothing of what it answers.
var credentialActions = map[string][]string{
	"microsoft.documentdb": {"readonlykeys"}, // a GET answering read-only master keys
	"microsoft.web":        {"publishxml"},   // a publishing profile: deployment users and passwords
}

// classify reads a request that passed the path policy. The first row that
// matches decides; a row naming a namespace reads the path's last scope.
func classify(method string, a armPath, query url.Values) verdict {
	ns, types := strings.ToLower(a.namespace()), a.types()
	final := strings.ToLower(a.final())
	aksCluster := ns == "microsoft.containerservice" && len(types) > 0 && types[0] == "managedclusters"
	switch {
	case aksCluster && (final == "runcommand" || slices.Contains(types, "commandresults")):
		return verdict{refusal: refusedRunCommand, level: permissions.Reach}
	case aksCluster && slices.Contains(types, "accessprofiles"),
		slices.Contains(credentialActions[ns], final):
		return verdict{refusal: refusedCredential, level: permissions.Reach}
	}
	if reads := secretMetadata(method, a, ns, types, query); reads != "" {
		return verdict{level: permissions.DataRead, reads: reads}
	}
	if method == http.MethodPost {
		for _, w := range credentialWords {
			if strings.Contains(final, w) {
				return verdict{refusal: refusedCredential, level: permissions.Reach}
			}
		}
		if strings.HasPrefix(final, "list") || strings.HasPrefix(final, "get") {
			return verdict{refusal: refusedUnnamed}
		}
	}
	switch {
	case method == http.MethodGet || method == http.MethodHead:
		return verdict{level: permissions.Read}
	case destructive(method, a, ns, types):
		return verdict{level: permissions.Destructive}
	case ns == "microsoft.authorization":
		return verdict{level: permissions.AccessControl}
	}
	return verdict{level: permissions.Write}
}

// ordinaryRow is one change a mode or a rule may allow: its service,
// resource and operation as action reads them.
type ordinaryRow struct{ service, resource, operation string }

// ordinary are the changes a mode or a rule may allow. Any other write asks
// each time. Adding a row is a security change.
var ordinary = []ordinaryRow{
	{"microsoft.compute", "virtualmachines", "start"},
	{"microsoft.compute", "virtualmachines", "restart"},
	{"microsoft.resources", "tags", "write"}, // a PUT or PATCH
}

// action is the request as the engine decides it, read from its path, its
// query and its body. The scope is the path's own subscription and group, in
// lower case; "" when the path names no subscription, whatever default the
// proxy forwards with. A query or body naming another subscription, or a body
// that is not JSON, asks each time.
func action(method string, a armPath, v verdict, query url.Values, body []byte, sub Subscription) permissions.Action {
	act := permissions.Action{
		Target: permissions.Azure, AccountName: strings.ToLower(a.subscription),
		Service: strings.ToLower(a.namespace()), Operation: operation(method, a), Resource: resourceType(method, a),
		AccessLevel: v.level, Summary: summary(method, a, v, sub),
	}
	if ps := a.pairs(); len(ps) > 0 {
		act.Name = ps[len(ps)-1].name
	}
	if a.subscription != "" {
		act.Scope = act.AccountName
		if a.group != "" {
			act.Scope += "/" + strings.ToLower(a.group)
		}
		// Reaching past the subscription keeps its scope, so its mode and rules
		// still refuse, and asks each time.
		act.Once = namesAnotherSubscription(act.AccountName, query, body)
		// A data read in no group lists every group's, but for a deployment's,
		// which a subscription holds of its own.
		act.Spans = v.level == permissions.DataRead && a.group == "" && act.Service != "microsoft.resources"
	}
	types := a.types()
	act.Confined = v.level == permissions.AccessControl && a.group != "" && len(types) > 0 && types[0] == "roleassignments"
	if v.level == permissions.Write && !slices.Contains(ordinary, ordinaryRow{act.Service, act.Resource, act.Operation}) {
		act.Once = true
	}
	return act
}

// operation is what a request does, as a rule names it: write for a PUT or
// PATCH, delete, read for a GET or HEAD, and a POST's action by name.
func operation(method string, a armPath) string {
	switch method {
	case http.MethodPut, http.MethodPatch:
		return "write"
	case http.MethodDelete:
		return "delete"
	case http.MethodPost:
		return strings.ToLower(a.last)
	}
	return "read"
}

// resourceType is the last scope's type path in lower case, a collection's
// type included and a POST's action left out.
func resourceType(method string, a armPath) string {
	ts := a.types()
	if a.last != "" && len(a.scopes) > 0 && method == http.MethodPost {
		ts = ts[:len(ts)-1]
	}
	return strings.Join(ts, "/")
}

// namesAnotherSubscription is whether a query value, or a key or string of
// body read token by token, names /subscriptions/ of one other than own, or
// body is not one JSON value. An empty body names nothing.
func namesAnotherSubscription(own string, query url.Values, body []byte) bool {
	for _, values := range query {
		if slices.ContainsFunc(values, func(v string) bool { return mentionsOther(own, v) }) {
			return true
		}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return false
	}
	return runproxy.AnyJSONString(body, func(s string) bool { return mentionsOther(own, s) })
}

// mentionsOther is whether s names /subscriptions/ followed by an id that is
// not own, in any case.
func mentionsOther(own, s string) bool {
	const marker = "/subscriptions/"
	s = strings.ToLower(s)
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			return false
		}
		s = s[i+len(marker):]
		id := s[:strings.IndexFunc(s+"/", func(r rune) bool {
			return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r == '-')
		})]
		if id != "" && id != own {
			return true
		}
	}
}

// types is the last scope's types, then its trailing segment, lower-cased.
func (a armPath) types() []string {
	var ts []string
	for _, p := range a.pairs() {
		ts = append(ts, strings.ToLower(p.typ))
	}
	if a.last != "" && len(a.scopes) > 0 {
		ts = append(ts, strings.ToLower(a.last))
	}
	return ts
}

// secretNamespaces are the namespaces whose resources the secret-metadata
// list names. An operation's result there can be the finished resource.
var secretNamespaces = []string{
	"microsoft.apimanagement", "microsoft.app", "microsoft.automation", "microsoft.containerinstance",
	"microsoft.resources", "microsoft.web",
}

// secretMetadata is what a read on the list of metadata Azure documents as
// able to hold a secret answers, or "".
func secretMetadata(method string, a armPath, ns string, types []string, query url.Values) string {
	get := method == http.MethodGet
	switch {
	case get && slices.Contains(secretNamespaces, ns) && len(types) > 0 && strings.HasSuffix(types[len(types)-1], "operationresults"):
		return "an operation's result"
	case ns == "microsoft.web" && method == http.MethodPost && webConfigList(a, types, "appsettings"):
		return "app settings"
	case ns == "microsoft.web" && method == http.MethodPost && webConfigList(a, types, "connectionstrings"):
		return "connection strings"
	case ns == "microsoft.compute" && get && expands(query, "userdata"):
		return "user data"
	case ns == "microsoft.resources" && get && slices.Equal(types, []string{"deployments"}):
		return "a deployment"
	case ns == "microsoft.resources" && method == http.MethodPost && slices.Equal(types, []string{"deployments", "exporttemplate"}):
		return "a deployment's template"
	case ns == "microsoft.automation" && get && slices.Equal(types, []string{"automationaccounts", "variables"}):
		return "Automation variables"
	case ns == "microsoft.apimanagement" && get && len(types) > 1 && types[0] == "service" && types[len(types)-1] == "subscriptions":
		return "API Management subscriptions"
	case ns == "microsoft.web" && get && slices.Contains(types, "hybridconnectionnamespaces"):
		return "a hybrid connection"
	case ns == "microsoft.app" && get && containerTemplate(types),
		ns == "microsoft.containerinstance" && get && slices.Equal(types, []string{"containergroups"}):
		return "a container's environment"
	}
	return ""
}

// webConfigList is whether a path is an app's, or a slot's,
// config/{setting}/list.
func webConfigList(a armPath, types []string, setting string) bool {
	if !slices.Equal(types, []string{"sites", "config", "list"}) && !slices.Equal(types, []string{"sites", "slots", "config", "list"}) {
		return false
	}
	ps := a.pairs()
	return is(ps[len(ps)-1].name, setting)
}

// containerTemplate is whether a Container Apps path answers a container
// template, whose environment can hold a secret: an app or its revisions, a
// job or its executions.
func containerTemplate(types []string) bool {
	for _, t := range [][]string{
		{"containerapps"}, {"containerapps", "revisions"}, {"jobs"}, {"jobs", "executions"},
	} {
		if slices.Equal(types, t) {
			return true
		}
	}
	return false
}

// expands is whether query's $expand, its name in any case as Resource
// Manager reads it, names what, in any case.
func expands(query url.Values, what string) bool {
	for key, values := range query {
		if !strings.EqualFold(key, "$expand") {
			continue
		}
		for _, v := range values {
			for item := range strings.SplitSeq(v, ",") {
				if strings.EqualFold(strings.TrimSpace(item), what) {
					return true
				}
			}
		}
	}
	return false
}

// destructive is whether a change is on the destructive list.
func destructive(method string, a armPath, ns string, types []string) bool {
	if ns == "microsoft.authorization" {
		// A role assignment, a role or a lock: who may do what, which deleting
		// changes without losing data, at any scope.
		return false
	}
	if method == http.MethodDelete {
		switch {
		case a.group == "", a.isGroup():
			return true
		case ns == "microsoft.containerservice" && slices.Equal(types, []string{"managedclusters"}) && a.last == "":
			return true
		}
	}
	return slices.Contains(types, "runcommand") || slices.Contains(types, "runcommands")
}
