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

package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"

	"github.com/kubetail-org/kubetail/apps/desktop/sidecar/internal/lib/apimeta"
)

// Entry is one single-value setting, a row of app.db's settings per scope.
// T's kind is how it is stored: a bool as the integer 0 or 1, an int64 as an
// integer, a string as text, and any other type, other integer types
// included, as JSON text. Default and Strictest are handed to every reader,
// so a T holding a slice or a map is never modified by one.
type Entry[T any] struct {
	Key     string
	Default T
	// Strictest is what a value Kubetail cannot use reads as; nil reads it as
	// Default. A setting that can widen what a run may do names one.
	Strictest *T
	// PerCluster lets a cluster have a value of its own.
	PerCluster bool
	// Check refuses a value Kubetail cannot use; nil passes every value.
	Check func(T) error
}

// SettingRefusal is a value a setting's check refuses: its key and the
// check's error.
type SettingRefusal struct {
	Key    string
	Reason error
}

func (r SettingRefusal) Error() string { return r.Key + ": " + r.Reason.Error() }
func (r SettingRefusal) Unwrap() error { return r.Reason }

// settingScope is where a setting's value comes from.
type settingScope int

const (
	scopeDefault      settingScope = iota // no row, or a NULL
	scopeEveryCluster                     // the row for every cluster
	scopeOwn                              // the cluster's own row
)

var (
	// errNoClusterScope is a cluster's write or reset of a setting no cluster
	// has its own of, which no read would see.
	errNoClusterScope = errors.New("settings: this setting has no value of a cluster's own")
	// errNullValue is a JSON value that encodes as null: a reset is the
	// row's NULL, and a read refuses the text null.
	errNullValue = errors.New("a value is never null")
)

// Why a stored value cannot be used. The store logs these, never the value.
var (
	errWrongKind   = errors.New("a value of the wrong kind")
	errUndecodable = errors.New("JSON that does not decode")
	errRefused     = errors.New("a value its check refuses")
)

// encode is v as stored, once e's check passes it.
func (e Entry[T]) encode(v T) (any, error) {
	if e.Check != nil {
		if err := e.Check(v); err != nil {
			return nil, SettingRefusal{Key: e.Key, Reason: err}
		}
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Bool:
		if rv.Bool() {
			return int64(1), nil
		}
		return int64(0), nil
	case reflect.Int64:
		return rv.Int(), nil
	case reflect.String:
		return rv.String(), nil
	}
	data, err := json.Marshal(v)
	if err != nil { // coverage:ignore an entry's JSON type is one json.Marshal always encodes
		return nil, fmt.Errorf("encode %s: %w", e.Key, err)
	}
	if string(data) == "null" {
		return nil, SettingRefusal{Key: e.Key, Reason: errNullValue}
	}
	return string(data), nil
}

// decode is a stored value as T, or why Kubetail cannot use it.
func (e Entry[T]) decode(raw any) (T, error) {
	var zero, v T
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Bool:
		n, ok := raw.(int64)
		if !ok || (n != 0 && n != 1) {
			return zero, errWrongKind
		}
		rv.SetBool(n == 1)
	case reflect.Int64:
		n, ok := raw.(int64)
		if !ok {
			return zero, errWrongKind
		}
		rv.SetInt(n)
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return zero, errWrongKind
		}
		rv.SetString(s)
	default:
		s, ok := raw.(string)
		if !ok {
			return zero, errWrongKind
		}
		// A NULL is a row's own value, so the JSON null is not one; nor is
		// anything after the first value.
		dec := json.NewDecoder(strings.NewReader(s))
		dec.DisallowUnknownFields()
		if strings.TrimSpace(s) == "null" || dec.Decode(&v) != nil || dec.Decode(new(json.RawMessage)) != io.EOF {
			return zero, errUndecodable
		}
	}
	if e.Check != nil && e.Check(v) != nil {
		return zero, errRefused
	}
	return v, nil
}

// valueOf is a stored value, not NULL, as e reads it: one Kubetail cannot use
// reads as e's strictest value, else its default.
func (e Entry[T]) valueOf(raw any) T {
	v, err := e.decode(raw)
	if err == nil {
		return v
	}
	slog.Warn("settings: a stored value cannot be used", "key", e.Key, "reason", err.Error())
	if e.Strictest != nil {
		return *e.Strictest
	}
	return e.Default
}

// valueIn is e's value for every cluster in stored, every setting's stored
// value by key: the default for no row or a NULL.
func (e Entry[T]) valueIn(stored map[string]any) T {
	if raw := stored[e.Key]; raw != nil {
		return e.valueOf(raw)
	}
	return e.Default
}

// valueFor is e's value for cluster ("" for every cluster) in scopes, its
// stored values by cluster, and where it comes from: the cluster's own row,
// else the row for every cluster, else the default. A NULL falls through. An
// entry with no PerCluster reads the row for every cluster alone.
func (e Entry[T]) valueFor(scopes map[apimeta.ClusterID]any, cluster apimeta.ClusterID) (T, settingScope) {
	if raw := scopes[cluster]; e.PerCluster && cluster != "" && raw != nil {
		return e.valueOf(raw), scopeOwn
	}
	if raw := scopes[""]; raw != nil {
		return e.valueOf(raw), scopeEveryCluster
	}
	return e.Default, scopeDefault
}

// readSetting is e's value for cluster and where it comes from, as valueFor
// answers them, from the two rows that can hold it.
func readSetting[T any](ctx context.Context, st stmts, e Entry[T], cluster apimeta.ClusterID) (T, settingScope, error) {
	if !e.PerCluster {
		cluster = ""
	}
	scopes, err := selectScopedSetting(ctx, st, e.Key, cluster)
	if err != nil {
		return e.Default, scopeDefault, err
	}
	v, scope := e.valueFor(scopes, cluster)
	return v, scope, nil
}

// selectScopedSetting is key's stored values for cluster and for every
// cluster, by cluster, "" for every cluster; a NULL is nil.
func selectScopedSetting(ctx context.Context, st stmts, key string, cluster apimeta.ClusterID) (map[apimeta.ClusterID]any, error) {
	rows, err := st.Query(ctx, stmtSelectScopedSetting, cluster, key)
	if err != nil {
		return nil, fmt.Errorf("select setting %s: %w", key, err)
	}
	return scanSettings[apimeta.ClusterID](rows)
}

// selectSettingScopes is key's stored value in every scope, by cluster, ""
// for every cluster; a NULL is nil.
func selectSettingScopes(ctx context.Context, st stmts, key string) (map[apimeta.ClusterID]any, error) {
	rows, err := st.Query(ctx, stmtSelectSettingScopes, key)
	if err != nil {
		return nil, fmt.Errorf("select setting %s: %w", key, err)
	}
	return scanSettings[apimeta.ClusterID](rows)
}

// scanSettings reads rows of a name, a key or a cluster, and a stored value
// into a map, and closes rows.
func scanSettings[K ~string](rows *sql.Rows) (map[K]any, error) {
	defer rows.Close()
	stored := map[K]any{}
	for rows.Next() {
		var name K
		var raw any
		if err := rows.Scan(&name, &raw); err != nil { // coverage:ignore the name is a STRICT TEXT column, and value scans into any
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		stored[name] = raw
	}
	if err := rows.Err(); err != nil { // coverage:ignore an I/O error past the first row, which no test can inject
		return nil, fmt.Errorf("scan settings: %w", err)
	}
	return stored, nil
}

// writeSetting stores v as e's value for cluster ("" for every cluster), once
// e's check passes it: a SettingRefusal otherwise, writing nothing.
func writeSetting[T any](ctx context.Context, st stmts, e Entry[T], cluster apimeta.ClusterID, v T, at int64) error {
	if err := e.checkScope(cluster); err != nil {
		return err
	}
	raw, err := e.encode(v)
	if err != nil {
		return err
	}
	return upsertSetting(ctx, st, e.Key, cluster, raw, at)
}

// resetSetting puts e back to its default for cluster: a NULL, so the row
// keeps the time of the change.
func resetSetting[T any](ctx context.Context, st stmts, e Entry[T], cluster apimeta.ClusterID, at int64) error {
	if err := e.checkScope(cluster); err != nil {
		return err
	}
	return upsertSetting(ctx, st, e.Key, cluster, nil, at)
}

// checkScope refuses a cluster for an entry no cluster has its own of.
func (e Entry[T]) checkScope(cluster apimeta.ClusterID) error {
	if cluster != "" && !e.PerCluster {
		return errNoClusterScope
	}
	return nil
}

func upsertSetting(ctx context.Context, st stmts, key string, cluster apimeta.ClusterID, raw any, at int64) error {
	if _, err := st.Exec(ctx, stmtUpsertSetting, key, cluster, raw, at); err != nil {
		return fmt.Errorf("write setting %s: %w", key, err)
	}
	return nil
}
