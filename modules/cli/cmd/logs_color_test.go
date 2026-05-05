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
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUseColor_NonTTYWriterIsFalse(t *testing.T) {
	assert.False(t, useColor(&bytes.Buffer{}))
}

func TestUseColor_RegularFileIsFalse(t *testing.T) {
	// A regular temp file is an *os.File but not a TTY — must not get color.
	f, err := os.CreateTemp(t.TempDir(), "out")
	require.NoError(t, err)
	defer f.Close()
	assert.False(t, useColor(f))
}

func TestUseColor_NoColorEnvIsFalse(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	f, err := os.CreateTemp(t.TempDir(), "out")
	require.NoError(t, err)
	defer f.Close()
	assert.False(t, useColor(f))
}

func TestGetDotIndicator_NoColorReturnsPlainBullet(t *testing.T) {
	got := getDotIndicator("abc123", false)
	assert.Equal(t, "●", got)
	assert.NotContains(t, got, "\033[")
}

func TestGetDotIndicator_ColorWrapsBullet(t *testing.T) {
	got := getDotIndicator("abc123", true)
	assert.True(t, strings.HasPrefix(got, "\033["), "expected ANSI prefix, got %q", got)
	assert.Contains(t, got, "●")
	assert.True(t, strings.HasSuffix(got, "\033[0m"), "expected reset suffix, got %q", got)
}

func TestStripUncoloredDot_RemovesDotWhenNoColor(t *testing.T) {
	got := stripUncoloredDot([]string{"timestamp", "dot", "pod"}, false)
	assert.Equal(t, []string{"timestamp", "pod"}, got)
}

func TestStripUncoloredDot_PreservesDotWhenColor(t *testing.T) {
	in := []string{"timestamp", "dot"}
	got := stripUncoloredDot(in, true)
	assert.Equal(t, in, got)
}

func TestStripUncoloredDot_NoOpWhenDotAbsent(t *testing.T) {
	in := []string{"timestamp", "pod"}
	got := stripUncoloredDot(in, false)
	assert.Equal(t, in, got)
}
