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

package webfetch

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// maxHops is how many redirects a fetch follows.
const maxHops = 5

// follow requests approved and follows its redirects on the approved host,
// answering the response that is not one, or a refusal naming the redirect.
// The client follows none itself, so the count and the approved host are here.
func (t *Tool) follow(ctx context.Context, approved *url.URL) (*http.Response, error) {
	u := approved
	for hops := 0; ; hops++ {
		resp, err := t.get(ctx, u)
		if err != nil {
			return nil, err
		}
		if !isRedirect(resp.StatusCode) {
			return resp, nil
		}
		resp.Body.Close()
		next, err := hop(approved, resp)
		if err != nil {
			return nil, err
		}
		if hops == maxHops {
			return nil, refusal("The page redirected more than 5 times. WebFetch stopped at " + capURL(u.String()) + ".")
		}
		u = next
	}
}

// isRedirect is one of the five codes net/http would follow. Any other 3xx is
// answered as a status.
func isRedirect(code int) bool {
	switch code {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// hop is where a redirect leads, when the fetch may follow it unasked.
func hop(approved *url.URL, resp *http.Response) (*url.URL, error) {
	loc := resp.Header.Get("Location")
	next, err := resp.Request.URL.Parse(loc)
	if loc == "" || err != nil {
		return nil, refusal("The server answered " + status(resp.StatusCode) + " with nowhere to go.")
	}
	if next.Scheme != "https" {
		// Not "call again": the upgrade would land on the same redirect.
		return nil, refusal("The page redirects to " + capURL(next.String()) + ", which is not https. WebFetch does not follow it.")
	}
	checked, err := target(next.String())
	if err != nil {
		return nil, refusal("The page redirects to " + capURL(next.String()) + ". " + err.Error())
	}
	if !sameSite(approved, checked) {
		return nil, refusal("REDIRECT DETECTED: The URL redirects to a different host.\n\n" +
			"Original URL: " + capURL(approved.String()) + "\n" +
			"Redirect URL: " + capURL(next.String()) + "\n" +
			"Status: " + status(resp.StatusCode) + "\n\n" +
			"To read it, call WebFetch again with that URL.")
	}
	return checked, nil
}

// sameSite is whether b is on a's port, a missing one read as 443, and on its
// host, or its host with or without one leading www.
func sameSite(a, b *url.URL) bool {
	return portOf(a) == portOf(b) && hostOf(a) == hostOf(b)
}

func portOf(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	return "443"
}

func hostOf(u *url.URL) string {
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}
