// Copyright 2024-2026 The Kubetail Authors
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
	"testing"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestDerefNamespaceSuccess(t *testing.T) {
	tests := []struct {
		name                 string
		setAllowedNamespaces []string
		setNamespace         *string
		wantNamespace        string
	}{
		{
			"any namespace allowed: <empty>",
			[]string{},
			nil,
			"my-default-namespace",
		},
		{
			"any namespace allowed: <all>",
			[]string{},
			ptr.To(""),
			"",
		},
		{
			"any namespace allowed: default",
			[]string{},
			ptr.To("default"),
			"default",
		},
		{
			"any namespace allowed: testns",
			[]string{},
			ptr.To("testns"),
			"testns",
		},
		{
			"single namespace allowed: testns",
			[]string{"testns"},
			ptr.To("testns"),
			"testns",
		},
		{
			"multiple namespaces allowed: <all>",
			[]string{"testns1", "testns2"},
			ptr.To("testns1"),
			"testns1",
		},
		{
			"BypassNamespaceCheck",
			[]string{"testns1", "testns2"},
			BypassNamespaceCheck,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNamespace, err := DerefNamespace(tt.setAllowedNamespaces, tt.setNamespace, "my-default-namespace")
			assert.Nil(t, err)
			assert.Equal(t, tt.wantNamespace, actualNamespace)
		})
	}
}

func TestDerefNamespaceError(t *testing.T) {
	tests := []struct {
		name                 string
		setAllowedNamespaces []string
		setNamespace         *string
	}{
		{
			"single namespace allowed: <empty>",
			[]string{"testns"},
			nil,
		},
		{
			"single namespace allowed: <all>",
			[]string{"testns"},
			ptr.To(""),
		},
		{
			"single namespace allowed: not-testns",
			[]string{"testns"},
			ptr.To("not-testns"),
		},
		{
			"multiple namespaces allowed: <empty>",
			[]string{"testns1", "testns2"},
			nil,
		},
		{
			"multiple namespaces allowed: <all>",
			[]string{"testns1", "testns2"},
			ptr.To(""),
		},
		{
			"multiple namespaces allowed: not-testns1",
			[]string{"testns1", "testns2"},
			ptr.To("not-testns1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ns, err := DerefNamespace(tt.setAllowedNamespaces, tt.setNamespace, "my-default-namespace")
			assert.Equal(t, ns, "")
			assert.NotNil(t, err)
			assert.Equal(t, err, errors.ErrForbidden)
		})
	}
}

func TestDerefNamespaceToListSuccess(t *testing.T) {
	tests := []struct {
		name                 string
		setAllowedNamespaces []string
		setNamespace         *string
		wantNamespaces       []string
	}{
		{
			"any namespace allowed: <empty>",
			[]string{},
			nil,
			[]string{"my-default-namespace"},
		},
		{
			"any namespace allowed: <all>",
			[]string{},
			ptr.To(""),
			[]string{""},
		},
		{
			"any namespace allowed: default",
			[]string{},
			ptr.To("default"),
			[]string{"default"},
		},
		{
			"any namespace allowed: testns",
			[]string{},
			ptr.To("testns"),
			[]string{"testns"},
		},
		{
			"single namespace allowed: <all>",
			[]string{"testns"},
			ptr.To(""),
			[]string{"testns"},
		},
		{
			"single namespace allowed: testns",
			[]string{"testns"},
			ptr.To("testns"),
			[]string{"testns"},
		},
		{
			"multiple namespaces allowed: <all>",
			[]string{"testns1", "testns2"},
			ptr.To(""),
			[]string{"testns1", "testns2"},
		},
		{
			"multiple namespaces allowed: testns1",
			[]string{"testns1", "testns2"},
			ptr.To("testns1"),
			[]string{"testns1"},
		},
		{
			"BypassNamespaceCheck",
			[]string{"testns1", "testns2"},
			BypassNamespaceCheck,
			[]string{""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNamespaces, err := DerefNamespaceToList(tt.setAllowedNamespaces, tt.setNamespace, "my-default-namespace")
			assert.Nil(t, err)
			assert.Equal(t, tt.wantNamespaces, actualNamespaces)
		})
	}
}

func TestToNamespacesError(t *testing.T) {
	tests := []struct {
		name                 string
		setAllowedNamespaces []string
		setNamespace         *string
		wantError            error
	}{
		{
			"single namespace allowed: <empty>",
			[]string{"testns"},
			nil,
			errors.ErrForbidden,
		},
		{
			"single namespace allowed: not-testns",
			[]string{"testns"},
			ptr.To("not-testns"),
			errors.ErrForbidden,
		},
		{
			"multiple namespaces allowed: <empty>",
			[]string{"testns1", "testns2"},
			nil,
			errors.ErrForbidden,
		},
		{
			"multiple namespaces allowed: not-testns1",
			[]string{"testns1", "testns2"},
			ptr.To("not-testns1"),
			errors.ErrForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNamespaces, err := DerefNamespaceToList(tt.setAllowedNamespaces, tt.setNamespace, "my-default-namespace")
			assert.NotNil(t, err)
			assert.Equal(t, tt.wantError, err)
			assert.Equal(t, []string(nil), actualNamespaces)
		})
	}
}
