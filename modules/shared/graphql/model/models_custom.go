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

package model

import (
	"io"
	"strconv"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

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
	return out, errors.NewValidationError("int64", "Expected string representing 64-bit integer")
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
		return nil, errors.NewValidationError("timestamppbtimestamp", "Expected RFC3339 formatted string")
	}

	// convert to time
	t, err := time.Parse(time.RFC3339Nano, tmpStr)
	if err != nil {
		return nil, err
	}

	// convert to timestamppb.Timestamp
	return timestamppb.New(t), nil
}
