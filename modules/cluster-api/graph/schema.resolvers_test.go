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
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	gqlerrors "github.com/kubetail-org/kubetail/modules/shared/graphql/errors"
)

func TestLogRecordsFetchRequiresToken(t *testing.T) {
	r := &queryResolver{}
	_, err := r.LogRecordsFetch(context.Background(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	assert.ErrorIs(t, gqlerrors.ErrUnauthenticated, err)
}

func TestLogRecordsFollowRequiresToken(t *testing.T) {
	r := &subscriptionResolver{}
	_, err := r.LogRecordsFollow(context.Background(), nil, nil, nil, nil, nil, nil)
	assert.ErrorIs(t, gqlerrors.ErrUnauthenticated, err)
}
