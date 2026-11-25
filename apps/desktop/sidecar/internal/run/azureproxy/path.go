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
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

// armPath is a path as Resource Manager reads it:
//
//	/subscriptions
//	/tenants
//	/subscriptions/{id}[/resourceGroups/{g}]{provider}…[/{last}]
//	{provider}…[/{last}]
//
//	{provider} = /providers/{namespace}[/{type}/{name}]…
//
// Resource Manager folds case, so every fixed word is read in any case. An
// extension resource nests a scope: providers in a type's place starts the
// next one.
type armPath struct {
	subscription string
	group        string
	scopes       []scope
	last         string // a collection's type or an action; one segment
}

// scope is one /providers/ segment: a namespace and its type/name pairs.
type scope struct {
	namespace string
	pairs     []pair
}

type pair struct{ typ, name string }

// methods are the methods Resource Manager serves.
var methods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete,
}

// parsePath reads p, refusing a method Resource Manager does not serve and a
// path that is not canonical or that the grammar does not admit. The two
// polling paths outside it are admitted for a GET alone.
func parsePath(method, p string) (armPath, bool) {
	if !slices.Contains(methods, method) || !strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "/" {
		return armPath{}, false
	}
	rest := strings.Split(p[1:], "/")
	if len(rest) == 1 && (is(rest[0], "subscriptions") || is(rest[0], "tenants")) {
		return armPath{last: rest[0]}, true
	}
	var a armPath
	if is(rest[0], "subscriptions") {
		a.subscription, rest = rest[1], rest[2:]
		if len(rest) >= 2 && is(rest[0], "resourceGroups") {
			a.group, rest = rest[1], rest[2:]
		}
		if len(rest) == 2 && is(rest[0], "operationResults") {
			return a, method == http.MethodGet
		}
	}
	for len(rest) >= 2 && is(rest[0], "providers") {
		s := scope{namespace: rest[1]}
		rest = rest[2:]
		for len(rest) >= 2 && !is(rest[0], "providers") {
			s.pairs = append(s.pairs, pair{rest[0], rest[1]})
			rest = rest[2:]
		}
		a.scopes = append(a.scopes, s)
	}
	if len(rest) == 1 {
		a.last, rest = rest[0], nil
	}
	if len(rest) > 0 || a.subscription == "" && len(a.scopes) == 0 || !a.ascii() {
		return armPath{}, false
	}
	return a, true
}

// ascii is whether every word the classifier reads, each namespace, type and
// the last segment, is ASCII. Resource Manager folds case as .NET does, where
// ſ is s, so a word outside ASCII could reach an action the classifier never
// names. A resource's own name may be any text.
func (a armPath) ascii() bool {
	words := []string{a.last}
	for _, s := range a.scopes {
		words = append(words, s.namespace)
		for _, p := range s.pairs {
			words = append(words, p.typ)
		}
	}
	return !slices.ContainsFunc(words, func(w string) bool { return !isASCII(w) })
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// is reports whether a segment is the fixed word w, in ASCII's case alone.
func is(seg, w string) bool { return isASCII(seg) && strings.EqualFold(seg, w) }

// namespace is the namespace of the path's last scope, which names what the
// request reaches.
func (a armPath) namespace() string {
	if len(a.scopes) == 0 {
		return ""
	}
	return a.scopes[len(a.scopes)-1].namespace
}

// pairs is the last scope's type/name pairs.
func (a armPath) pairs() []pair {
	if len(a.scopes) == 0 {
		return nil
	}
	return a.scopes[len(a.scopes)-1].pairs
}

// resource is the last scope as namespace/type/name…, or "" for a path with
// no scope.
func (a armPath) resource() string {
	if len(a.scopes) == 0 {
		return ""
	}
	parts := []string{a.namespace()}
	for _, p := range a.pairs() {
		parts = append(parts, p.typ, p.name)
	}
	return strings.Join(parts, "/")
}

// isGroup is whether the path names a resource group itself.
func (a armPath) isGroup() bool {
	return a.group != "" && len(a.scopes) == 0 && a.last == ""
}

// final is the path's last segment.
func (a armPath) final() string {
	switch ps := a.pairs(); {
	case a.last != "":
		return a.last
	case len(ps) > 0:
		return ps[len(ps)-1].name
	case len(a.scopes) > 0:
		return a.namespace()
	case a.group != "":
		return a.group
	}
	return a.subscription
}
