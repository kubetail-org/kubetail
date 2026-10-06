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

package k8shelpers

import (
	"context"
	"slices"

	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

// Use this ptr to bypass namespace checks
var BypassNamespaceCheck = ptr.To("")

// ResolveAllowedNamespaces returns the allowed-namespaces list effective for
// this request: the static (server-wide) list, optionally narrowed by the
// session-level override carried in ctx under K8SSessionNamespacesCtxKey.
func ResolveAllowedNamespaces(ctx context.Context, static []string) []string {
	override, _ := ctx.Value(K8SSessionNamespacesCtxKey).([]string)
	return NarrowAllowedNamespaces(static, override)
}

// NarrowAllowedNamespaces narrows a static (server-wide) allowed-namespaces
// list by a session-level override. The override can only ever shrink the
// static list, never widen it:
//   - no override: the static list applies unchanged
//   - override present, static list empty (unrestricted): the override
//     becomes the effective list
//   - override present, static list set: the intersection of both applies;
//     if they share nothing (e.g. the static config was tightened after the
//     session logged in), the static list applies unchanged - returning the
//     empty intersection would mean "unrestricted" under this package's
//     conventions and turn a narrowing into a widening
func NarrowAllowedNamespaces(static, override []string) []string {
	if len(override) == 0 {
		return static
	}
	if len(static) == 0 {
		return override
	}
	var out []string
	for _, ns := range override {
		if slices.Contains(static, ns) {
			out = append(out, ns)
		}
	}
	if len(out) == 0 {
		return static
	}
	return out
}

// Dereference `namespace` argument and check that it is allowed
func DerefNamespace(allowedNamespaces []string, namespace *string, defaultNamespace string) (string, error) {
	ns := ptr.Deref(namespace, defaultNamespace)

	// bypass auth
	if namespace == BypassNamespaceCheck {
		return ns, nil
	}

	// perform auth
	if len(allowedNamespaces) > 0 && !slices.Contains(allowedNamespaces, ns) {
		return "", errors.ErrForbidden
	}

	return ns, nil
}

// Dereference `namespace` argument, check if it's allowed, if equal to "" then return allowedNamespaes
func DerefNamespaceToList(allowedNamespaces []string, namespace *string, defaultNamespace string) ([]string, error) {
	ns := ptr.Deref(namespace, defaultNamespace)

	// bypass auth
	if namespace == BypassNamespaceCheck {
		return []string{""}, nil
	}

	// perform auth
	if ns != "" && len(allowedNamespaces) > 0 && !slices.Contains(allowedNamespaces, ns) {
		return nil, errors.ErrForbidden
	}

	// listify
	if ns == "" && len(allowedNamespaces) > 0 {
		return allowedNamespaces, nil
	}

	return []string{ns}, nil
}
