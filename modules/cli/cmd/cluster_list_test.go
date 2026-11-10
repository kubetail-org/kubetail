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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
)

func TestRunClusterList(t *testing.T) {
	deployed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client := &fakeHelmClient{releases: []*release.Release{
		{
			Name:      "kubetail",
			Namespace: "kubetail-system",
			Version:   2,
			Info:      &release.Info{LastDeployed: deployed, Status: common.StatusDeployed},
			Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: "kubetail", Version: "0.9.1", AppVersion: "0.5.0"}},
		},
	}}
	var out bytes.Buffer

	err := runClusterList(&out, client)
	require.NoError(t, err)

	// tabwriter aligns each column under its header
	want := "" +
		"NAME      NAMESPACE        REVISION  UPDATED                        STATUS    CHART           APP VERSION\n" +
		"kubetail  kubetail-system  2         2026-01-02 03:04:05 +0000 UTC  deployed  kubetail-0.9.1  0.5.0\n"
	assert.Equal(t, want, out.String())
}

func TestRunClusterList_NoReleases(t *testing.T) {
	var out bytes.Buffer

	err := runClusterList(&out, &fakeHelmClient{})

	require.NoError(t, err)
	assert.Equal(t, "NAME  NAMESPACE  REVISION  UPDATED  STATUS  CHART  APP VERSION\n", out.String())
}

func TestRunClusterList_Error(t *testing.T) {
	wantErr := errors.New("connection refused")
	var out bytes.Buffer

	err := runClusterList(&out, &fakeHelmClient{err: wantErr})

	assert.ErrorIs(t, err, wantErr)
	assert.Empty(t, out.String())
}
