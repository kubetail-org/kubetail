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
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kubetail-org/kubetail/modules/cli/internal/clusterapi"
)

func TestSelectBackend_ExplicitKubernetes(t *testing.T) {
	tryCalled := false
	tryKubetail := func() error {
		tryCalled = true
		return nil
	}
	got, err := selectBackend("kubernetes", tryKubetail)
	require.NoError(t, err)
	assert.Equal(t, backendKubernetes, got)
	assert.False(t, tryCalled, "tryKubetail must not be invoked when backend is explicit kubernetes")
}

func TestSelectBackend_ExplicitKubetail_ErrorsWhenNotInstalled(t *testing.T) {
	tryKubetail := func() error { return clusterapi.ErrAPINotInstalled }
	_, err := selectBackend("kubetail", tryKubetail)
	require.Error(t, err)
	assert.Contains(t, err.Error(), clusterapi.APIServiceName)
}

func TestSelectBackend_ExplicitKubetail_ErrorsOnOtherFailure(t *testing.T) {
	tryKubetail := func() error { return errors.New("rbac") }
	_, err := selectBackend("kubetail", tryKubetail)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rbac")
}

func TestSelectBackend_ExplicitKubetail_OkWhenAvailable(t *testing.T) {
	tryKubetail := func() error { return nil }
	got, err := selectBackend("kubetail", tryKubetail)
	require.NoError(t, err)
	assert.Equal(t, backendKubetail, got)
}

func TestSelectBackend_AutoPicksKubetailWhenAvailable(t *testing.T) {
	tryKubetail := func() error { return nil }
	got, err := selectBackend("auto", tryKubetail)
	require.NoError(t, err)
	assert.Equal(t, backendKubetail, got)
}

func TestSelectBackend_AutoFallsBackOnNotInstalled(t *testing.T) {
	tryKubetail := func() error {
		return fmt.Errorf("wrapped: %w", clusterapi.ErrAPINotInstalled)
	}
	got, err := selectBackend("auto", tryKubetail)
	require.NoError(t, err, "auto must silently fall back on not-installed")
	assert.Equal(t, backendKubernetes, got)
}

func TestSelectBackend_AutoPropagatesOtherErrors(t *testing.T) {
	tryKubetail := func() error { return errors.New("network blew up") }
	_, err := selectBackend("auto", tryKubetail)
	require.Error(t, err, "auto must surface non-not-installed errors")
	assert.Contains(t, err.Error(), "network blew up")
}

func TestSelectBackend_RejectsUnknownValue(t *testing.T) {
	tryKubetail := func() error { return nil }
	_, err := selectBackend("weird", tryKubetail)
	require.Error(t, err)
}

func TestShouldWarnFallback(t *testing.T) {
	tests := []struct {
		flag   string
		choice backendChoice
		want   bool
	}{
		{"auto", backendKubernetes, true},
		{"", backendKubernetes, true},
		{"auto", backendKubetail, false},
		{"kubernetes", backendKubernetes, false},
		{"kubetail", backendKubetail, false},
	}
	for _, tt := range tests {
		t.Run(tt.flag+"_"+fmt.Sprint(tt.choice), func(t *testing.T) {
			assert.Equal(t, tt.want, shouldWarnFallback(tt.flag, tt.choice))
		})
	}
}

func TestColorizeWarning_NonTTYWriterReturnsPlainText(t *testing.T) {
	// bytes.Buffer is not an *os.File, so colorizeWarning must not inject
	// ANSI escapes — redirected stderr / CI logs see clean text.
	var buf bytes.Buffer
	got := colorizeWarning(&buf, kubetailFallbackWarning)
	assert.Equal(t, kubetailFallbackWarning, got)
	assert.NotContains(t, got, "\033[")
}

func TestColorizeWarning_NoColorEnvDisablesEscapes(t *testing.T) {
	// Even on a real TTY, NO_COLOR (https://no-color.org) must suppress
	// escapes. Use a *os.File to exercise the TTY branch's short-circuit.
	t.Setenv("NO_COLOR", "1")
	f, err := os.CreateTemp(t.TempDir(), "tty")
	require.NoError(t, err)
	defer f.Close()
	got := colorizeWarning(f, kubetailFallbackWarning)
	assert.Equal(t, kubetailFallbackWarning, got)
}
