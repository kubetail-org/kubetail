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

	"github.com/kubetail-org/kubetail/modules/shared/k8shelpers"
)

func TestGetBearerTokenRequired(t *testing.T) {
	r := &Resolver{}

	t.Run("rejects missing token", func(t *testing.T) {
		_, err := r.getBearerTokenRequired(context.Background())
		assert.Error(t, err)
	})

	t.Run("rejects empty token", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), k8shelpers.K8STokenCtxKey, "")
		_, err := r.getBearerTokenRequired(ctx)
		assert.Error(t, err)
	})

	t.Run("rejects empty token", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), k8shelpers.K8STokenCtxKey, " ")
		_, err := r.getBearerTokenRequired(ctx)
		assert.Error(t, err)
	})

	t.Run("accepts non-empty token", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), k8shelpers.K8STokenCtxKey, "xxx")
		token, err := r.getBearerTokenRequired(ctx)
		assert.NoError(t, err)
		assert.Equal(t, "xxx", token)
	})
}
