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

package graph

import (
	"slices"
	"strings"
	"testing"
)

// The auth projection is the whole distance between the Kubetail account and the webview, and it
// is one schema edit wide: AuthState binds onto connectors.State and Identity onto oauth.Identity,
// so a field declared here may be served with no Go code written to review. Pinning both types'
// fields is what makes that edit fail rather than ship.
func TestAuthProjectionCarriesNoTokens(t *testing.T) {
	for _, tc := range []struct {
		typeName string
		want     []string
	}{
		{"AuthState", []string{"authenticated", "identity", "restoring"}},
		{"Identity", []string{"email", "name", "sub"}},
	} {
		def := parsedSchema.Types[tc.typeName]
		if def == nil {
			t.Fatalf("%s: not in the schema", tc.typeName)
		}
		var got []string
		for _, f := range def.Fields {
			got = append(got, f.Name)
		}
		slices.Sort(got)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s fields = %v, want %v — a field added here may be served straight off its Go type; if it carries no credential, add it to this list", tc.typeName, got, tc.want)
		}
	}
}

// A query with no caller is removed (docs/read-contracts/README.md): each field here has a
// document in the webview.
func TestEveryQueryFieldHasACaller(t *testing.T) {
	var got []string
	for _, f := range parsedSchema.Query.Fields {
		if !strings.HasPrefix(f.Name, "__") {
			got = append(got, f.Name)
		}
	}
	slices.Sort(got)
	want := []string{"chatPermissionRulesGet", "envVarsGet", "logsFetch", "permissionSettingsGet", "sandboxCloudAccessGet", "sandboxPathFaultGet", "sandboxPathGet", "sandboxStateGet"}
	if !slices.Equal(got, want) {
		t.Fatalf("Query fields = %v, want %v", got, want)
	}
}

// A query field ends in its contract's verb, and its description opens with the
// contract (docs/read-contracts/README.md).
func TestEveryQueryFieldIsNamedForItsContract(t *testing.T) {
	opening := map[string]string{"Get": "A Get of ", "List": "A List of ", "Fetch": "A Fetch of "}
	for _, f := range parsedSchema.Query.Fields {
		if strings.HasPrefix(f.Name, "__") {
			continue
		}
		var verb string
		for v := range opening {
			if strings.HasSuffix(f.Name, v) {
				verb = v
			}
		}
		if verb == "" {
			t.Errorf("%s: ends in none of Get, List or Fetch", f.Name)
			continue
		}
		if !strings.HasPrefix(f.Description, opening[verb]) {
			t.Errorf("%s: description %q does not open with %q", f.Name, f.Description, opening[verb])
		}
	}
}

// A query's result type is named for its contract: a Get returns the value itself, a Fetch its
// `<Noun>FetchResult`; and a noun of Kubetail's own ends in State, never Status.
func TestQueryResultTypesAreNamedForTheirContract(t *testing.T) {
	results := map[string]string{}
	for _, f := range parsedSchema.Query.Fields {
		results[f.Name] = f.Type.Name()
	}
	if got := results["logsFetch"]; got != "LogsFetchResult" {
		t.Errorf("logsFetch returns %s, want LogsFetchResult", got)
	}
	if got := results["sandboxStateGet"]; got != "SandboxState" {
		t.Errorf("sandboxStateGet returns %s, want SandboxState", got)
	}
}
