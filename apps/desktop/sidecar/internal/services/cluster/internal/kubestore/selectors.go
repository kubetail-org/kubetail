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
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
)

// selectorRow is the Pod selector one object carries, as the selectors table keeps it.
type selectorRow struct {
	Namespace string
	// Terms are the requirements a Pod must meet, all of them; none matches every Pod.
	Terms []selectorTerm
}

// selectorTerm is one row of the selector_terms table. The JSON names are the columns, since
// the terms bind as one JSON argument.
type selectorTerm struct {
	Key  string   `json:"key"`
	Op   string   `json:"op"`
	Vals []string `json:"vals"`
}

// termOps spells each operator a selector can hold as the selects view reads it.
var termOps = map[selection.Operator]string{
	selection.Equals:       "In",
	selection.DoubleEquals: "In",
	selection.In:           "In",
	selection.NotEquals:    "NotIn",
	selection.NotIn:        "NotIn",
	selection.Exists:       "Exists",
	selection.DoesNotExist: "DoesNotExist",
}

// podSelectorAt is where each selecting kind keeps its LabelSelector, keyed by api group and
// kind. A Service's selector is a map, and read apart.
var podSelectorAt = map[[2]string][]string{
	{"apps", "Deployment"}:                 {"spec", "selector"},
	{"apps", "ReplicaSet"}:                 {"spec", "selector"},
	{"apps", "StatefulSet"}:                {"spec", "selector"},
	{"apps", "DaemonSet"}:                  {"spec", "selector"},
	{"batch", "Job"}:                       {"spec", "selector"},
	{"policy", "PodDisruptionBudget"}:      {"spec", "selector"},
	{"networking.k8s.io", "NetworkPolicy"}: {"spec", "podSelector"},
}

// selectsByKind says u's kind, by its body's own group and kind, can carry a Pod selector.
func selectsByKind(u *unstructured.Unstructured) bool {
	gk := groupKindOf(u)
	_, ok := podSelectorAt[gk]
	return ok || gk == [2]string{"", "Service"}
}

// selectorOf reads the Pod selector u carries, by its body's own group and kind, through
// apimachinery so it means what it means to the cluster. False is no row: a kind that
// selects nothing, or a selector that selects nothing or does not read.
func selectorOf(u *unstructured.Unstructured) (selectorRow, bool) {
	sel, ok := podSelector(u)
	if !ok {
		return selectorRow{}, false
	}
	// Read the flag, never the empty list: a nil selector has no requirements and selects
	// nothing.
	reqs, selectable := sel.Requirements()
	if !selectable {
		return selectorRow{}, false
	}
	row := selectorRow{Namespace: u.GetNamespace()}
	for _, r := range reqs {
		op, ok := termOps[r.Operator()]
		if !ok { // coverage:ignore LabelSelectorAsSelector and SelectorFromSet make only the operators termOps spells
			// A term the view cannot read would match what the cluster does not.
			return selectorRow{}, false
		}
		row.Terms = append(row.Terms, selectorTerm{Key: r.Key(), Op: op, Vals: r.Values().List()})
	}
	return row, true
}

// podSelector is u's selector as apimachinery reads it.
func podSelector(u *unstructured.Unstructured) (labels.Selector, bool) {
	gk := groupKindOf(u)
	if gk == [2]string{"", "Service"} {
		// A Service without a selector selects nothing: its endpoints are someone else's.
		set, _, err := unstructured.NestedStringMap(u.Object, "spec", "selector")
		if err != nil || len(set) == 0 {
			return nil, false
		}
		return labels.SelectorFromSet(set), true
	}
	at, ok := podSelectorAt[gk]
	if !ok {
		return nil, false
	}
	var ls *metav1.LabelSelector
	if m, ok, _ := unstructured.NestedMap(u.Object, at...); ok {
		ls = &metav1.LabelSelector{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(m, ls) != nil {
			return nil, false
		}
		// policy/v1beta1 reads an empty selector as selecting nothing; policy/v1 as every Pod.
		if gk[1] == "PodDisruptionBudget" && u.GetAPIVersion() == "policy/v1beta1" &&
			len(ls.MatchLabels)+len(ls.MatchExpressions) == 0 {
			return nil, false
		}
	}
	sel, err := metav1.LabelSelectorAsSelector(ls)
	if err != nil {
		return nil, false
	}
	return sel, true
}

// writeSelector rewrites the object's selector rows: none, or its selector and terms.
func writeSelector(ctx context.Context, st stmts, row objectRow) error {
	for _, id := range []stmtID{stmtDeleteSelectorOfObject, stmtDeleteSelectorTermsOfObject} {
		if _, err := st.Exec(ctx, id, row.UID); err != nil {
			return err
		}
	}
	if row.Selector == nil {
		return nil
	}
	if _, err := st.Exec(ctx, stmtInsertSelector, row.UID, row.Selector.Namespace); err != nil {
		return err
	}
	if len(row.Selector.Terms) == 0 {
		return nil
	}
	termsJSON, err := json.Marshal(row.Selector.Terms)
	if err != nil { // coverage:ignore plain values always marshal
		return err
	}
	_, err = st.Exec(ctx, stmtInsertSelectorTerms, row.UID, string(termsJSON))
	return err
}
