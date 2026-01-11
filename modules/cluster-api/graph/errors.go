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
	"fmt"

	"github.com/vektah/gqlparser/v2/gqlerror"
	"google.golang.org/grpc"

	"github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

// New GRPC error
func NewGrpcError(conn *grpc.ClientConn, err error) *gqlerror.Error {
	err = fmt.Errorf("%s: %w", conn.CanonicalTarget(), err)
	return errors.NewError("INTERNAL_SERVER_ERROR", err.Error())
}
