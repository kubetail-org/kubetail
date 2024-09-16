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

package model

import (
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"google.golang.org/protobuf/types/known/timestamppb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubetail-org/kubetail/backend/server/graph/lib"
)

type List interface{}

type Object interface{}

// Int64 scalar
func MarshalInt64(val int64) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		io.WriteString(w, strconv.Quote(strconv.FormatInt(val, 10)))
	})
}

func UnmarshalInt64(v interface{}) (int64, error) {
	var out int64
	if tmpStr, ok := v.(string); ok {
		return strconv.ParseInt(tmpStr, 10, 64)
	}
	return out, lib.NewValidationError("int64", "Expected string representing 64-bit integer")
}

// StringMap scalar
func MarshalStringMap(val map[string]string) graphql.Marshaler {
	return graphql.WriterFunc(func(w io.Writer) {
		err := json.NewEncoder(w).Encode(val)
		if err != nil {
			panic(err)
		}
	})
}

func UnmarshalStringMap(v interface{}) (map[string]string, error) {
	if m, ok := v.(map[string]string); ok {
		return m, nil
	}
	return nil, lib.NewValidationError("stringmap", "Expected json-encoded string representing map[string]string")
}

// MetaV1Time scalar
func MarshalMetaV1Time(t metav1.Time) graphql.Marshaler {
	if t.IsZero() {
		return graphql.Null
	}

	return graphql.WriterFunc(func(w io.Writer) {
		b, _ := t.MarshalJSON()
		w.Write(b)
	})
}

func UnmarshalMetaV1Time(v interface{}) (metav1.Time, error) {
	var t metav1.Time
	if tmpStr, ok := v.(string); ok {
		err := t.UnmarshalQueryParameter(tmpStr)
		return t, err
	}
	return t, lib.NewValidationError("metav1time", "Expected RFC3339 formatted string")
}

// TimestampPBTimestamp scalar
func TimestampPBTimestamp(ts *timestamppb.Timestamp) graphql.Marshaler {
	t := ts.AsTime()

	if t.IsZero() {
		return graphql.Null
	}

	return graphql.WriterFunc(func(w io.Writer) {
		b, _ := t.MarshalJSON()
		w.Write(b)
	})
}

func UnmarshalTimestampPBTimestamp(v interface{}) (*timestamppb.Timestamp, error) {
	// convert to string
	tmpStr, ok := v.(string)
	if !ok {
		return nil, lib.NewValidationError("timestamppbtimestamp", "Expected RFC3339 formatted string")
	}

	// convert to time
	t, err := time.Parse(time.RFC3339Nano, tmpStr)
	if err != nil {
		return nil, err
	}

	// convert to timestamppb.Timestamp
	return timestamppb.New(t), nil
}
