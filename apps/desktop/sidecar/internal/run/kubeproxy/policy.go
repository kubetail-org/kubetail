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

package kubeproxy

import (
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// refusal is why the policy refuses a request, in the words the client prints;
// pass lets it through.
type refusal string

const (
	pass          refusal = ""
	refusedReach  refusal = "kubetail: the sandbox cannot exec, attach, port-forward, proxy or impersonate. Run the command outside the sandbox."
	refusedToken  refusal = "kubetail: the sandbox cannot request a service account token. Run the command outside the sandbox."
	refusedMethod refusal = "kubetail: the sandbox does not send this method. Run the command outside the sandbox."
	refusedPath   refusal = "kubetail: the sandbox does not read this path. Run the command outside the sandbox."

	refusedDenied      refusal = "kubetail: the user did not approve this change."
	refusedUnanswered  refusal = "kubetail: the user did not answer this change."
	refusedUnrecorded  refusal = "kubetail: this change could not be recorded, so it was not sent."
	refusedUnshowable  refusal = "kubetail: a change must be sent as JSON or YAML text to be shown. Run the command outside the sandbox."
	refusedTooLarge    refusal = "kubetail: this change is too large to show. Run the command outside the sandbox."
	refusedRedacted    refusal = "kubetail: this change carries a value the sandbox read redacted. Run the command outside the sandbox."
	refusedHelm        refusal = "kubetail: helm rebuilds a release from Secret data this command read redacted. Allow Secret data for this namespace, then run it again."
	refusedRepeatedKey refusal = "kubetail: this change repeats a key in one object, so it cannot be shown as it would be read. Run the command outside the sandbox."
	refusedQuery       refusal = "kubetail: this change's query does not parse, so it cannot be shown as it would be sent."
)

// classifyRefusal is the access level of what a refusal refuses, false for a
// refusal of anything else. Exec, a tunnel, impersonation and a token reach
// past the API server: the boundary never allows the reach level, and the
// handler answers each before any queue.
func classifyRefusal(why refusal) (permissions.AccessLevel, bool) {
	switch why {
	case refusedReach, refusedToken:
		return permissions.Reach, true
	}
	return "", false
}

// reachSubresources are the subresources that reach past the API server: into
// a container, or through it to a pod, a service or a node.
var reachSubresources = map[string]bool{"exec": true, "attach": true, "portforward": true, "proxy": true}

// nonResourceReads are the paths with neither prefix a GET may read.
var nonResourceReads = map[string]bool{
	"/api": true, "/apis": true, "/version": true, "/livez": true, "/readyz": true, "/healthz": true,
}

// decide is the policy: what r's method, headers and parsed path allow, and
// the path as it parsed it. A write of a resource passes, for the handler to
// put to the user (isWrite). It reads what the API server would, never a
// segment anywhere in the path.
func decide(r *http.Request) (apiPath, refusal) {
	if !canonical(r.URL) {
		return apiPath{}, refusedPath
	}
	p := parsePath(r.URL.Path)
	if p.proxy || reachSubresources[p.subresource] || reaches(r.Header) {
		return p, refusedReach
	}
	// A token's answer is a credential, and the model reads what a command prints.
	if p.kind == resourcePath && p.group == "" && p.resource == "serviceaccounts" && p.subresource == "token" {
		return p, refusedToken
	}
	switch p.kind {
	case resourcePath:
		if r.Method == http.MethodGet || writeMethods[r.Method] {
			return p, pass
		}
		return p, refusedMethod
	case discoveryPath:
		if r.Method == http.MethodGet {
			return p, pass
		}
	default:
		if r.Method == http.MethodGet && (nonResourceReads[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/openapi/")) {
			return p, pass
		}
	}
	return p, refusedPath
}

// canonical is whether u's path reads the same decoded, raw and resolved: no
// escape that decoding changes, and no ., .. or empty segment and no trailing
// slash. The policy reads the decoded path and the proxy forwards the raw one,
// and a server in front of the cluster may resolve a .., so any of them could
// let configmaps/../secrets/x pass as a ConfigMap and be served as a Secret.
func canonical(u *url.URL) bool {
	return u.RawPath == "" && path.Clean(u.Path) == u.Path
}

// selfReviews are the reviews that ask what the caller may do, or who it is:
// kubectl auth can-i and whoami, keyed by group then resource.
var selfReviews = map[string]map[string]bool{
	"authorization.k8s.io":  {"selfsubjectaccessreviews": true, "selfsubjectrulesreviews": true},
	"authentication.k8s.io": {"selfsubjectreviews": true},
}

// writeMethods are the methods that change a resource.
var writeMethods = map[string]bool{
	http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// isWrite is whether a request the policy passed changes the cluster, so the
// user is asked first. A self review is a POST that changes nothing.
func isWrite(r *http.Request, p apiPath) bool {
	return writeMethods[r.Method] && !(r.Method == http.MethodPost && isSelfReview(p))
}

// isSelfReview is whether p is a self review created as the API server takes
// one: cluster-scoped, with no name.
func isSelfReview(p apiPath) bool {
	return selfReviews[p.group][p.resource] && p.namespace == "" && p.name == ""
}

// reaches is whether h asks for a stream past HTTP, or to act as someone else.
func reaches(h http.Header) bool {
	if runproxy.Upgrades(h) {
		return true
	}
	for name := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(name), "Impersonate-") {
			return true
		}
	}
	return false
}

// pathKind is what a request's path names.
type pathKind int

const (
	nonResourcePath pathKind = iota // neither /api/{version} nor /apis/{group}
	discoveryPath                   // a group's or a group version's discovery
	resourcePath
)

// apiPath is a request's path as the API server reads it, after
// RequestInfoFactory: the group ("" for core), the version, and for a resource
// its namespace, name and subresource. proxy is a legacy /proxy/ after the
// version, and watch a legacy /watch/.
type apiPath struct {
	kind                                   pathKind
	proxy, watch                           bool
	group, version                         string
	namespace, resource, name, subresource string
}

// parsePath reads path as the API server does.
func parsePath(path string) apiPath {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var p apiPath
	switch {
	case len(parts) >= 2 && parts[0] == "api":
		p.version, parts = parts[1], parts[2:]
	case len(parts) == 2 && parts[0] == "apis":
		return apiPath{kind: discoveryPath, group: parts[1]}
	case len(parts) >= 3 && parts[0] == "apis":
		p.group, p.version, parts = parts[1], parts[2], parts[3:]
	default:
		return apiPath{kind: nonResourcePath}
	}
	if len(parts) > 0 && (parts[0] == "watch" || parts[0] == "proxy") {
		p.proxy, p.watch, parts = parts[0] == "proxy", parts[0] == "watch", parts[1:]
	}
	if len(parts) == 0 {
		p.kind = discoveryPath
		return p
	}
	p.kind = resourcePath
	if len(parts) >= 2 && parts[0] == "namespaces" {
		// A namespace's own subresources follow its name.
		if len(parts) == 2 || parts[2] == "status" || parts[2] == "finalize" {
			p.resource, p.name = parts[0], parts[1]
			if len(parts) > 2 {
				p.subresource = parts[2]
			}
			return p
		}
		p.namespace, parts = parts[1], parts[2:]
	}
	p.resource = parts[0]
	if len(parts) >= 2 {
		p.name = parts[1]
	}
	if len(parts) >= 3 {
		p.subresource = parts[2]
	}
	return p
}

// onSecrets is whether p is a request on core Secrets, whose answers the
// proxy redacts.
func (p apiPath) onSecrets() bool {
	return p.kind == resourcePath && p.group == "" && p.resource == "secrets"
}
