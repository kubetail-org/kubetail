// Copyright 2024-2025 Andres Morey
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
	"slices"

	"k8s.io/utils/ptr"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

// Dereference `namespace` argument and check that it is allowed
func DerefNamespace(allowedNamespaces []string, namespace *string, defaultNamespace string) (string, error) {
	ns := ptr.Deref(namespace, defaultNamespace)

	// perform auth
	if len(allowedNamespaces) > 0 && !slices.Contains(allowedNamespaces, ns) {
		return "", errors.ErrForbidden
	}

	return ns, nil
}

// Dereference `namespace` argument, check if it's allowed, if equal to "" then return allowedNamespaes
func DerefNamespaceToList(allowedNamespaces []string, namespace *string, defaultNamespace string) ([]string, error) {
	ns := ptr.Deref(namespace, defaultNamespace)

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
