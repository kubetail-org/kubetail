// Copyright 2024 Andres Morey
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

package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

func TestToNamespaceSuccess(t *testing.T) {
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
			"default",
		},
		{
			"any namespace allowed: <all>",
			[]string{},
			ptr.To[string](""),
			"",
		},
		{
			"any namespace allowed: default",
			[]string{},
			ptr.To[string]("default"),
			"default",
		},
		{
			"any namespace allowed: testns",
			[]string{},
			ptr.To[string]("testns"),
			"testns",
		},
		{
			"single namespace allowed: testns",
			[]string{"testns"},
			ptr.To[string]("testns"),
			"testns",
		},
		{
			"multiple namespaces allowed: <all>",
			[]string{"testns1", "testns2"},
			ptr.To[string]("testns1"),
			"testns1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{allowedNamespaces: tt.setAllowedNamespaces}
			actualNamespace := r.ToNamespace(tt.setNamespace)
			assert.Equal(t, tt.wantNamespace, actualNamespace)
		})
	}
}

func TestToNamespaceError(t *testing.T) {
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
			"single namespace allowed: not-testns",
			[]string{"testns"},
			ptr.To[string]("not-testns"),
		},
		{
			"multiple namespaces allowed: <empty>",
			[]string{"testns1", "testns2"},
			nil,
		},
		{
			"multiple namespaces allowed: not-testns1",
			[]string{"testns1", "testns2"},
			ptr.To[string]("not-testns1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Panics(t, func() {
				r := Resolver{allowedNamespaces: tt.setAllowedNamespaces}
				r.ToNamespace(tt.setNamespace)
			})
		})
	}
}

func TestToNamespacesSuccess(t *testing.T) {
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
			[]string{"default"},
		},
		{
			"any namespace allowed: <all>",
			[]string{},
			ptr.To[string](""),
			[]string{""},
		},
		{
			"any namespace allowed: default",
			[]string{},
			ptr.To[string]("default"),
			[]string{"default"},
		},
		{
			"any namespace allowed: testns",
			[]string{},
			ptr.To[string]("testns"),
			[]string{"testns"},
		},
		{
			"single namespace allowed: <all>",
			[]string{"testns"},
			ptr.To[string](""),
			[]string{"testns"},
		},
		{
			"single namespace allowed: testns",
			[]string{"testns"},
			ptr.To[string]("testns"),
			[]string{"testns"},
		},
		{
			"multiple namespaces allowed: <all>",
			[]string{"testns1", "testns2"},
			ptr.To[string](""),
			[]string{"testns1", "testns2"},
		},
		{
			"multiple namespaces allowed: testns1",
			[]string{"testns1", "testns2"},
			ptr.To[string]("testns1"),
			[]string{"testns1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{allowedNamespaces: tt.setAllowedNamespaces}
			actualNamespaces, err := r.ToNamespaces(tt.setNamespace)
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
			ErrForbidden,
		},
		{
			"single namespace allowed: not-testns",
			[]string{"testns"},
			ptr.To[string]("not-testns"),
			ErrForbidden,
		},
		{
			"multiple namespaces allowed: <empty>",
			[]string{"testns1", "testns2"},
			nil,
			ErrForbidden,
		},
		{
			"multiple namespaces allowed: not-testns1",
			[]string{"testns1", "testns2"},
			ptr.To[string]("not-testns1"),
			ErrForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := Resolver{allowedNamespaces: tt.setAllowedNamespaces}
			actualNamespaces, err := r.ToNamespaces(tt.setNamespace)
			assert.NotNil(t, err)
			assert.Equal(t, tt.wantError, err)
			assert.Equal(t, []string(nil), actualNamespaces)
		})
	}
}
