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

package kubestore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// appWeb is a LabelSelector matching app=web.
var appWeb = map[string]any{"matchLabels": map[string]any{"app": "web"}}

func TestSelectorOfReadsEachSelectingKind(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		apiVersion string
		kind       string
		fields     map[string]any
		row        bool
	}{
		{"Service", "v1", "Service", map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "web"}}}, true},
		{"Deployment", "apps/v1", "Deployment", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"ReplicaSet", "apps/v1", "ReplicaSet", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"StatefulSet", "apps/v1", "StatefulSet", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"DaemonSet", "apps/v1", "DaemonSet", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"Job", "batch/v1", "Job", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"PodDisruptionBudget", "policy/v1", "PodDisruptionBudget", map[string]any{"spec": map[string]any{"selector": appWeb}}, true},
		{"NetworkPolicy", "networking.k8s.io/v1", "NetworkPolicy", map[string]any{"spec": map[string]any{"podSelector": appWeb}}, true},

		{"a Service with an empty selector", "v1", "Service", map[string]any{"spec": map[string]any{"selector": map[string]any{}}}, false},
		{"a Service with no selector", "v1", "Service", map[string]any{"spec": map[string]any{"type": "ExternalName"}}, false},
		{"a policy/v1beta1 PodDisruptionBudget with {}", "policy/v1beta1", "PodDisruptionBudget",
			map[string]any{"spec": map[string]any{"selector": map[string]any{}}}, false},
		{"a policy/v1 PodDisruptionBudget with {}", "policy/v1", "PodDisruptionBudget",
			map[string]any{"spec": map[string]any{"selector": map[string]any{}}}, true},
		{"a kind of another group spelled the same", "example.com/v1", "Deployment", map[string]any{"spec": map[string]any{"selector": appWeb}}, false},
		{"a kind with no selector", "v1", "ConfigMap", map[string]any{"data": map[string]any{"selector": "x"}}, false},
	} {
		sel, ok := selectorOf(bodyOf(tc.apiVersion, tc.kind, tc.fields))
		assert.Equal(t, tc.row, ok, tc.name)
		if ok {
			assert.Equal(t, "prod", sel.Namespace, tc.name)
		}
	}
}

func TestSelectorTermsReadEachForm(t *testing.T) {
	t.Parallel()

	expr := func(key, op string, vals ...string) map[string]any {
		values := []any{}
		for _, v := range vals {
			values = append(values, v)
		}
		return map[string]any{"key": key, "operator": op, "values": values}
	}
	deployment := func(selector any) map[string]any {
		return map[string]any{"spec": map[string]any{"selector": selector}}
	}
	for _, tc := range []struct {
		name   string
		u      map[string]any
		kind   [2]string
		terms  []selectorTerm
		absent bool
	}{
		{name: "a Service's map", kind: [2]string{"v1", "Service"},
			u:     map[string]any{"spec": map[string]any{"selector": map[string]any{"tier": "front", "app": "web"}}},
			terms: []selectorTerm{{Key: "app", Op: "In", Vals: []string{"web"}}, {Key: "tier", Op: "In", Vals: []string{"front"}}}},
		{name: "matchLabels", kind: [2]string{"apps/v1", "Deployment"},
			u:     deployment(map[string]any{"matchLabels": map[string]any{"app": "web"}}),
			terms: []selectorTerm{{Key: "app", Op: "In", Vals: []string{"web"}}}},
		{name: "matchExpressions", kind: [2]string{"apps/v1", "Deployment"},
			u: deployment(map[string]any{"matchExpressions": []any{
				expr("a", "In", "x", "y"), expr("b", "NotIn", "z"), expr("c", "Exists"), expr("d", "DoesNotExist"),
			}}),
			terms: []selectorTerm{
				{Key: "a", Op: "In", Vals: []string{"x", "y"}},
				{Key: "b", Op: "NotIn", Vals: []string{"z"}},
				{Key: "c", Op: "Exists", Vals: []string{}},
				{Key: "d", Op: "DoesNotExist", Vals: []string{}},
			}},
		{name: "an empty selector", kind: [2]string{"apps/v1", "Deployment"}, u: deployment(map[string]any{})},
		{name: "a PodDisruptionBudget with no selector", kind: [2]string{"policy/v1", "PodDisruptionBudget"},
			u: map[string]any{"spec": map[string]any{"minAvailable": int64(1)}}, absent: true},
		{name: "an operator apimachinery refuses", kind: [2]string{"apps/v1", "Deployment"},
			u: deployment(map[string]any{"matchExpressions": []any{expr("a", "Near", "x")}}), absent: true},
		{name: "a key apimachinery refuses", kind: [2]string{"apps/v1", "Deployment"},
			u: deployment(map[string]any{"matchLabels": map[string]any{"bad key": "x"}}), absent: true},
		{name: "a selector that does not convert", kind: [2]string{"apps/v1", "Deployment"},
			u: deployment(map[string]any{"matchLabels": "app=web"}), absent: true},
		{name: "a Service's map apimachinery would refuse", kind: [2]string{"v1", "Service"},
			u:     map[string]any{"spec": map[string]any{"selector": map[string]any{"app": "bad value!"}}},
			terms: []selectorTerm{{Key: "app", Op: "In", Vals: []string{"bad value!"}}}},
	} {
		sel, ok := selectorOf(bodyOf(tc.kind[0], tc.kind[1], tc.u))
		assert.Equal(t, !tc.absent, ok, tc.name)
		assert.ElementsMatch(t, tc.terms, sel.Terms, tc.name)
	}
}

var serviceKind = Kind{APIVersion: "v1", Kind: "Service", Resource: "services"}

// service is a Service of the given uid and resourceVersion selecting the given labels.
func service(uid, rv string, selector map[string]any) *unstructured.Unstructured {
	return obj(map[string]any{
		"apiVersion": "v1", "kind": "Service",
		"metadata": map[string]any{"uid": uid, "name": uid, "namespace": "prod", "resourceVersion": rv},
		"spec":     map[string]any{"selector": selector},
	})
}

// selectorTerms are an object's selector as namespace:key op vals, one per term in term order,
// or nil when it has no selectors row.
func selectorTerms(t *testing.T, s *Store, uid string) []string {
	t.Helper()
	if countRows(t, s, `SELECT COUNT(*) FROM selectors WHERE uid = ?`, uid) == 0 {
		return nil
	}
	rows, err := db(t, s).QueryContext(context.Background(), `
		SELECT s.namespace || ':' || t.key || ' ' || t.op || ' ' || t.vals
		FROM selectors s JOIN selector_terms t ON t.uid = s.uid
		WHERE s.uid = ? ORDER BY t.term`, uid)
	require.NoError(t, err)
	defer rows.Close()
	terms := []string{}
	for rows.Next() {
		var term string
		require.NoError(t, rows.Scan(&term))
		terms = append(terms, term)
	}
	require.NoError(t, rows.Err())
	return terms
}

func TestSelectorsFollowTheirObject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Added, service("svc", "1", map[string]any{"app": "web", "tier": "front"})))
	assert.Equal(t, []string{`prod:app In ["web"]`, `prod:tier In ["front"]`}, selectorTerms(t, s, "svc"))

	require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Modified, service("svc", "2", map[string]any{"app": "web"})))
	assert.Equal(t, []string{`prod:app In ["web"]`}, selectorTerms(t, s, "svc"))

	require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Modified, service("svc", "3", map[string]any{})))
	assert.Nil(t, selectorTerms(t, s, "svc"))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM selector_terms`))

	// A selector with no terms is still a row.
	require.NoError(t, s.ApplyChange(ctx, deploymentKind, watch.Added, obj(map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment",
		"metadata": map[string]any{"uid": "dep", "name": "dep", "namespace": "prod", "resourceVersion": "1"},
		"spec":     map[string]any{"selector": map[string]any{}},
	})))
	assert.Equal(t, []string{}, selectorTerms(t, s, "dep"))

	require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Modified, service("svc", "4", map[string]any{"app": "web"})))
	require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Deleted, service("svc", "5", map[string]any{"app": "web"})))
	assert.Nil(t, selectorTerms(t, s, "svc"))

	for _, uid := range []string{"gone", "kept"} {
		require.NoError(t, s.ApplyChange(ctx, serviceKind, watch.Added, service(uid, "1", map[string]any{"app": "web"})))
	}
	session := beginReplace(t, s, serviceKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{service("kept", "1", map[string]any{"app": "web"})}))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)
	assert.Nil(t, selectorTerms(t, s, "gone"))
	assert.NotNil(t, selectorTerms(t, s, "kept"))

	require.NoError(t, s.ClearKind(ctx, serviceKind))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM selectors WHERE uid = 'kept'`))
	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM selector_terms`))
}
