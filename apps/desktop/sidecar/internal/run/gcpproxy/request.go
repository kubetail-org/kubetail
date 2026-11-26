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

package gcpproxy

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// Why a request is refused before it is classified.
const (
	refusedPath           = "kubetail: this path is not one the sandbox reaches Google Cloud on."
	refusedQuery          = "kubetail: this request's query does not parse, so it was not sent."
	refusedQueryKey       = "kubetail: a credential in the query is not sent to Google Cloud."
	refusedMethodOverride = "kubetail: a method override in the query is not sent to Google Cloud; send the method itself."
	refusedAlt            = "kubetail: this service's answers are read as JSON alone, so it was not sent."
	refusedBilledTwice    = "kubetail: this request names more than one project to bill, so it was not sent."
)

// checkPath is escaped, a request's path as sent, read as svc reads it: each
// segment decoded. It answers why the path is refused, or "": off svc's
// prefixes, an empty, "." or ".." segment, a bad escape, or an encoded "/" or
// "." anywhere but in a storage object's name, which the JSON API takes
// encoded. So a classifier reading the segments reads what Google routes on.
func checkPath(svc service, escaped string) (segs []string, why string) {
	if !slices.ContainsFunc(svc.paths, func(p string) bool { return strings.HasPrefix(escaped, p) }) {
		return nil, refusedPath
	}
	raw := strings.Split(escaped[1:], "/")
	segs = make([]string, len(raw))
	for i, r := range raw {
		seg, err := url.PathUnescape(r)
		if err != nil || seg == "" || seg == "." || seg == ".." {
			return nil, refusedPath
		}
		objectName := svc.name == "storage" && i >= 3 && raw[i-1] == "o" && raw[i-3] == "b"
		if !objectName && (strings.Contains(seg, "/") || encodesADot(r)) {
			return nil, refusedPath
		}
		segs[i] = seg
	}
	return segs, ""
}

// encodesADot is whether raw spells a "." as %2E.
func encodesADot(raw string) bool {
	return strings.Contains(strings.ToLower(raw), "%2e")
}

// checkRequest reads what the classifier does not: the query, which must
// parse and carry no credential and no method override, and for a rewritten
// service an alt other than json; and the project it bills, named once at
// most. A method override would run what the classifier read as one method
// as another, and a second project billed would not be the one classified.
func checkRequest(svc service, r *http.Request) (url.Values, string) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, refusedQuery
	}
	if len(r.Header.Values("X-Goog-User-Project")) > 1 || len(systemValues(q, "userproject")) > 1 {
		return nil, refusedBilledTwice
	}
	for name := range q {
		switch systemParameter(name) {
		case "access_token", "oauth_token", "key":
			return nil, refusedQueryKey
		case "httpmethod":
			return nil, refusedMethodOverride
		}
	}
	if svc.rewrites() {
		for _, alt := range systemValues(q, "alt") {
			if !strings.EqualFold(alt, "json") {
				return nil, refusedAlt
			}
		}
	}
	return q, ""
}

// systemParameter is a query parameter's name as Google reads a system
// parameter's, which it also takes with a leading $; case is ignored, so no
// spelling passes.
func systemParameter(name string) string {
	return strings.ToLower(strings.TrimPrefix(name, "$"))
}

// systemValues is every value q holds for the system parameter name, under
// any spelling systemParameter reads as it.
func systemValues(q url.Values, name string) []string {
	var values []string
	for key, v := range q {
		if systemParameter(key) == name {
			values = append(values, v...)
		}
	}
	return values
}

// hasSystemValue is whether q holds value, in any case, for any of the
// system parameters names.
func hasSystemValue(q url.Values, value string, names ...string) bool {
	for _, name := range names {
		if slices.ContainsFunc(systemValues(q, name), func(v string) bool { return strings.EqualFold(v, value) }) {
			return true
		}
	}
	return false
}
