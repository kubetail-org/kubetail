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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/watch"
)

// body decodes a projected row's stored JSON back into a map.
func body(t *testing.T, row objectRow) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(row.RawJSON, &out))
	return out
}

func TestProjectReadsTheIdentityColumns(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"uid": "uid-1", "name": "api-0", "namespace": "prod",
			"resourceVersion": "42", "generation": int64(3),
			"creationTimestamp": "2026-08-01T00:00:00Z",
			"labels":            map[string]any{"app": "api"},
		},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	assert.Equal(t, "uid-1", row.UID)
	assert.Equal(t, "prod", row.Namespace)
	assert.Equal(t, "api-0", row.Name)
	assert.Equal(t, "42", row.ResourceVersion)
	assert.Equal(t, int64(3), row.Generation)
	assert.NotZero(t, row.CreatedAt)
	assert.Equal(t, map[string]string{"app": "api"}, row.Labels)
}

// managedFields and the kubectl last-applied annotation are roughly half a typical
// object's bytes and nothing reads them — stripping is what lets the stored body be
// served verbatim.
func TestProjectStripsServerNoise(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "ConfigMap",
		"metadata": map[string]any{
			"uid": "uid-1", "name": "cm",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
		},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	meta := body(t, row)["metadata"].(map[string]any)
	assert.NotContains(t, meta, "managedFields")
	// The annotations map emptied by that removal is noise of its own.
	assert.NotContains(t, meta, "annotations")
}

// The cache file must never hold the cluster's credentials. Keys survive so a UI can
// list what a Secret holds.
func TestProjectRedactsSecretValues(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata":   map[string]any{"uid": "uid-1", "name": "creds"},
		"data":       map[string]any{"password": "aHVudGVyMg=="},
		"stringData": map[string]any{"password": "hunter2"},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	stored := body(t, row)
	assert.Equal(t, map[string]any{"password": redactedValue}, stored["data"])
	assert.NotContains(t, stored, "stringData")
	// The caller's body is the live watch object; projecting must not mutate it.
	assert.Equal(t, "hunter2", u.Object["stringData"].(map[string]any)["password"])
}

// Redaction reads the BODY's own kind, so it cannot be bypassed by how the collection
// was addressed.
func TestProjectRedactsOnlyCoreSecrets(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Secret",
		"metadata": map[string]any{"uid": "uid-1", "name": "not-a-secret"},
		"data":     map[string]any{"password": "kept"},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	assert.Equal(t, map[string]any{"password": "kept"}, body(t, row)["data"])
}

// A CRD that inlines a credential rather than referencing one is redacted at the path its
// own schema names, and the rest of the body is left intact.
func TestProjectRedactsInlineCRDCredentials(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"uid": "uid-1", "name": "tls"},
		"spec": map[string]any{
			"dnsNames":  []any{"api.example.com"},
			"keystores": map[string]any{"jks": map[string]any{"create": true, "password": "hunter2"}},
		},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	jks := body(t, row)["spec"].(map[string]any)["keystores"].(map[string]any)["jks"].(map[string]any)
	assert.Equal(t, redactedValue, jks["password"])
	assert.Equal(t, true, jks["create"], "the rest of the body survives")
}

// A map-valued entry keeps its keys, so a reader can still list what the object holds.
func TestProjectRedactsCRDCredentialMapValuesKeepingKeys(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "grafana.integreatly.org/v1beta1", "kind": "GrafanaDatasource",
		"metadata": map[string]any{"uid": "uid-1", "name": "prom"},
		"spec": map[string]any{"datasource": map[string]any{
			"jsonData":       map[string]any{"timeInterval": "5s"},
			"secureJsonData": map[string]any{"basicAuthPassword": "hunter2"},
		}},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	ds := body(t, row)["spec"].(map[string]any)["datasource"].(map[string]any)
	assert.Equal(t, map[string]any{"basicAuthPassword": redactedValue}, ds["secureJsonData"])
	assert.Equal(t, map[string]any{"timeInterval": "5s"}, ds["jsonData"], "configuration is not a credential")
}

// A redaction whose path the body does not carry is a no-op, not an invented field.
func TestSanitizeLeavesABodyMissingARedactedPath(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"uid": "uid-1", "name": "tls"},
		"spec":     map[string]any{"dnsNames": []any{"api.example.com"}},
	})

	got := sanitize(u)

	assert.NotContains(t, got.Object["spec"], "keystores")
}

func TestProjectKeepsOnlyOwnerRefsThatCarryAUID(t *testing.T) {
	t.Parallel()

	yes := true
	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "uid-1", "name": "api-0", "ownerReferences": []any{
			map[string]any{"uid": "owner-1", "controller": yes},
			map[string]any{"name": "no-uid"},
		}},
	})

	row, err := projectObject(u)

	require.NoError(t, err)
	assert.Equal(t, []ownerRef{{UID: "owner-1", IsController: true}}, row.OwnerRefs)
}

// A body with no UID is unkeyable: nothing could address the row later, and a delta
// for it would book progress against a row that was never written.
func TestProjectRefusesAnObjectWithNoUID(t *testing.T) {
	t.Parallel()

	_, err := projectObject(obj(map[string]any{"apiVersion": "v1", "kind": "Pod"}))

	assert.Error(t, err)
}

func TestProjectRefusesAnEmptyBody(t *testing.T) {
	t.Parallel()

	_, err := projectObject(&unstructured.Unstructured{})

	assert.Error(t, err)
}

// podWith builds a Pod carrying the given owner references and labels. A nil for either
// is the shape apimachinery hands back for an object that has none.
func podWith(uid string, owners []any, labels map[string]any) *unstructured.Unstructured {
	meta := map[string]any{"uid": uid, "name": "api-0", "namespace": "prod", "resourceVersion": "1"}
	if owners != nil {
		meta["ownerReferences"] = owners
	}
	if labels != nil {
		meta["labels"] = labels
	}
	return obj(map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": meta})
}

// owner is one ownerReferences entry.
func owner(uid string, controller bool) map[string]any {
	return map[string]any{"uid": uid, "kind": "ReplicaSet", "name": "rs", "controller": controller}
}

func TestInsertRoundTripsEveryOwnerRef(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	u := podWith("uid-1", []any{owner("o-1", true), owner("o-2", false), owner("o-3", false)}, nil)

	require.NoError(t, s.ApplyChange(context.Background(), podKind, watch.Added, u))

	assert.Equal(t, 3, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE child_uid='uid-1'`))
	assert.Equal(t, 1, countRows(t, s,
		`SELECT is_controller FROM owner_refs WHERE child_uid='uid-1' AND owner_uid='o-1'`))
	assert.Zero(t, countRows(t, s,
		`SELECT is_controller FROM owner_refs WHERE child_uid='uid-1' AND owner_uid='o-2'`))
}

// The insert upserts, so only the DELETE ahead of it can retire an edge the object dropped.
func TestInsertDropsAnOwnerRefTheObjectNoLongerCarries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added,
		podWith("uid-1", []any{owner("o-1", true), owner("o-2", false)}, nil)))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified,
		podWith("uid-1", []any{owner("o-1", true)}, nil)))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE child_uid='uid-1'`))
}

// The nil guard: OwnerRefs is built by append, so an object with no owner marshals to
// `null`, and json_each('null') yields one all-NULL row — a NOT NULL violation under
// STRICT, inside the relist page's transaction, for every unowned object in the cluster.
func TestInsertTakesAnObjectWithNoOwnerRefs(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(context.Background(), podKind, watch.Added,
		podWith("uid-1", nil, nil)))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM owner_refs WHERE child_uid='uid-1'`))
}

func TestInsertRoundTripsEveryLabel(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	u := podWith("uid-1", nil, map[string]any{"app": "api", "tier": "web", "env": "prod"})

	require.NoError(t, s.ApplyChange(context.Background(), podKind, watch.Added, u))

	assert.Equal(t, 3, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='uid-1'`))
	assert.Equal(t, 1, countRows(t, s,
		`SELECT COUNT(*) FROM labels WHERE uid='uid-1' AND key='tier' AND value='web'`))
}

// The insert upserts, so only the DELETE ahead of it can retire a label the object dropped.
func TestInsertDropsALabelTheObjectNoLongerCarries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added,
		podWith("uid-1", nil, map[string]any{"app": "api", "tier": "web"})))

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified,
		podWith("uid-1", nil, map[string]any{"app": "api"})))

	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='uid-1'`))
}

// The other half of the nil guard: an unlabelled object's Labels is the nil map
// apimachinery returns, which marshals to `null` exactly as the ref slice does.
func TestInsertTakesAnObjectWithNoLabels(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(context.Background(), podKind, watch.Added,
		podWith("uid-1", nil, nil)))

	assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM labels WHERE uid='uid-1'`))
}

// A body carrying a value JSON cannot represent is skipped, not fatal: a relist drops it
// until the next pass, where failing would hand the same body back forever.
func TestProjectObjectRefusesABodyThatWillNotMarshal(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"uid": "uid-1", "name": "api-0"},
		// A float64 the copier carries happily and the encoder rejects.
		"spec": map[string]any{"weight": math.NaN()},
	})

	_, err := projectObject(u)

	assert.ErrorIs(t, err, errUnprojectable)
}

// A value we cannot read is a value we cannot prove is safe: the field goes, rather than
// being stored because it did not parse.
func TestSanitizeDropsASecretWhoseDataIsNotAMap(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"uid": "uid-1", "name": "creds"},
		"data":     "not-a-map",
	})

	got := sanitize(u)

	assert.NotContains(t, got.Object, "data")
}

func TestSanitizeDropsAPasswordThatIsNotAString(t *testing.T) {
	t.Parallel()

	u := obj(map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"uid": "uid-1", "name": "tls"},
		"spec": map[string]any{"keystores": map[string]any{
			"jks": map[string]any{"password": map[string]any{"value": "hunter2"}},
		}},
	})

	got := sanitize(u)

	jks, _, err := unstructured.NestedMap(got.Object, "spec", "keystores", "jks")
	require.NoError(t, err)
	assert.NotContains(t, jks, "password")
}

// The discrimination is the err a Nested* read returns, not its found boolean: "absent"
// and "there but unreadable" are the same boolean, and only one of them is safe to skip.
// Collapse this back into !ok and the two cases above store the value again.
func TestNestedReadsDistinguishAnAbsentPathFromAnUnreadableOne(t *testing.T) {
	t.Parallel()

	body := map[string]any{"data": "not-a-map"}

	_, missing, missingErr := unstructured.NestedMap(body, "absent")
	_, present, presentErr := unstructured.NestedMap(body, "data")

	assert.False(t, missing)
	assert.NoError(t, missingErr)
	assert.False(t, present)
	assert.Error(t, presentErr, "a wrong-typed value is reported by the error alone")
}

// Redaction reads the body's own group and kind, so a Secret mirrored under some other
// kind's worker is still redacted, and a body that merely claims a redacted kind's name in
// another group is not.
func TestSanitizeRedactsByTheBodysOwnGroupAndKind(t *testing.T) {
	t.Parallel()

	secret := sanitize(obj(map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata": map[string]any{"uid": "uid-1", "name": "creds"},
		"data":     map[string]any{"password": "aHVudGVyMg=="},
	}))
	lookalike := sanitize(obj(map[string]any{
		"apiVersion": "example.com/v1", "kind": "Secret",
		"metadata": map[string]any{"uid": "uid-2", "name": "creds"},
		"data":     map[string]any{"password": "aHVudGVyMg=="},
	}))

	assert.Equal(t, map[string]any{"password": redactedValue}, secret.Object["data"])
	assert.Equal(t, map[string]any{"password": "aHVudGVyMg=="}, lookalike.Object["data"])
}

// writeSeq is one object's stamp, and 0 when the row is gone.
func writeSeq(t *testing.T, s *Store, uid string) int64 {
	t.Helper()
	return int64(countRows(t, s, `SELECT COALESCE(MAX(write_seq), 0) FROM objects WHERE uid = ?`, uid))
}

// The stamp is what a reader resumes from: it must move for a write that changed the
// object, and stay put for one that did not.
func TestAnObjectIsStampedWithItsWritePosition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	first := writeSeq(t, s, "uid-1")
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-1", "api-0", "42")))
	same := writeSeq(t, s, "uid-1")
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-1", "api-0", "43")))
	moved := writeSeq(t, s, "uid-1")

	assert.Positive(t, first)
	assert.Equal(t, first, same, "the same body carries no news")
	assert.Greater(t, moved, first, "a new resourceVersion is a change")
}

// Nothing upstream rejects a body with no resourceVersion, and an empty one is equal to
// itself forever — so it must never read as "unchanged", or the row would keep the stamp
// of its first write for the life of the cache and no reader past that position would
// ever see it again.
func TestAnObjectWithNoResourceVersionIsAlwaysAChange(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "")))
	first := writeSeq(t, s, "uid-1")
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-1", "api-0", "")))

	assert.Greater(t, writeSeq(t, s, "uid-1"), first)
}

// The upsert rewrites api_version and kind, so the stamp has to move when they do: a
// preferred-version flip reaching the write path before the old kind's rows are cleared
// would otherwise leave the row below every reader of the kind it now belongs to, with no
// delete logged under the kind it left.
func TestAnObjectThatChangesKindMovesItsStamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	renamed := Kind{APIVersion: "v1beta1", Kind: "Pod", Resource: "pods"}

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	first := writeSeq(t, s, "uid-1")
	require.NoError(t, s.ApplyChange(ctx, renamed, watch.Modified, pod("uid-1", "api-0", "42")))

	assert.Greater(t, writeSeq(t, s, "uid-1"), first)
}

// A relist rewrites every row of a kind. Moving the stamp on each rewrite would turn a
// cold list into one change per object, and a reader would take all of them to learn
// that nothing moved.
func TestARelistThatChangesNothingLeavesEveryStamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	rows := []*unstructured.Unstructured{pod("uid-1", "api-0", "42"), pod("uid-2", "api-1", "43")}
	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, rows))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)
	before := []int64{writeSeq(t, s, "uid-1"), writeSeq(t, s, "uid-2")}

	again := beginReplace(t, s, podKind)
	require.NoError(t, again.WritePage(ctx, rows))
	_, err = again.Commit(ctx, "101")
	require.NoError(t, err)

	assert.Equal(t, before, []int64{writeSeq(t, s, "uid-1"), writeSeq(t, s, "uid-2")})
	assert.NotZero(t, before[0])
}

// An object's edges are rewritten on every write, so a table that will not take them fails
// the write: a row whose labels or owners silently stopped being written would serve stale
// edges for as long as the object lived.
func TestInsertObjectRowReportsAnEdgeItCouldNotWrite(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"owner_refs", "labels"} {
		t.Run(table, func(t *testing.T) {
			s := newTestStore(t)
			dropTable(t, s, table)

			err := s.ApplyChange(context.Background(), podKind, watch.Added, pod("uid-1", "api-0", "42"))

			assert.Error(t, err)
		})
	}
	// A Pod's containers are rewritten the same way: the DELETE failing, and the INSERT
	// after a DELETE that worked.
	for name, table := range map[string]string{"delete": "", "insert": `CREATE TABLE containers (pod_uid TEXT)`} {
		t.Run("containers "+name, func(t *testing.T) {
			s := newTestStore(t)
			dropTable(t, s, "containers")
			if table != "" {
				_, err := db(t, s).ExecContext(context.Background(), table)
				require.NoError(t, err)
			}

			err := s.ApplyChange(context.Background(), podKind, watch.Added, podWithContainers("uid-1", "42", "api"))

			assert.Error(t, err)
		})
	}
	// The references and the selector are side tables of the same write.
	t.Run("refs", func(t *testing.T) {
		s := newTestStore(t)
		dropTable(t, s, "refs")

		assert.Error(t, s.ApplyChange(context.Background(), podKind, watch.Added, pod("uid-1", "api-0", "42")))
	})
	for _, table := range []string{"selectors", "selector_terms"} {
		t.Run(table, func(t *testing.T) {
			s := newTestStore(t)
			dropTable(t, s, table)

			assert.Error(t, s.ApplyChange(context.Background(), serviceKind, watch.Added, service("svc", "1", map[string]any{"app": "web"})))
		})
	}
	t.Run("selectors insert", func(t *testing.T) {
		s := newTestStore(t)
		failWrites(t, s, "selectors", "INSERT")

		assert.Error(t, s.ApplyChange(context.Background(), serviceKind, watch.Added, service("svc", "1", map[string]any{"app": "web"})))
		assert.Zero(t, countRows(t, s, `SELECT COUNT(*) FROM objects`), "the object went in without its selector")
	})
}

// The log is written before the row goes and in the same transaction, so a log that will
// not take the entry must fail the delete outright — the alternative is a row gone with no
// record that it went, which is the one thing a resuming reader cannot recover from.
func TestADeleteThatCannotBeLoggedFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	dropTable(t, s, "deletes")

	err := s.ApplyChange(ctx, podKind, watch.Deleted, pod("uid-1", "api-0", "42"))

	assert.Error(t, err)
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects`), "the row went unlogged")
}

// The same promise for the relist's prune, which takes every row the list did not carry.
func TestAPruneThatCannotBeLoggedFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	session := beginReplace(t, s, podKind)
	dropTable(t, s, "deletes")

	_, err := session.Commit(ctx, "100")

	assert.Error(t, err)
	assert.Equal(t, 1, countRows(t, s, `SELECT COUNT(*) FROM objects`), "the rows went unlogged")
}

// changedAt is one object's changed_at, and 0 when the row is gone.
func changedAt(t *testing.T, s *Store, uid string) int64 {
	t.Helper()
	return int64(countRows(t, s, `SELECT COALESCE(MAX(changed_at), 0) FROM objects WHERE uid = ?`, uid))
}

// changed_at is the time of the write that last changed the row, so it agrees with
// write_seq on what a change is: a rewrite of the same version keeps it, and a new
// version or a new identity moves it to that write's time.
func TestChangedAtMovesOnlyWhenTheRowChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := newTestStore(t)
	updatedAt := func() int64 {
		return int64(countRows(t, s, `SELECT updated_at FROM objects WHERE uid = 'uid-1'`))
	}

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Added, pod("uid-1", "api-0", "42")))
	first := changedAt(t, s, "uid-1")
	assert.Equal(t, updatedAt(), first, "the first write is a change")

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-1", "api-0", "42")))
	session := beginReplace(t, s, podKind)
	require.NoError(t, session.WritePage(ctx, []*unstructured.Unstructured{pod("uid-1", "api-0", "42")}))
	_, err := session.Commit(ctx, "100")
	require.NoError(t, err)
	assert.Equal(t, first, changedAt(t, s, "uid-1"), "a rewrite of the same version")
	assert.Greater(t, updatedAt(), first)

	require.NoError(t, s.ApplyChange(ctx, podKind, watch.Modified, pod("uid-1", "api-0", "43")))
	moved := changedAt(t, s, "uid-1")
	assert.Equal(t, updatedAt(), moved, "a new resourceVersion")

	renamed := Kind{APIVersion: "v1beta1", Kind: "Pod", Resource: "pods"}
	require.NoError(t, s.ApplyChange(ctx, renamed, watch.Modified, pod("uid-1", "api-0", "43")))
	assert.Equal(t, updatedAt(), changedAt(t, s, "uid-1"), "a new identity")
	assert.Greater(t, changedAt(t, s, "uid-1"), moved)
}
