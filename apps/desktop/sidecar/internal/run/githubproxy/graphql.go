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
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/run/permissions"
)

// destructiveMutations are the mutations that are destructive.
var destructiveMutations = []string{"deleteRef"}

// accessControlWords mark a mutation that changes who may do what, read in
// its name: branch protection and rulesets, collaborators, teams' access,
// IP allow lists, admins, roles, permissions and members' settings.
var accessControlWords = []string{
	"BranchProtectionRule", "RepositoryRuleset", "Collaborator", "TeamsRepository", "IpAllowList",
	"Admin", "Role", "Permission", "MembersCan",
}

// graphQL is what the proxy reads of a GraphQL request.
type graphQL struct {
	mutations []string // every mutation's top-level fields, in order
	operation string   // operationName, as sent
	// maintainerEdits is whether a pull request's update sets whether its
	// base's maintainers may push to its branch.
	maintainerEdits bool
}

// destructive is whether g holds a destructive mutation.
func (g graphQL) destructive() bool {
	return slices.ContainsFunc(g.mutations, func(f string) bool { return slices.Contains(destructiveMutations, f) })
}

// changesAccess is whether g holds a mutation that changes who may do what.
func (g graphQL) changesAccess() bool {
	named := slices.ContainsFunc(g.mutations, func(f string) bool {
		return slices.ContainsFunc(accessControlWords, func(w string) bool { return strings.Contains(f, w) })
	})
	return named || g.maintainerEdits
}

// action is the request's action: a read with no mutation, else a change.
// It names no scope, since a mutation names its objects in its body, and no
// mutation is on ordinary, so each asks each time.
func (g graphQL) action() permissions.Action {
	act := permissions.Action{Service: "graphql", Operation: "query", Resource: "graphql", AccessLevel: permissions.Read}
	switch {
	case len(g.mutations) == 0:
	case g.destructive():
		act.Operation, act.AccessLevel = "mutation", permissions.Destructive
	case g.changesAccess():
		act.Operation, act.AccessLevel = "mutation", permissions.AccessControl
	default:
		act.Operation, act.AccessLevel, act.Once = "mutation", permissions.Write, true
	}
	return act
}

// readGraphQL reads body as GitHub would, or refuses it: one JSON object, no
// key twice, a string query, an operationName that is a string or null, and a
// document that parses and holds queries and mutations alone. Any mutation in
// the document makes the request a change, whichever operation it selects.
func readGraphQL(body []byte) (graphQL, bool) {
	query, operation, variables, ok := readBody(body)
	if !ok {
		return graphQL{}, false
	}
	g := graphQL{operation: operation}
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil || len(doc.Operations) == 0 {
		return graphQL{}, false
	}
	for _, op := range doc.Operations {
		switch op.Operation {
		case ast.Query:
		case ast.Mutation:
			fields, ok := fieldsOf(op.SelectionSet, doc.Fragments, map[string]bool{})
			if !ok {
				return graphQL{}, false
			}
			for _, f := range fields {
				g.mutations = append(g.mutations, f.Name)
				if f.Name == "updatePullRequest" && setsMaintainerEdits(f, op, variables) {
					g.maintainerEdits = true
				}
			}
		default:
			return graphQL{}, false
		}
	}
	return g, true
}

// readBody is body's query and operationName.
func readBody(body []byte) (query, operation string, variables map[string]any, ok bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return "", "", nil, false
	}
	var q *string
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return "", "", nil, false
		}
		key := tok.(string) // an object's key is a string, or Token fails
		if seen[key] {
			return "", "", nil, false
		}
		seen[key] = true
		switch key {
		case "query":
			var text string
			if dec.Decode(&text) != nil {
				return "", "", nil, false
			}
			q = &text
		case "variables":
			if dec.Decode(&variables) != nil {
				return "", "", nil, false
			}
		case "operationName":
			var name *string
			if dec.Decode(&name) != nil {
				return "", "", nil, false
			}
			if name != nil {
				operation = *name
			}
		default:
			var skip json.RawMessage
			if dec.Decode(&skip) != nil {
				return "", "", nil, false
			}
		}
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return "", "", nil, false
	}
	if _, err := dec.Token(); err != io.EOF || q == nil {
		return "", "", nil, false
	}
	return *q, operation, variables, true
}

// fieldsOf is set's top-level field names, read through its inline fragments
// and the fragments it spreads, each once; false for a spread of a fragment
// the document does not hold.
func fieldsOf(set ast.SelectionSet, fragments ast.FragmentDefinitionList, spread map[string]bool) ([]*ast.Field, bool) {
	var fields []*ast.Field
	for _, sel := range set {
		var inner ast.SelectionSet
		switch s := sel.(type) {
		case *ast.Field:
			fields = append(fields, s)
			continue
		case *ast.InlineFragment:
			inner = s.SelectionSet
		case *ast.FragmentSpread:
			def := fragments.ForName(s.Name)
			if def == nil {
				return nil, false
			}
			if spread[s.Name] {
				continue
			}
			spread[s.Name] = true
			inner = def.SelectionSet
		}
		more, ok := fieldsOf(inner, fragments, spread)
		if !ok {
			return nil, false
		}
		fields = append(fields, more...)
	}
	return fields, true
}

// setsMaintainerEdits is whether f, an updatePullRequest, lets the base's
// maintainers push to the branch: its input's maintainerCanModify is true,
// written in the document or through a variable of op.
func setsMaintainerEdits(f *ast.Field, op *ast.OperationDefinition, variables map[string]any) bool {
	input := f.Arguments.ForName("input")
	if input == nil {
		return false
	}
	if input.Value.Kind == ast.Variable {
		object, _ := variableOf(input.Value.Raw, op, variables).(map[string]any)
		return object["maintainerCanModify"] == true
	}
	flag := input.Value.Children.ForName("maintainerCanModify")
	switch {
	case flag == nil:
		return false
	case flag.Kind == ast.Variable:
		return variableOf(flag.Raw, op, variables) == true
	}
	return flag.Kind == ast.BooleanValue && flag.Raw == "true"
}

// variableOf is the value op's variable name takes: the one sent, else its
// default as GitHub reads it.
func variableOf(name string, op *ast.OperationDefinition, variables map[string]any) any {
	if v, ok := variables[name]; ok {
		return v
	}
	def := op.VariableDefinitions.ForName(name)
	if def == nil || def.DefaultValue == nil {
		return nil
	}
	value, err := def.DefaultValue.Value(nil)
	if err != nil {
		return nil
	}
	return value
}
