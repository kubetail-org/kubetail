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

package githubproxy

import (
	"net/http"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/runproxy"
)

// unshownPack is what a push's request draws where its body would be. The
// request names the repository and every ref and option, and a commit id is
// a hash of its whole content, so the new id says which commit a ref will
// name; the user has the commits in the workspace.
const unshownPack = "a Git pack of the commits pushed; an update may rewrite history"

// apiRequest is an API request that is decided, as the user is shown it:
// its method and path, and body, the bytes sent.
func apiRequest(r *http.Request, host string, act permissions.Action, body []byte) runproxy.Request {
	act.Summary = "GitHub: " + r.Method + " " + r.URL.Path
	return runproxy.Request{Action: act, Sent: sentOf(r, host, body, headerDetails(r.Header))}
}

// graphQLRequest is a GraphQL mutation as the user is shown it: each
// mutation's field and the operation named, then the document as sent.
func graphQLRequest(r *http.Request, host string, g graphQL, body []byte) runproxy.Request {
	var details []runproxy.Detail
	for _, m := range g.mutations {
		details = append(details, runproxy.Detail{Label: "Mutation", Value: m})
	}
	if g.operation != "" {
		details = append(details, runproxy.Detail{Label: "Operation", Value: g.operation})
	}
	details = append(details, headerDetails(r.Header)...)
	if v := r.Header.Get("GraphQL-Features"); v != "" {
		details = append(details, runproxy.Detail{Label: "Features", Value: v})
	}
	act := g.action()
	act.Summary = "GitHub: GraphQL " + strings.Join(g.mutations, ", ")
	return runproxy.Request{Action: act, Sent: sentOf(r, host, body, details)}
}

// pushRequest is a push as the user is shown it: the repository and each
// ref and option, its pack unshown.
func pushRequest(r *http.Request, host, repo string, p push) runproxy.Request {
	act := gitAction(repo, p)
	act.Summary = "GitHub: push to " + repo
	sent := sentOf(r, host, nil, p.details(repo))
	sent.Unshown = unshownPack
	return runproxy.Request{Action: act, Sent: sent}
}

func sentOf(r *http.Request, host string, body []byte, details []runproxy.Detail) runproxy.Sent {
	return runproxy.Sent{
		Host: host, Method: r.Method, Path: r.URL.RequestURI(),
		ContentType: r.Header.Get("Content-Type"), Body: body, Details: details,
	}
}

// headerDetails are the forwarded headers that change what any API request
// does, each that was sent: Accept and the API version.
func headerDetails(h http.Header) []runproxy.Detail {
	var details []runproxy.Detail
	add := func(label, name string) {
		if v := h.Get(name); v != "" {
			details = append(details, runproxy.Detail{Label: label, Value: v})
		}
	}
	add("Accept", "Accept")
	add("API version", "X-GitHub-Api-Version")
	return details
}
