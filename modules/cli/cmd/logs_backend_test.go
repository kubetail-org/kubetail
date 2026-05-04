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

package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectBackend_ExplicitKubernetes(t *testing.T) {
	probeCalled := false
	probe := func(ctx context.Context) (bool, error) {
		probeCalled = true
		return true, nil
	}
	got, err := selectBackend(context.Background(), "kubernetes", probe)
	require.NoError(t, err)
	assert.Equal(t, backendKubernetes, got)
	assert.False(t, probeCalled, "probe must not be invoked when backend is explicit")
}

func TestSelectBackend_ExplicitKubetail_ErrorsWhenUnavailable(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return false, nil }
	_, err := selectBackend(context.Background(), "kubetail", probe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "v1.api.kubetail.com")
}

func TestSelectBackend_ExplicitKubetail_ErrorsWhenProbeFails(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return false, errors.New("rbac") }
	_, err := selectBackend(context.Background(), "kubetail", probe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rbac")
}

func TestSelectBackend_ExplicitKubetail_OkWhenAvailable(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return true, nil }
	got, err := selectBackend(context.Background(), "kubetail", probe)
	require.NoError(t, err)
	assert.Equal(t, backendKubetail, got)
}

func TestSelectBackend_AutoPicksKubetailWhenAvailable(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return true, nil }
	got, err := selectBackend(context.Background(), "auto", probe)
	require.NoError(t, err)
	assert.Equal(t, backendKubetail, got)
}

func TestSelectBackend_AutoFallsBackOnUnavailable(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return false, nil }
	got, err := selectBackend(context.Background(), "auto", probe)
	require.NoError(t, err)
	assert.Equal(t, backendKubernetes, got)
}

func TestSelectBackend_AutoFallsBackOnProbeError(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return false, errors.New("network") }
	got, err := selectBackend(context.Background(), "auto", probe)
	require.NoError(t, err, "auto must not fail loudly when probe errors")
	assert.Equal(t, backendKubernetes, got)
}

func TestSelectBackend_RejectsUnknownValue(t *testing.T) {
	probe := func(ctx context.Context) (bool, error) { return false, nil }
	_, err := selectBackend(context.Background(), "weird", probe)
	require.Error(t, err)
}
